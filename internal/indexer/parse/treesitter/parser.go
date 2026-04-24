package treesitter

import (
	"context"
	"mcp-indexer/internal/indexer/parse"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
)

type extractor interface {
	extract(root *sitter.Node, src []byte) *parse.ParseResult
}

// Parser — tree-sitter реализация parse.Parser.
type Parser struct {
	lang      *sitter.Language
	extractor extractor
}

func (p *Parser) Parse(absPath string) (*parse.ParseResult, error) {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &parse.ParseError{Message: err.Error()}
	}

	sp := sitter.NewParser()
	sp.SetLanguage(p.lang)

	tree, err := sp.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, &parse.ParseError{Message: err.Error()}
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		line := findErrorLine(root)
		return nil, &parse.ParseError{Message: "syntax error", Line: line}
	}

	return p.extractor.extract(root, src), nil
}

// nodeText возвращает текст узла из исходного кода.
func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

func findErrorLine(node *sitter.Node) int {
	if node.Type() == "ERROR" || node.IsMissing() {
		return int(node.StartPoint().Row) + 1
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if line := findErrorLine(node.Child(i)); line > 0 {
			return line
		}
	}
	return 0
}
