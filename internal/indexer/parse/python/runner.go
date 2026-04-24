package python

import (
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/parse/treesitter"
)

// New возвращает Python парсер на базе tree-sitter.
// scriptPath игнорируется (оставлен для совместимости API).
func New(_ string) parse.Parser {
	return treesitter.NewPython()
}
