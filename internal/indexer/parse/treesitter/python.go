package treesitter

import (
	"mcp-indexer/internal/indexer/parse"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// NewPython возвращает Python парсер на базе tree-sitter.
func NewPython() *Parser {
	return &Parser{
		lang:      python.GetLanguage(),
		extractor: &pyExtractor{},
	}
}

type pyExtractor struct{}

func (e *pyExtractor) extract(root *sitter.Node, src []byte) *parse.ParseResult {
	result := &parse.ParseResult{}
	importMap := map[string]string{} // alias → module
	localDefs := map[string]bool{}   // top-level имена для резолюции calls

	// Pass 1: top-level объявления
	for i := 0; i < int(root.NamedChildCount()); i++ {
		node := root.NamedChild(i)
		switch node.Type() {
		case "import_statement":
			e.extractImport(node, src, result, importMap)
		case "import_from_statement":
			e.extractFromImport(node, src, result, importMap)
		case "class_definition":
			e.extractClass(node, src, result, localDefs)
		case "function_definition", "async_function_definition":
			e.extractFunction(node, src, result, "", localDefs)
		case "decorated_definition":
			for j := 0; j < int(node.NamedChildCount()); j++ {
				child := node.NamedChild(j)
				switch child.Type() {
				case "class_definition":
					e.extractClass(child, src, result, localDefs)
				case "function_definition", "async_function_definition":
					e.extractFunction(child, src, result, "", localDefs)
				}
			}
		}
	}

	// Pass 2: call sites по всему дереву с отслеживанием scope
	seen := map[string]bool{}
	e.walkCalls(root, src, importMap, localDefs, result, seen, "")

	return result
}

func (e *pyExtractor) extractImport(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if node.FieldNameForChild(i) != "name" {
			continue
		}
		switch child.Type() {
		case "dotted_name":
			name := nodeText(child, src)
			result.Imports = append(result.Imports, name)
			alias := strings.SplitN(name, ".", 2)[0]
			importMap[alias] = name
		case "aliased_import":
			namePart := child.ChildByFieldName("name")
			aliasPart := child.ChildByFieldName("alias")
			if namePart == nil {
				continue
			}
			name := nodeText(namePart, src)
			result.Imports = append(result.Imports, name)
			if aliasPart != nil {
				importMap[nodeText(aliasPart, src)] = name
			} else {
				importMap[strings.SplitN(name, ".", 2)[0]] = name
			}
		}
	}
}

func (e *pyExtractor) extractFromImport(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string) {
	module := ""
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		fieldName := node.FieldNameForChild(i)

		switch fieldName {
		case "module_name":
			module = nodeText(child, src)
			if strings.HasPrefix(module, ".") {
				return // пропускаем relative imports
			}
			result.Imports = append(result.Imports, module)
		case "name":
			if module == "" {
				continue
			}
			switch child.Type() {
			case "dotted_name":
				importMap[nodeText(child, src)] = module
			case "aliased_import":
				aliasPart := child.ChildByFieldName("alias")
				namePart := child.ChildByFieldName("name")
				effective := ""
				if aliasPart != nil {
					effective = nodeText(aliasPart, src)
				} else if namePart != nil {
					effective = nodeText(namePart, src)
				}
				if effective != "" {
					importMap[effective] = module
				}
			}
		}
	}
}

func (e *pyExtractor) extractClass(node *sitter.Node, src []byte, result *parse.ParseResult, localDefs map[string]bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := nodeText(nameNode, src)
	localDefs[className] = true

	var bases []string
	superNode := node.ChildByFieldName("superclasses")
	if superNode != nil {
		for i := 0; i < int(superNode.NamedChildCount()); i++ {
			bases = append(bases, nodeText(superNode.NamedChild(i), src))
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

	bodyNode := node.ChildByFieldName("body")
	if bodyNode == nil {
		return
	}
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		switch child.Type() {
		case "function_definition", "async_function_definition":
			e.extractFunction(child, src, result, className, nil)
		case "decorated_definition":
			for j := 0; j < int(child.NamedChildCount()); j++ {
				def := child.NamedChild(j)
				if def.Type() == "function_definition" || def.Type() == "async_function_definition" {
					e.extractFunction(def, src, result, className, nil)
				}
			}
		}
	}
}

func (e *pyExtractor) extractFunction(node *sitter.Node, src []byte, result *parse.ParseResult, parentName string, localDefs map[string]bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	qualified := name
	kind := "function"
	if parentName != "" {
		qualified = parentName + "." + name
		kind = "method"
	} else if localDefs != nil {
		localDefs[name] = true
	}

	result.Symbols = append(result.Symbols, parse.SymbolDef{
		Kind:      kind,
		Name:      name,
		Qualified: qualified,
		Parent:    parentName,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})
}

// walkCalls рекурсивно обходит дерево, отслеживая scope вызывающего символа.
// scope — qualified name текущей функции/метода ("" = уровень файла).
func (e *pyExtractor) walkCalls(node *sitter.Node, src []byte, importMap map[string]string, localDefs map[string]bool, result *parse.ParseResult, seen map[string]bool, scope string) {
	switch node.Type() {
	case "call":
		funcNode := node.ChildByFieldName("function")
		if funcNode != nil {
			e.resolveCall(funcNode, src, importMap, localDefs, result, seen, int(node.StartPoint().Row)+1, scope)
		}
		// Рекурсия в аргументы (могут содержать вложенные вызовы)
		for i := 0; i < int(node.NamedChildCount()); i++ {
			e.walkCalls(node.NamedChild(i), src, importMap, localDefs, result, seen, scope)
		}
		return

	case "function_definition", "async_function_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			funcName := nodeText(nameNode, src)
			var newScope string
			// Если scope — имя класса (без точки), это метод
			if scope != "" && !strings.Contains(scope, ".") {
				newScope = scope + "." + funcName
			} else {
				newScope = funcName
			}
			for i := 0; i < int(node.NamedChildCount()); i++ {
				e.walkCalls(node.NamedChild(i), src, importMap, localDefs, result, seen, newScope)
			}
			return
		}

	case "class_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			className := nodeText(nameNode, src)
			for i := 0; i < int(node.NamedChildCount()); i++ {
				e.walkCalls(node.NamedChild(i), src, importMap, localDefs, result, seen, className)
			}
			return
		}
	}

	// Default: рекурсия с тем же scope
	for i := 0; i < int(node.NamedChildCount()); i++ {
		e.walkCalls(node.NamedChild(i), src, importMap, localDefs, result, seen, scope)
	}
}

func (e *pyExtractor) resolveCall(funcNode *sitter.Node, src []byte, importMap map[string]string, localDefs map[string]bool, result *parse.ParseResult, seen map[string]bool, line int, caller string) {
	name := nodeText(funcNode, src)

	// Находим корень цепочки атрибутов (для `os.path.join` корень — `os`)
	root := funcNode
	for root.Type() == "attribute" {
		obj := root.ChildByFieldName("object")
		if obj == nil {
			break
		}
		root = obj
	}
	rootName := nodeText(root, src)

	ref := parse.CallRef{Caller: caller, Line: line}
	var key string
	if mod, ok := importMap[rootName]; ok {
		ref.Module = mod
		key = caller + ":module:" + mod
	} else if localDefs[name] {
		ref.Local = name
		key = caller + ":local:" + name
	} else {
		key = caller + ":unresolved:" + name
	}

	if !seen[key] {
		seen[key] = true
		result.Calls = append(result.Calls, ref)
	}
}
