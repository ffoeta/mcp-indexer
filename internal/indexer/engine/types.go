// Package engine реализует двухфазную индексацию:
// Phase 1 (collect) — парсинг файлов, построение карт символов.
// Phase 2 (resolve) — разрешение рёбер, запись в SQLite.
package engine

import "mcp-indexer/internal/indexer/parse"

// RawFile — сырые данные одного файла после парсинга.
type RawFile struct {
	Key     string
	AbsPath string
	RelPath string
	Lang    string
	Symbols []parse.SymbolDef
	Imports []string
	Calls   []parse.CallRef
}

// DefinedEntry описывает внутренний символ проекта.
type DefinedEntry struct {
	FileKey string `json:"file"`
	Line    int    `json:"line"`
	Kind    string `json:"kind"`
}

// CollectResult — выход Phase 1.
type CollectResult struct {
	Files         []*RawFile
	DefinedMap    map[string]DefinedEntry // fullQualified → entry
	FileModuleMap map[string]string        // importString → fileKey (для resolve imports)
	External      []string                 // импорты, не разрешённые во внутренние файлы
}
