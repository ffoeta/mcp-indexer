package treesitter

import (
	"mcp-indexer/internal/indexer/parse"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

// NewJava возвращает Java парсер на базе tree-sitter.
func NewJava() *Parser {
	return &Parser{
		lang:      java.GetLanguage(),
		extractor: &javaExtractor{},
	}
}

type javaExtractor struct{}

func (e *javaExtractor) extract(root *sitter.Node, src []byte) *parse.ParseResult {
	result := &parse.ParseResult{}
	importMap := map[string]string{} // simpleName → fullClass
	seen := map[string]bool{}        // дедупликация call edges

	for i := 0; i < int(root.NamedChildCount()); i++ {
		node := root.NamedChild(i)
		switch node.Type() {
		case "import_declaration":
			e.extractImport(node, src, result, importMap)
		case "class_declaration":
			e.extractClass(node, src, result, importMap, seen)
		case "interface_declaration":
			e.extractInterface(node, src, result, importMap, seen)
		case "enum_declaration":
			e.extractEnum(node, src, result)
		}
	}

	return result
}

func (e *javaExtractor) extractImport(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string) {
	isStatic := false
	for i := 0; i < int(node.ChildCount()); i++ {
		if node.Child(i).Type() == "static" {
			isStatic = true
			break
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "scoped_identifier", "type_identifier":
			name := nodeText(child, src)
			isWildcard := strings.HasSuffix(name, ".*")
			name = strings.TrimSuffix(name, ".*")
			result.Imports = append(result.Imports, name)
			parts := strings.Split(name, ".")
			importMap[parts[len(parts)-1]] = name

			if isStatic && !isWildcard && len(parts) >= 2 {
				className := strings.Join(parts[:len(parts)-1], ".")
				classSimple := parts[len(parts)-2]
				if _, exists := importMap[classSimple]; !exists {
					importMap[classSimple] = className
				}
			}
		}
	}
}

func (e *javaExtractor) extractClass(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string, seen map[string]bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := nodeText(nameNode, src)

	var bases []string
	if superNode := node.ChildByFieldName("superclass"); superNode != nil {
		for i := 0; i < int(superNode.ChildCount()); i++ {
			child := superNode.Child(i)
			if child.Type() == "type_identifier" || child.Type() == "scoped_type_identifier" {
				bases = append(bases, nodeText(child, src))
				break
			}
		}
	}

	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      "class",
		Name:      className,
		Qualified: className,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
		Bases:     bases,
	})

	if bodyNode := node.ChildByFieldName("body"); bodyNode != nil {
		e.extractClassBody(bodyNode, src, result, className, importMap, seen)
	}
}

func (e *javaExtractor) extractInterface(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string, seen map[string]bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)

	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      "class",
		Name:      name,
		Qualified: name,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})

	if bodyNode := node.ChildByFieldName("body"); bodyNode != nil {
		e.extractClassBody(bodyNode, src, result, name, importMap, seen)
	}
}

func (e *javaExtractor) extractEnum(node *sitter.Node, src []byte, result *parse.ParseResult) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      "class",
		Name:      name,
		Qualified: name,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})
}

func (e *javaExtractor) extractClassBody(bodyNode *sitter.Node, src []byte, result *parse.ParseResult, className string, importMap map[string]string, seen map[string]bool) {
	// Collect local method names for intra-class call resolution
	localMethods := map[string]bool{}
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		switch child.Type() {
		case "method_declaration", "constructor_declaration":
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				localMethods[nodeText(nameNode, src)] = true
			}
		}
	}

	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		switch child.Type() {
		case "method_declaration":
			methodName := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				methodName = nodeText(nameNode, src)
			}
			e.extractMethod(child, src, result, className)
			if methodName != "" {
				caller := className + "." + methodName
				e.walkCalls(child, src, importMap, localMethods, result, seen, caller)
			}
		case "constructor_declaration":
			ctorName := ""
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				ctorName = nodeText(nameNode, src)
			}
			e.extractConstructor(child, src, result, className)
			if ctorName != "" {
				caller := className + "." + ctorName
				e.walkCalls(child, src, importMap, localMethods, result, seen, caller)
			}
		}
	}
}

func (e *javaExtractor) extractMethod(node *sitter.Node, src []byte, result *parse.ParseResult, className string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      "method",
		Name:      name,
		Qualified: className + "." + name,
		Parent:    className,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})
}

func (e *javaExtractor) extractConstructor(node *sitter.Node, src []byte, result *parse.ParseResult, className string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      "method",
		Name:      name,
		Qualified: className + "." + name,
		Parent:    className,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})
}

func (e *javaExtractor) walkCalls(node *sitter.Node, src []byte, importMap map[string]string, localMethods map[string]bool, result *parse.ParseResult, seen map[string]bool, caller string) {
	switch node.Type() {
	case "method_invocation":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			break
		}
		calleeName := nodeText(nameNode, src)
		objNode := node.ChildByFieldName("object")

		if objNode == nil || objNode.Type() == "this" {
			// Bare call or this.method() → potential intra-class call
			if localMethods[calleeName] {
				e.addLocalCall(calleeName, caller, int(node.StartPoint().Row)+1, result, seen)
			}
		} else if objNode.Type() == "type_identifier" || objNode.Type() == "identifier" {
			objName := nodeText(objNode, src)
			if fullName, ok := importMap[objName]; ok {
				e.addCall(fullName, caller, int(node.StartPoint().Row)+1, result, seen)
			}
		}

	case "object_creation_expression":
		typeNode := node.ChildByFieldName("type")
		if typeNode != nil {
			typeName := nodeText(typeNode, src)
			if fullName, ok := importMap[typeName]; ok {
				e.addCall(fullName, caller, int(node.StartPoint().Row)+1, result, seen)
			}
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		e.walkCalls(node.NamedChild(i), src, importMap, localMethods, result, seen, caller)
	}
}

func (e *javaExtractor) addCall(name string, caller string, line int, result *parse.ParseResult, seen map[string]bool) {
	key := caller + ":module:" + name
	if seen[key] {
		return
	}
	seen[key] = true
	result.Calls = append(result.Calls, parse.CallRef{
		Caller: caller,
		Line:   line,
		Module: name,
	})
}

func (e *javaExtractor) addLocalCall(callee string, caller string, line int, result *parse.ParseResult, seen map[string]bool) {
	key := caller + ":local:" + callee
	if seen[key] {
		return
	}
	seen[key] = true
	result.Calls = append(result.Calls, parse.CallRef{
		Caller: caller,
		Line:   line,
		Local:  callee,
	})
}
