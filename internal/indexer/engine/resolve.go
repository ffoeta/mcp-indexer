package engine

import (
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/parse"
	"strings"
)

// ResolveResult — готовые к записи в БД строки.
type ResolveResult struct {
	Files    []store.FileRow
	Nodes    []store.NodeRow
	Calls    []store.CallEdge
	Inherits []store.InheritEdge
	Imports  []store.ImportEdge
	FTSDocs  []store.SearchDoc
}

// Resolve — Phase 2: преобразует CollectResult в строки БД.
// Резолюция вызовов идёт в 3 прохода (same-file → by-import → global).
func Resolve(cr *CollectResult, norm *tokenize.Normalizer) *ResolveResult {
	rr := &ResolveResult{}

	// ───── files ─────
	for _, rf := range cr.Files {
		rr.Files = append(rr.Files, store.FileRow{
			FileID:  rf.FileID,
			ShortID: cr.FileShortIDs[rf.FileID],
			Key:     rf.Key,
			RelPath: rf.RelPath,
			Lang:    rf.Lang,
			Package: rf.Package,
			Hash:    "",
		})
		// FTS: file
		rr.FTSDocs = append(rr.FTSDocs, store.SearchDoc{
			DocID:   rf.FileID,
			DocKind: store.DocFile,
			Path:    preTokenize(rf.RelPath, norm),
		})
	}

	// ───── objects ─────
	for _, rf := range cr.Files {
		for _, obj := range rf.Parsed.Objects {
			// Каноничный nid пересчитываем по (lang, kind, fqn, key, startLine):
			// это совпадает с ключом из Collect и однозначно идентифицирует
			// конкретный AST-узел (важно при «наложении» FQN, например при
			// перегрузках конструкторов в Java).
			nid := store.NodeID(rf.Lang, store.KindObject, obj.FQN, rf.Key, obj.StartLine)
			rr.Nodes = append(rr.Nodes, store.NodeRow{
				NodeID:    nid,
				ShortID:   cr.NodeShortIDs[nid],
				FileID:    rf.FileID,
				Kind:      store.KindObject,
				Subkind:   obj.Subkind,
				Name:      obj.Name,
				FQN:       obj.FQN,
				OwnerID:   "", // объекты сами могут быть owner-ами; nested через owner_id ниже
				Scope:     store.ScopeGlobal,
				Doc:       obj.Doc,
				StartLine: obj.StartLine,
				EndLine:   obj.EndLine,
			})
			rr.FTSDocs = append(rr.FTSDocs, store.SearchDoc{
				DocID:   nid,
				DocKind: store.DocNode,
				Name:    preTokenize(obj.Name, norm),
				FQN:     preTokenize(obj.FQN, norm),
				Path:    preTokenize(rf.RelPath, norm),
			})
		}
	}

	// nested objects: object.owner_id = parent object (если parent object same file)
	for i := range rr.Nodes {
		n := &rr.Nodes[i]
		if n.Kind != store.KindObject {
			continue
		}
		// parent — strip last segment if exists и matches существующему object
		parentFQN := stripLastSegment(n.FQN)
		if parentFQN == "" {
			continue
		}
		if pid, ok := cr.ObjectFQNs[parentFQN]; ok {
			n.OwnerID = pid
		}
	}

	// ───── methods ─────
	for _, rf := range cr.Files {
		for _, m := range rf.Parsed.Methods {
			// nid строим напрямую из (lang, kind, fqn, key, startLine) — иначе
			// перегрузки (одинаковый FQN, разные строки, e.g. два Java-ctor)
			// получили бы одинаковый short_id и упёрлись в UNIQUE-constraint.
			mid := store.NodeID(rf.Lang, store.KindMethod, m.FQN, rf.Key, m.StartLine)
			ownerID := ""
			if m.OwnerFQN != "" {
				if oid, ok := cr.ObjectFQNs[m.OwnerFQN]; ok {
					ownerID = oid
				}
			}
			rr.Nodes = append(rr.Nodes, store.NodeRow{
				NodeID:    mid,
				ShortID:   cr.NodeShortIDs[mid],
				FileID:    rf.FileID,
				Kind:      store.KindMethod,
				Subkind:   m.Subkind,
				Name:      m.Name,
				FQN:       m.FQN,
				OwnerID:   ownerID,
				Scope:     m.Scope,
				Signature: m.Signature,
				Doc:       m.Doc,
				StartLine: m.StartLine,
				EndLine:   m.EndLine,
			})
			rr.FTSDocs = append(rr.FTSDocs, store.SearchDoc{
				DocID:   mid,
				DocKind: store.DocNode,
				Name:    preTokenize(m.Name, norm),
				FQN:     preTokenize(m.FQN, norm),
				Path:    preTokenize(rf.RelPath, norm),
			})
		}
	}

	// ───── calls ─────
	for _, rf := range cr.Files {
		for _, c := range rf.Parsed.Calls {
			callerID, ok := cr.MethodFQNs[c.CallerFQN]
			if !ok {
				continue // caller вне индекса — отбрасываем (не должно случаться)
			}
			calleeID, conf := resolveCall(c, rf, cr)
			rr.Calls = append(rr.Calls, store.CallEdge{
				CallerID:    callerID,
				CalleeID:    calleeID,
				CalleeName:  c.CalleeName,
				CalleeOwner: c.CalleeOwner,
				Line:        c.Line,
				Confidence:  conf,
			})
		}
	}

	// ───── inherits ─────
	for _, rf := range cr.Files {
		for _, obj := range rf.Parsed.Objects {
			childID := store.NodeID(rf.Lang, store.KindObject, obj.FQN, rf.Key, obj.StartLine)
			modPrefix := modulePrefix(rf.RelPath, rf.Lang)
			for _, base := range obj.Bases {
				parentID := resolveInherit(base.Name, modPrefix, rf, cr)
				rr.Inherits = append(rr.Inherits, store.InheritEdge{
					ChildID:    childID,
					ParentID:   parentID,
					ParentHint: base.Name,
					Relation:   base.Relation,
				})
			}
		}
	}

	// ───── imports (дедупим per-file по Raw) ─────
	for _, rf := range cr.Files {
		seen := map[string]bool{}
		for _, imp := range rf.Parsed.Imports {
			if seen[imp.Raw] {
				continue
			}
			seen[imp.Raw] = true
			tgt := cr.FileByImport[imp.Raw]
			rr.Imports = append(rr.Imports, store.ImportEdge{
				FileID:       rf.FileID,
				TargetFileID: tgt,
				Raw:          imp.Raw,
			})
		}
	}

	return rr
}

// resolveCall — три прохода.
// Возвращает (calleeNodeID, confidence). NodeID="" → unresolved.
func resolveCall(c parse.CallRef, rf *RawFile, cr *CollectResult) (string, int) {
	// Pass 1: same-file
	if id := pass1SameFile(c, rf, cr); id != "" {
		return id, store.ConfSameFile
	}
	// Pass 2: by-import / var-types
	if id := pass2ByImport(c, rf, cr); id != "" {
		return id, store.ConfImport
	}
	// Pass 3: global by simple name
	if id := pass3Global(c, cr); id != "" {
		return id, store.ConfGlobal
	}
	return "", store.ConfNone
}

// pass1SameFile резолвит вызовы внутри одного файла.
//   - bare (CalleeOwner==""): ищем method того же owner-class что и caller; иначе top-level в файле
//   - CalleeOwner совпадает с object в этом же файле: ищем method (owner=CalleeOwner, name=callee)
func pass1SameFile(c parse.CallRef, rf *RawFile, cr *CollectResult) string {
	if c.CalleeOwner == "" {
		// caller's class — strip last segment
		callerOwner := stripLastSegment(c.CallerFQN)
		if callerOwner != "" {
			if methods, ok := cr.OwnerToMethods[callerOwner]; ok {
				if id, ok := methods[c.CalleeName]; ok {
					return id
				}
			}
		}
		// top-level в файле (методы без owner)
		if fileMethods, ok := cr.FileToMethods[rf.FileID]; ok {
			if id, ok := fileMethods[c.CalleeName]; ok {
				// убедимся что это owner-less метод
				return id
			}
		}
		return ""
	}
	// CalleeOwner — простое имя класса в том же файле?
	for _, obj := range rf.Parsed.Objects {
		if obj.Name == c.CalleeOwner {
			if methods, ok := cr.OwnerToMethods[obj.FQN]; ok {
				if id, ok := methods[c.CalleeName]; ok {
					return id
				}
			}
		}
	}
	return ""
}

// pass2ByImport резолвит callee_owner через VarTypes (simple name → typeFQN) либо через importMap.
//
// Учитывает асимметрию импортов:
//   - Java: import com.x.OrderRepo  → importMap["OrderRepo"] = "com.x.OrderRepo" (FQN класса).
//   - Python from-import: from pkg.repo import OrderRepo
//     → importMap["OrderRepo"] = "pkg.repo" (модуль). Класс FQN = модуль + "." + alias.
//
// Поэтому для каждого кандидата пробуем сначала точный FQN, потом модуль.член.
func pass2ByImport(c parse.CallRef, rf *RawFile, cr *CollectResult) string {
	if c.CalleeOwner == "" {
		return ""
	}

	// Кандидаты на тип callee_owner-а (порядок имеет значение: чем раньше — тем точнее).
	var candidates []string

	// 1) Type variable из VarTypes (locals/params/fields).
	if t := varLookup(c.CallerFQN, c.CalleeOwner, cr.VarTypes); t != "" {
		candidates = append(candidates, t)
		if mapped, ok := rf.ImportMap[t]; ok {
			candidates = append(candidates, mapped)            // Java-style: FQN класса
			candidates = append(candidates, mapped+"."+t)       // Python-style: модуль + класс
		}
	}

	// 2) callee_owner сам по себе может быть alias класса (через import).
	if mapped, ok := rf.ImportMap[c.CalleeOwner]; ok {
		candidates = append(candidates, mapped)
		candidates = append(candidates, mapped+"."+c.CalleeOwner)
	}

	// 3) callee_owner может быть напрямую FQN или local class name → FQN.
	candidates = append(candidates, c.CalleeOwner)

	for _, fqn := range candidates {
		if methods, ok := cr.OwnerToMethods[fqn]; ok {
			if id, ok := methods[c.CalleeName]; ok {
				return id
			}
		}
	}
	return ""
}

// pass3Global линкует если есть ровно одно совпадение по simple name.
func pass3Global(c parse.CallRef, cr *CollectResult) string {
	candidates := cr.NameToMethods[c.CalleeName]
	if len(candidates) != 1 {
		return ""
	}
	return candidates[0]
}

// varLookup — поиск типа переменной по cascade scope.
func varLookup(callerFQN, varName string, varTypes map[string]map[string]string) string {
	if scope, ok := varTypes[callerFQN]; ok {
		if t, ok := scope[varName]; ok {
			return t
		}
	}
	// owner-scope (class fields, видны во всех методах класса)
	if owner := stripLastSegment(callerFQN); owner != "" {
		if scope, ok := varTypes[owner]; ok {
			if t, ok := scope[varName]; ok {
				return t
			}
		}
	}
	// file-scope (modulePrefix.<module> → ScopeFQN="" в Python после canonicalize? Нет, Python module-level имеет caller=<module>, ScopeFQN тоже caller-FQN)
	// Заглушка: scope с пустым ключом
	if scope, ok := varTypes[""]; ok {
		if t, ok := scope[varName]; ok {
			return t
		}
	}
	return ""
}

// resolveInherit резолвит base hint в objectID.
func resolveInherit(hint, modPrefix string, rf *RawFile, cr *CollectResult) string {
	// 1) точное совпадение (Java import-resolved или fully qualified)
	if id, ok := cr.ObjectFQNs[hint]; ok {
		return id
	}
	// 2) modulePrefix + hint (intra-package для Python)
	if modPrefix != "" {
		if id, ok := cr.ObjectFQNs[modPrefix+"."+hint]; ok {
			return id
		}
	}
	// 3) через importMap файла
	if fqn, ok := rf.ImportMap[hint]; ok {
		if id, ok := cr.ObjectFQNs[fqn]; ok {
			return id
		}
	}
	// 4) глобально по simple name (если уникально)
	if cands := cr.NameToObjects[hint]; len(cands) == 1 {
		return cands[0]
	}
	return ""
}

// preTokenize применяет нормализатор и склеивает в одну строку (для FTS5).
func preTokenize(s string, norm *tokenize.Normalizer) string {
	return strings.Join(norm.Tokenize(s), " ")
}

func stripLastSegment(fqn string) string {
	i := strings.LastIndex(fqn, ".")
	if i < 0 {
		return ""
	}
	return fqn[:i]
}

func lastSegment(fqn string) string {
	i := strings.LastIndex(fqn, ".")
	if i < 0 {
		return fqn
	}
	return fqn[i+1:]
}