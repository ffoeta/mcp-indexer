package treesitter

import (
	"mcp-indexer/internal/common/store"
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

// extract — entry point. Не использует state-объект между вызовами:
// каждый файл независим.
func (e *javaExtractor) extract(root *sitter.Node, src []byte) *parse.ParseResult {
	result := &parse.ParseResult{}
	importMap := map[string]string{} // simpleName → FQN

	for i := 0; i < int(root.NamedChildCount()); i++ {
		node := root.NamedChild(i)
		switch node.Type() {
		case "package_declaration":
			result.Package = e.extractPackage(node, src)
		case "import_declaration":
			e.extractImport(node, src, result, importMap)
		case "class_declaration", "interface_declaration", "enum_declaration":
			e.extractObject(node, src, result, importMap, "")
		}
	}
	return result
}

// ───────── package + imports ─────────

func (e *javaExtractor) extractPackage(node *sitter.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "scoped_identifier" || ch.Type() == "identifier" {
			return nodeText(ch, src)
		}
	}
	return ""
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
		ch := node.Child(i)
		if ch.Type() != "scoped_identifier" && ch.Type() != "type_identifier" {
			continue
		}
		raw := nodeText(ch, src)
		isWildcard := strings.HasSuffix(raw, ".*")
		raw = strings.TrimSuffix(raw, ".*")
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ".")
		simple := parts[len(parts)-1]
		result.Imports = append(result.Imports, parse.ImportRef{Raw: raw, Alias: simple})
		importMap[simple] = raw
		// import static com.x.Foo.bar — даём ещё один alias на класс Foo.
		if isStatic && !isWildcard && len(parts) >= 2 {
			classFQN := strings.Join(parts[:len(parts)-1], ".")
			classSimple := parts[len(parts)-2]
			if _, ok := importMap[classSimple]; !ok {
				importMap[classSimple] = classFQN
			}
		}
	}
}

// ───────── objects (class/interface/enum) ─────────

func (e *javaExtractor) extractObject(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string, parentFQN string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)

	fqn := name
	switch {
	case parentFQN != "":
		fqn = parentFQN + "." + name
	case result.Package != "":
		fqn = result.Package + "." + name
	}

	subkind := store.SubClass
	switch node.Type() {
	case "interface_declaration":
		subkind = store.SubInterface
	case "enum_declaration":
		subkind = store.SubEnum
	}

	bases := e.extractBases(node, src)
	doc := javaPrevDoc(node, src)

	result.Objects = append(result.Objects, parse.ObjectDef{
		Name:      name,
		FQN:       fqn,
		Subkind:   subkind,
		Bases:     bases,
		Doc:       doc,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})

	body := node.ChildByFieldName("body")
	if body == nil {
		return
	}
	e.walkClassBody(body, src, result, importMap, fqn)
}

func (e *javaExtractor) extractBases(node *sitter.Node, src []byte) []parse.BaseRef {
	var bases []parse.BaseRef
	if sup := node.ChildByFieldName("superclass"); sup != nil {
		for i := 0; i < int(sup.NamedChildCount()); i++ {
			ch := sup.NamedChild(i)
			if t := javaTypeName(ch, src); t != "" {
				bases = append(bases, parse.BaseRef{Name: t, Relation: store.RelExtends})
				break
			}
		}
	}
	if ifaces := node.ChildByFieldName("interfaces"); ifaces != nil {
		for i := 0; i < int(ifaces.NamedChildCount()); i++ {
			child := ifaces.NamedChild(i)
			// type_list внутри super_interfaces
			if child.Type() == "type_list" {
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if t := javaTypeName(child.NamedChild(j), src); t != "" {
						bases = append(bases, parse.BaseRef{Name: t, Relation: store.RelImplements})
					}
				}
			} else if t := javaTypeName(child, src); t != "" {
				bases = append(bases, parse.BaseRef{Name: t, Relation: store.RelImplements})
			}
		}
	}
	// extends (для interface) — поле extends_interfaces
	if ext := node.ChildByFieldName("extends_interfaces"); ext != nil {
		for i := 0; i < int(ext.NamedChildCount()); i++ {
			child := ext.NamedChild(i)
			if child.Type() == "type_list" {
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if t := javaTypeName(child.NamedChild(j), src); t != "" {
						bases = append(bases, parse.BaseRef{Name: t, Relation: store.RelExtends})
					}
				}
			}
		}
	}
	return bases
}

// walkClassBody проходит class_body / interface_body / enum_body.
func (e *javaExtractor) walkClassBody(body *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string, classFQN string) {
	// Сначала собираем var-types полей класса (нужны Pass 2 для этого класса).
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		if child.Type() == "field_declaration" {
			e.collectFieldVarTypes(child, src, result, classFQN)
		}
	}
	// Затем methods, constructors, nested objects.
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		switch child.Type() {
		case "method_declaration":
			e.extractMethod(child, src, result, importMap, classFQN, store.SubMethod)
		case "constructor_declaration":
			e.extractMethod(child, src, result, importMap, classFQN, store.SubCtor)
		case "class_declaration", "interface_declaration", "enum_declaration":
			e.extractObject(child, src, result, importMap, classFQN)
		}
	}
	// Synthetic <init> для field-initializer-ов и static/instance-блоков.
	e.collectInitCalls(body, src, result, classFQN)
}

// collectInitCalls создаёт synthetic method <init> на классе и атрибутирует
// к нему все вызовы из field initializers, static initializers и instance blocks.
// Если ничего из этого нет — synthetic method не создаётся.
func (e *javaExtractor) collectInitCalls(body *sitter.Node, src []byte, result *parse.ParseResult, classFQN string) {
	initFQN := classFQN + "." + parse.SyntheticInitName
	callsBefore := len(result.Calls)
	seen := map[string]bool{}

	startLine := int(body.StartPoint().Row) + 1
	endLine := int(body.EndPoint().Row) + 1

	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		switch child.Type() {
		case "field_declaration":
			// Walk variable_declarator's initializer expressions.
			for j := 0; j < int(child.NamedChildCount()); j++ {
				vd := child.NamedChild(j)
				if vd.Type() != "variable_declarator" {
					continue
				}
				// Initializer = любой named child кроме первого (name).
				for k := 1; k < int(vd.NamedChildCount()); k++ {
					e.walkMethodBody(vd.NamedChild(k), src, result, initFQN, seen)
				}
			}
		case "static_initializer", "block":
			e.walkMethodBody(child, src, result, initFQN, seen)
		}
	}

	if len(result.Calls) == callsBefore {
		return
	}
	result.Methods = append(result.Methods, parse.MethodDef{
		Name:      parse.SyntheticInitName,
		FQN:       initFQN,
		OwnerFQN:  classFQN,
		Subkind:   store.SubInit,
		Scope:     store.ScopeMember,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// ───────── methods + calls ─────────

func (e *javaExtractor) extractMethod(node *sitter.Node, src []byte, result *parse.ParseResult, importMap map[string]string, ownerFQN, subkind string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	fqn := ownerFQN + "." + name
	doc := javaPrevDoc(node, src)
	sig := javaSignature(node, src)

	result.Methods = append(result.Methods, parse.MethodDef{
		Name:      name,
		FQN:       fqn,
		OwnerFQN:  ownerFQN,
		Subkind:   subkind,
		Scope:     store.ScopeMember,
		Signature: sig,
		Doc:       doc,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
	})

	// formal parameters → var-types в scope метода
	if params := node.ChildByFieldName("parameters"); params != nil {
		for i := 0; i < int(params.NamedChildCount()); i++ {
			p := params.NamedChild(i)
			if p.Type() != "formal_parameter" {
				continue
			}
			pType := p.ChildByFieldName("type")
			pName := p.ChildByFieldName("name")
			if pType == nil || pName == nil {
				continue
			}
			t := javaTypeName(pType, src)
			if t == "" {
				continue
			}
			result.VarTypes = append(result.VarTypes, parse.VarType{
				ScopeFQN: fqn, VarName: nodeText(pName, src), TypeName: t,
			})
		}
	}

	// body: locals + calls
	body := node.ChildByFieldName("body")
	if body == nil {
		return
	}
	callSeen := map[string]bool{}
	e.walkMethodBody(body, src, result, fqn, callSeen)
}

// walkMethodBody обходит тело метода/конструктора, собирая calls и var-types.
func (e *javaExtractor) walkMethodBody(node *sitter.Node, src []byte, result *parse.ParseResult, callerFQN string, callSeen map[string]bool) {
	switch node.Type() {
	case "local_variable_declaration":
		e.collectLocalVarTypes(node, src, result, callerFQN)
	case "method_invocation":
		e.handleMethodInvocation(node, src, result, callerFQN, callSeen)
	case "object_creation_expression":
		e.handleObjectCreation(node, src, result, callerFQN, callSeen)
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		e.walkMethodBody(node.NamedChild(i), src, result, callerFQN, callSeen)
	}
}

func (e *javaExtractor) handleMethodInvocation(node *sitter.Node, src []byte, result *parse.ParseResult, callerFQN string, callSeen map[string]bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	calleeName := nodeText(nameNode, src)
	calleeOwner := ""

	if obj := node.ChildByFieldName("object"); obj != nil {
		switch obj.Type() {
		case "this", "super":
			// bare-receiver: caller's class будет искаться в Pass 1
		case "identifier", "type_identifier":
			calleeOwner = nodeText(obj, src)
		case "object_creation_expression":
			// new Logger().info(...) — owner = тип создаваемого объекта
			if t := obj.ChildByFieldName("type"); t != nil {
				calleeOwner = javaTypeName(t, src)
			}
		case "field_access":
			// this.repo.save() / a.b.save() — owner = имя последнего сегмента
			if f := obj.ChildByFieldName("field"); f != nil {
				calleeOwner = nodeText(f, src)
			}
		default:
			// chain: foo().bar() — owner = текст (resolution не сработает, но hint полезен)
			calleeOwner = nodeText(obj, src)
		}
	}

	addJavaCall(result, callSeen, parse.CallRef{
		CallerFQN:   callerFQN,
		CalleeName:  calleeName,
		CalleeOwner: calleeOwner,
		Line:        int(node.StartPoint().Row) + 1,
	})
}

func (e *javaExtractor) handleObjectCreation(node *sitter.Node, src []byte, result *parse.ParseResult, callerFQN string, callSeen map[string]bool) {
	t := node.ChildByFieldName("type")
	if t == nil {
		return
	}
	typeName := javaTypeName(t, src)
	if typeName == "" {
		return
	}
	// Для new pkg.Foo() — typeName == "pkg.Foo"; берём simple-part в callee_name,
	// остаток (если есть) в owner.
	calleeName := typeName
	calleeOwner := ""
	if i := strings.LastIndex(typeName, "."); i > 0 {
		calleeName = typeName[i+1:]
		calleeOwner = typeName[:i]
	}
	addJavaCall(result, callSeen, parse.CallRef{
		CallerFQN:   callerFQN,
		CalleeName:  calleeName,
		CalleeOwner: calleeOwner,
		Line:        int(node.StartPoint().Row) + 1,
	})
}

func addJavaCall(result *parse.ParseResult, seen map[string]bool, ref parse.CallRef) {
	key := ref.CallerFQN + "|" + ref.CalleeOwner + "|" + ref.CalleeName
	if seen[key] {
		return
	}
	seen[key] = true
	result.Calls = append(result.Calls, ref)
}

// ───────── var-types ─────────

func (e *javaExtractor) collectFieldVarTypes(node *sitter.Node, src []byte, result *parse.ParseResult, classFQN string) {
	tNode := node.ChildByFieldName("type")
	if tNode == nil {
		// fallback: первый named child, который не modifiers
		for i := 0; i < int(node.NamedChildCount()); i++ {
			ch := node.NamedChild(i)
			if ch.Type() != "modifiers" {
				tNode = ch
				break
			}
		}
	}
	if tNode == nil {
		return
	}
	typeName := javaTypeName(tNode, src)
	if typeName == "" {
		return
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch.Type() != "variable_declarator" {
			continue
		}
		nameNode := ch.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		result.VarTypes = append(result.VarTypes, parse.VarType{
			ScopeFQN: classFQN,
			VarName:  nodeText(nameNode, src),
			TypeName: typeName,
		})
	}
}

func (e *javaExtractor) collectLocalVarTypes(node *sitter.Node, src []byte, result *parse.ParseResult, scopeFQN string) {
	tNode := node.ChildByFieldName("type")
	if tNode == nil {
		// fallback: первый named child
		if node.NamedChildCount() > 0 {
			tNode = node.NamedChild(0)
		}
	}
	if tNode == nil {
		return
	}
	typeName := javaTypeName(tNode, src)
	if typeName == "" {
		return
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch.Type() != "variable_declarator" {
			continue
		}
		nameNode := ch.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		result.VarTypes = append(result.VarTypes, parse.VarType{
			ScopeFQN: scopeFQN,
			VarName:  nodeText(nameNode, src),
			TypeName: typeName,
		})
	}
}

// ───────── helpers ─────────

// javaTypeName извлекает имя типа из узла (без generic-параметров).
//   "Foo" → "Foo"
//   "List<Foo>" → "List"
//   "com.x.Foo" → "com.x.Foo"
//   "Foo[]" → "Foo"
func javaTypeName(n *sitter.Node, src []byte) string {
	switch n.Type() {
	case "type_identifier", "identifier", "scoped_type_identifier", "scoped_identifier":
		return nodeText(n, src)
	case "integral_type", "floating_point_type", "boolean_type", "void_type":
		return nodeText(n, src)
	case "generic_type":
		// generic_type { type_identifier, type_arguments } — берём корень
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			if ch.Type() != "type_arguments" {
				return javaTypeName(ch, src)
			}
		}
	case "array_type":
		if elem := n.ChildByFieldName("element"); elem != nil {
			return javaTypeName(elem, src)
		}
		if n.NamedChildCount() > 0 {
			return javaTypeName(n.NamedChild(0), src)
		}
	}
	return ""
}

// javaPrevDoc возвращает текст предыдущего block_comment-сиблинга,
// если он начинается с "/**" (Javadoc).
func javaPrevDoc(node *sitter.Node, src []byte) string {
	prev := node.PrevNamedSibling()
	if prev == nil || prev.Type() != "block_comment" {
		return ""
	}
	text := nodeText(prev, src)
	if !strings.HasPrefix(text, "/**") {
		return ""
	}
	// Снимаем /** и */
	body := strings.TrimPrefix(text, "/**")
	body = strings.TrimSuffix(body, "*/")
	// Берём первую содержательную строку.
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		s = strings.TrimPrefix(s, "*")
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > 120 {
			s = s[:120]
		}
		return s
	}
	return ""
}

// javaSignature собирает короткую signature метода (без тела).
func javaSignature(node *sitter.Node, src []byte) string {
	var ret string
	if t := node.ChildByFieldName("type"); t != nil {
		ret = nodeText(t, src) + " "
	}
	name := ""
	if nm := node.ChildByFieldName("name"); nm != nil {
		name = nodeText(nm, src)
	}
	params := ""
	if p := node.ChildByFieldName("parameters"); p != nil {
		params = nodeText(p, src)
	}
	return ret + name + params
}
