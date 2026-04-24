package engine

import (
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"strings"
)

// ResolveResult — строки, готовые к записи в SQLite.
type ResolveResult struct {
	Files    []store.FileRow
	Symbols  []store.SymbolRow
	Imports  []store.ImportRow
	Edges    []store.EdgeRow
	Postings []store.TermPosting
}

// Resolve — Phase 2: преобразует CollectResult в разрешённые DB-строки.
// Строит рёбра: defines (file→sym, class→method), imports (file→file),
// inherits (sym→sym), calls (sym→sym, intra-file).
func Resolve(cr *CollectResult, norm *tokenize.Normalizer) *ResolveResult {
	rr := &ResolveResult{}
	edgeSeen := map[string]bool{} // дедупликация рёбер (type:from:to)

	// Per-file симвология: fileKey → {sym.Qualified → symbolID}
	// Используется для резолюции calls (callee = local qualified)
	fileSymMap := buildFileSymMap(cr)

	for _, rf := range cr.Files {
		fid := store.FileID(rf.Key)
		modPrefix := modulePrefix(rf.RelPath, rf.Lang)

		// --- File row ---
		rr.Files = append(rr.Files, store.FileRow{
			FileID:  fid,
			Key:     rf.Key,
			RelPath: rf.RelPath,
			Lang:    rf.Lang,
			Hash:    "",
		})

		// --- Symbols + defines edges ---
		for _, sym := range rf.Symbols {
			fq := fullQualified(modPrefix, sym.Qualified)
			sid := store.SymbolID(rf.Lang, fq, rf.Key, sym.StartLine)

			rr.Symbols = append(rr.Symbols, store.SymbolRow{
				SymbolID:  sid,
				FileID:    fid,
				Kind:      sym.Kind,
				Name:      sym.Name,
				Qualified: fq,
				StartLine: sym.StartLine,
				EndLine:   sym.EndLine,
			})

			// file → symbol (defines)
			addEdge(rr, edgeSeen, "defines", fid, sid, 100)

			// class → method (defines)
			if sym.Parent != "" {
				parentFQ := fullQualified(modPrefix, sym.Parent)
				if entry, ok := cr.DefinedMap[parentFQ]; ok {
					parentSID := store.SymbolID(rf.Lang, parentFQ, entry.FileKey, entry.Line)
					addEdge(rr, edgeSeen, "defines", parentSID, sid, 100)
				}
			}

			// symbol → symbol (inherits)
			for _, base := range sym.Bases {
				baseFQ := resolveBase(base, modPrefix, cr.DefinedMap)
				if baseFQ == "" {
					continue
				}
				entry := cr.DefinedMap[baseFQ]
				baseSID := store.SymbolID(rf.Lang, baseFQ, entry.FileKey, entry.Line)
				addEdge(rr, edgeSeen, "inherits", sid, baseSID, 100)
			}

			// term_postings для символа
			rr.Postings = append(rr.Postings, buildSymPostings(sid, sym.Name, fq, norm)...)
		}

		// --- Imports rows + imports edges ---
		for _, imp := range rf.Imports {
			rr.Imports = append(rr.Imports, store.ImportRow{FileID: fid, Imp: imp})
			if targetKey, ok := cr.FileModuleMap[imp]; ok {
				addEdge(rr, edgeSeen, "imports", fid, store.FileID(targetKey), 100)
			}
		}

		// --- Calls edges (intra-file, оба символа должны быть в defined_map) ---
		localSyms := fileSymMap[rf.Key] // sym.Qualified → symbolID
		for _, call := range rf.Calls {
			if call.Caller == "" || call.Local == "" {
				continue
			}
			callerFQ := fullQualified(modPrefix, call.Caller)
			callerEntry, ok := cr.DefinedMap[callerFQ]
			if !ok {
				continue
			}
			callerSID := store.SymbolID(rf.Lang, callerFQ, callerEntry.FileKey, callerEntry.Line)

			calleeSID := resolveLocalCallee(call.Local, call.Caller, rf.Lang, localSyms)
			if calleeSID == "" {
				continue
			}
			addEdge(rr, edgeSeen, "calls", callerSID, calleeSID, 90)
		}

		// term_postings для файла
		rr.Postings = append(rr.Postings, buildFilePostings(fid, rf.Key, norm)...)
	}

	return rr
}

// buildFileSymMap строит per-file карту: fileKey → {sym.Qualified → symbolID}.
// Qualified здесь — без модульного префикса (как в парсере).
func buildFileSymMap(cr *CollectResult) map[string]map[string]string {
	m := make(map[string]map[string]string, len(cr.Files))
	for _, rf := range cr.Files {
		local := make(map[string]string, len(rf.Symbols))
		modPrefix := modulePrefix(rf.RelPath, rf.Lang)
		for _, sym := range rf.Symbols {
			fq := fullQualified(modPrefix, sym.Qualified)
			local[sym.Qualified] = store.SymbolID(rf.Lang, fq, rf.Key, sym.StartLine)
		}
		m[rf.Key] = local
	}
	return m
}

// resolveBase ищет базовый класс в defined_map.
// Пробует: точное совпадение, затем с модульным префиксом текущего файла.
func resolveBase(base, modPrefix string, definedMap map[string]DefinedEntry) string {
	if _, ok := definedMap[base]; ok {
		return base
	}
	if modPrefix != "" {
		fq := modPrefix + "." + base
		if _, ok := definedMap[fq]; ok {
			return fq
		}
	}
	return ""
}

// resolveLocalCallee возвращает symbolID для intra-file callee.
// Python: call.Local — простое имя (top-level) → ключ в localSyms.
// Java: call.Local — имя метода, call.Caller = "ClassName.method" → callee = "ClassName.calleeMethod".
func resolveLocalCallee(calleeLocal, callerQualified, lang string, localSyms map[string]string) string {
	switch lang {
	case "java":
		// Извлекаем класс из caller: "ClassName.method" → "ClassName"
		dot := strings.Index(callerQualified, ".")
		if dot < 0 {
			return ""
		}
		callerClass := callerQualified[:dot]
		calleeFull := callerClass + "." + calleeLocal
		return localSyms[calleeFull]
	default: // python
		return localSyms[calleeLocal]
	}
}

func addEdge(rr *ResolveResult, seen map[string]bool, typ, from, to string, conf int) {
	key := typ + ":" + from + ":" + to
	if seen[key] {
		return
	}
	seen[key] = true
	rr.Edges = append(rr.Edges, store.EdgeRow{
		Type:       typ,
		FromID:     from,
		ToID:       to,
		Confidence: conf,
	})
}

func buildSymPostings(sid, name, qualified string, norm *tokenize.Normalizer) []store.TermPosting {
	var out []store.TermPosting
	for _, term := range norm.Tokenize(name) {
		out = append(out, store.TermPosting{Term: term, DocID: sid, Weight: 100})
	}
	// Qualified добавляет токены с меньшим весом (только уникальные)
	nameToks := toSet(norm.Tokenize(name))
	for _, term := range norm.Tokenize(qualified) {
		if !nameToks[term] {
			out = append(out, store.TermPosting{Term: term, DocID: sid, Weight: 80})
		}
	}
	return out
}

func buildFilePostings(fid, key string, norm *tokenize.Normalizer) []store.TermPosting {
	var out []store.TermPosting
	for _, term := range norm.Tokenize(key) {
		out = append(out, store.TermPosting{Term: term, DocID: fid, Weight: 40})
	}
	return out
}

func toSet(terms []string) map[string]bool {
	s := make(map[string]bool, len(terms))
	for _, t := range terms {
		s[t] = true
	}
	return s
}
