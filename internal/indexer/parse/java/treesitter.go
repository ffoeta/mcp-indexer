package java

import (
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/parse/treesitter"
)

// New возвращает Java парсер на базе tree-sitter.
func New() parse.Parser {
	return treesitter.NewJava()
}

// JavaParser оставлен для совместимости.
type JavaParser struct{}

func (p *JavaParser) Parse(absPath string) (*parse.ParseResult, error) {
	return treesitter.NewJava().Parse(absPath)
}
