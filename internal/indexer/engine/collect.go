package engine

import (
	"fmt"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/scan"
	"path/filepath"
	"strings"
)

// RawFile — после парсинга, с canonicalized FQN'ами.
type RawFile struct {
	FileID    string
	Key       string
	RelPath   string
	AbsPath   string
	Lang      string
	Package   string
	Parsed    *parse.ParseResult        // FQN'ы уже canonical (с modulePrefix для Python)
	ImportMap map[string]string         // alias → resolved FQN из ImportRef
}

// CollectResult — сырое представление + глобальные карты для Resolve.
type CollectResult struct {
	Files []*RawFile

	// Глобальные индексы по nodes:
	ObjectFQNs       map[string]string            // fqn → nodeID (только objects)
	MethodFQNs       map[string]string            // fqn → nodeID (только methods)
	NameToMethods    map[string][]string          // simpleName → []nodeID (для Pass 3 calls)
	NameToObjects    map[string][]string          // simpleName → []nodeID (для inherits Pass 3)
	OwnerToMethods   map[string]map[string]string // ownerFQN → name → methodID
	FileToMethods    map[string]map[string]string // fileID → name → methodID (для same-file без owner: top-level fns + module)
	FileByImport     map[string]string            // import_str → fileID
	VarTypes         map[string]map[string]string // scopeFQN → varName → typeName
	NodeShortIDs     map[string]int64             // nodeID → assigned short_id
	FileShortIDs     map[string]int64             // fileID → assigned short_id
}

// Collect парсит все файлы проекта и строит индексные карты для Resolve.
func Collect(rootAbs string, cfg *services.Config, matcher *services.Matcher, parsers map[string]parse.Parser) (*CollectResult, error) {
	entries, err := scan.Scan(rootAbs, cfg, matcher)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	cr := &CollectResult{
		ObjectFQNs:     map[string]string{},
		MethodFQNs:     map[string]string{},
		NameToMethods:  map[string][]string{},
		NameToObjects:  map[string][]string{},
		OwnerToMethods: map[string]map[string]string{},
		FileToMethods:  map[string]map[string]string{},
		FileByImport:   map[string]string{},
		VarTypes:       map[string]map[string]string{},
		NodeShortIDs:   map[string]int64{},
		FileShortIDs:   map[string]int64{},
	}

	var nextFileShort int64 = 1
	var nextNodeShort int64 = 1

	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.AbsPath))
		p, ok := parsers[ext]
		if !ok {
			continue
		}
		lang := langFromExt(ext)
		res, err := p.Parse(e.AbsPath)
		if err != nil {
			continue // best-effort
		}

		fid := store.FileID(e.Key)
		cr.FileShortIDs[fid] = nextFileShort
		nextFileShort++

		modPrefix := modulePrefix(e.RelPath, lang)
		canonicalize(res, modPrefix)

		// Resolve relative Python imports: ".utils" → "pkg.utils"
		if lang == "python" {
			canonicalizePyImports(res, store.PythonModuleName(e.RelPath))
		}

		importMap := buildImportMap(res)

		rf := &RawFile{
			FileID:    fid,
			Key:       e.Key,
			RelPath:   e.RelPath,
			AbsPath:   e.AbsPath,
			Lang:      lang,
			Package:   res.Package,
			Parsed:    res,
			ImportMap: importMap,
		}
		cr.Files = append(cr.Files, rf)

		// Заранее назначаем short_ids всем нодам и заполняем индексы.
		// Сначала objects (методы могут зависеть от owner_id).
		for i := range res.Objects {
			obj := &res.Objects[i]
			nid := store.NodeID(lang, store.KindObject, obj.FQN, e.Key, obj.StartLine)
			cr.NodeShortIDs[nid] = nextNodeShort
			nextNodeShort++
			cr.ObjectFQNs[obj.FQN] = nid
			cr.NameToObjects[obj.Name] = append(cr.NameToObjects[obj.Name], nid)
		}
		for i := range res.Methods {
			m := &res.Methods[i]
			nid := store.NodeID(lang, store.KindMethod, m.FQN, e.Key, m.StartLine)
			cr.NodeShortIDs[nid] = nextNodeShort
			nextNodeShort++
			cr.MethodFQNs[m.FQN] = nid
			cr.NameToMethods[m.Name] = append(cr.NameToMethods[m.Name], nid)
			if m.OwnerFQN != "" {
				if cr.OwnerToMethods[m.OwnerFQN] == nil {
					cr.OwnerToMethods[m.OwnerFQN] = map[string]string{}
				}
				cr.OwnerToMethods[m.OwnerFQN][m.Name] = nid
			}
			if cr.FileToMethods[fid] == nil {
				cr.FileToMethods[fid] = map[string]string{}
			}
			cr.FileToMethods[fid][m.Name] = nid
		}

		// Var-types
		for _, vt := range res.VarTypes {
			if cr.VarTypes[vt.ScopeFQN] == nil {
				cr.VarTypes[vt.ScopeFQN] = map[string]string{}
			}
			cr.VarTypes[vt.ScopeFQN][vt.VarName] = vt.TypeName
		}
	}

	// FileByImport — после полного списка файлов.
	for _, rf := range cr.Files {
		switch rf.Lang {
		case "python":
			cr.FileByImport[store.PythonModuleName(rf.RelPath)] = rf.FileID
		case "java":
			for _, obj := range rf.Parsed.Objects {
				// FQN уже canonical (com.x.OrderRepo)
				cr.FileByImport[obj.FQN] = rf.FileID
				// Plus simple name если уникально
				if _, exists := cr.FileByImport[obj.Name]; !exists {
					cr.FileByImport[obj.Name] = rf.FileID
				}
			}
		}
	}

	return cr, nil
}

// canonicalize применяет modulePrefix ко всем FQN-полям ParseResult.
// Для Java extractor уже даёт canonical FQN — modulePrefix="" → no-op.
func canonicalize(res *parse.ParseResult, modPrefix string) {
	if modPrefix == "" {
		return
	}
	prep := func(s string) string {
		if s == "" {
			return ""
		}
		return modPrefix + "." + s
	}
	for i := range res.Objects {
		res.Objects[i].FQN = prep(res.Objects[i].FQN)
	}
	for i := range res.Methods {
		res.Methods[i].FQN = prep(res.Methods[i].FQN)
		res.Methods[i].OwnerFQN = prep(res.Methods[i].OwnerFQN)
	}
	for i := range res.Calls {
		res.Calls[i].CallerFQN = prep(res.Calls[i].CallerFQN)
	}
	for i := range res.VarTypes {
		res.VarTypes[i].ScopeFQN = prep(res.VarTypes[i].ScopeFQN)
	}
}

// buildImportMap извлекает alias→fqn для Pass 2 резолюции.
// ImportRef с пустым Alias служат только для file-edge и в map не попадают.
func buildImportMap(res *parse.ParseResult) map[string]string {
	m := map[string]string{}
	for _, imp := range res.Imports {
		if imp.Alias == "" {
			continue
		}
		// Java: alias=ClassName, raw=com.x.ClassName.
		// Python: alias=имя в коде, raw=модуль (для from-import) или модуль (для plain).
		m[imp.Alias] = imp.Raw
	}
	return m
}

// canonicalizePyImports конвертирует относительные импорты Python в абсолютные.
// fileModule — модуль текущего файла, e.g. "pkg.sub.foo".
func canonicalizePyImports(res *parse.ParseResult, fileModule string) {
	if len(res.Imports) == 0 {
		return
	}
	moduleParts := strings.Split(fileModule, ".")
	pkgParts := moduleParts[:len(moduleParts)-1]

	for i := range res.Imports {
		raw := res.Imports[i].Raw
		if !strings.HasPrefix(raw, ".") {
			continue
		}
		dots := 0
		for _, c := range raw {
			if c == '.' {
				dots++
			} else {
				break
			}
		}
		rest := raw[dots:]
		upLevels := dots - 1
		if upLevels > len(pkgParts) {
			continue
		}
		baseParts := pkgParts[:len(pkgParts)-upLevels]
		var abs string
		switch {
		case rest == "" && len(baseParts) == 0:
			continue
		case rest == "":
			abs = strings.Join(baseParts, ".")
		case len(baseParts) == 0:
			abs = rest
		default:
			abs = strings.Join(baseParts, ".") + "." + rest
		}
		res.Imports[i].Raw = abs
	}
}

// modulePrefix — префикс канонического FQN для файла.
// Python: "pkg/mod.py" → "pkg.mod"; Java: "" (extractor уже добавил пакет).
func modulePrefix(relPath, lang string) string {
	switch lang {
	case "python":
		return store.PythonModuleName(relPath)
	default:
		return ""
	}
}

// langFromExt возвращает имя языка по расширению файла.
func langFromExt(ext string) string {
	switch ext {
	case ".py":
		return "python"
	case ".java":
		return "java"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
