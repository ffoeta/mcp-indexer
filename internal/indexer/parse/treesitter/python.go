package treesitter

import (
	"mcp-indexer/internal/common/store"
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

// pyState — рабочее состояние одного файла.
type pyState struct {
	importMap map[string]string // alias → module (например "od" → "os.path")
	topLevel  map[string]bool   // top-level имена (класс/функция/var)
}

func (e *pyExtractor) extract(root *sitter.Node, src []byte) *parse.ParseResult {
	result := &parse.ParseResult{}
	state := &pyState{
		importMap: map[string]string{},
		topLevel:  map[string]bool{},
	}

	// Synthetic module-method: к нему атрибутируем все module-level вызовы.
	endLine := int(root.EndPoint().Row) + 1
	if endLine < 1 {
		endLine = 1
	}
	result.Methods = append(result.Methods, parse.MethodDef{
		Name:      parse.SyntheticModuleName,
		FQN:       parse.SyntheticModuleName,
		Subkind:   store.SubModule,
		Scope:     store.ScopeGlobal,
		StartLine: 1,
		EndLine:   endLine,
	})

	// Pass 1: top-level declarations + imports
	for i := 0; i < int(root.NamedChildCount()); i++ {
		e.handleTopLevel(root.NamedChild(i), src, result, state)
	}

	// Pass 2: рекурсивный обход для calls + var-types внутри функций.
	// callerFQN на module-level — synthetic <module>.
	callSeen := map[string]bool{}
	e.walkBody(root, src, result, state, parse.SyntheticModuleName, callSeen)

	return result
}

// ───────── Pass 1: top-level ─────────

func (e *pyExtractor) handleTopLevel(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState) {
	switch node.Type() {
	case "import_statement":
		e.parseImport(node, src, result, st)
	case "import_from_statement":
		e.parseFromImport(node, src, result, st)
	case "class_definition":
		e.parseClass(node, src, result, st, "")
	case "function_definition", "async_function_definition":
		e.parseFunction(node, src, result, st, "" /*ownerFQN*/, store.SubFn)
	case "decorated_definition":
		for j := 0; j < int(node.NamedChildCount()); j++ {
			child := node.NamedChild(j)
			switch child.Type() {
			case "class_definition":
				e.parseClass(child, src, result, st, "")
			case "function_definition", "async_function_definition":
				e.parseFunction(child, src, result, st, "", store.SubFn)
			}
		}
	case "expression_statement":
		// аннотированное присваивание на уровне модуля: x: Foo = ...
		for j := 0; j < int(node.NamedChildCount()); j++ {
			ch := node.NamedChild(j)
			if ch.Type() == "assignment" || ch.Type() == "annotated_assignment" {
				e.collectVarType(ch, src, result, "" /*scope=file-level*/)
			}
		}
	}
}

func (e *pyExtractor) parseImport(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if node.FieldNameForChild(i) != "name" {
			continue
		}
		switch child.Type() {
		case "dotted_name":
			name := nodeText(child, src)
			alias := strings.SplitN(name, ".", 2)[0]
			result.Imports = append(result.Imports, parse.ImportRef{Raw: name, Alias: alias})
			st.importMap[alias] = name
		case "aliased_import":
			namePart := child.ChildByFieldName("name")
			aliasPart := child.ChildByFieldName("alias")
			if namePart == nil {
				continue
			}
			name := nodeText(namePart, src)
			alias := strings.SplitN(name, ".", 2)[0]
			if aliasPart != nil {
				alias = nodeText(aliasPart, src)
			}
			result.Imports = append(result.Imports, parse.ImportRef{Raw: name, Alias: alias})
			st.importMap[alias] = name
		}
	}
}

func (e *pyExtractor) parseFromImport(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState) {
	module := ""
	hasName := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		fieldName := node.FieldNameForChild(i)
		switch fieldName {
		case "module_name":
			module = nodeText(child, src)
		case "name":
			if module == "" {
				continue
			}
			switch child.Type() {
			case "dotted_name":
				name := nodeText(child, src)
				st.importMap[name] = module
				result.Imports = append(result.Imports, parse.ImportRef{Raw: module, Alias: name})
				hasName = true
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
					st.importMap[effective] = module
					result.Imports = append(result.Imports, parse.ImportRef{Raw: module, Alias: effective})
					hasName = true
				}
			}
		}
	}
	// Если from-import без явных имён (parse error / wildcard) — добавим пустой alias
	// чтобы хотя бы file-edge на модуль создалось.
	if module != "" && !hasName {
		result.Imports = append(result.Imports, parse.ImportRef{Raw: module, Alias: ""})
	}
}

func (e *pyExtractor) parseClass(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState, parentFQN string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	fqn := name
	if parentFQN != "" {
		fqn = parentFQN + "." + name
	}
	if parentFQN == "" {
		st.topLevel[name] = true
	}

	var bases []parse.BaseRef
	if superNode := node.ChildByFieldName("superclasses"); superNode != nil {
		for i := 0; i < int(superNode.NamedChildCount()); i++ {
			b := nodeText(superNode.NamedChild(i), src)
			if b == "" {
				continue
			}
			bases = append(bases, parse.BaseRef{Name: b, Relation: store.RelExtends})
		}
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	body := node.ChildByFieldName("body")
	doc := pyLeadingDoc(body, src)

	result.Objects = append(result.Objects, parse.ObjectDef{
		Name:      name,
		FQN:       fqn,
		Subkind:   store.SubClass,
		Bases:     bases,
		Doc:       doc,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Тело класса: методы + nested классы.
	if body == nil {
		return
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		ch := body.NamedChild(i)
		switch ch.Type() {
		case "function_definition", "async_function_definition":
			subk := store.SubMethod
			if nm := ch.ChildByFieldName("name"); nm != nil && nodeText(nm, src) == "__init__" {
				subk = store.SubCtor
			}
			e.parseFunction(ch, src, result, st, fqn, subk)
		case "decorated_definition":
			for j := 0; j < int(ch.NamedChildCount()); j++ {
				def := ch.NamedChild(j)
				if def.Type() == "function_definition" || def.Type() == "async_function_definition" {
					e.parseFunction(def, src, result, st, fqn, store.SubMethod)
				} else if def.Type() == "class_definition" {
					e.parseClass(def, src, result, st, fqn)
				}
			}
		case "class_definition":
			e.parseClass(ch, src, result, st, fqn)
		}
	}
}

func (e *pyExtractor) parseFunction(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState, ownerFQN, subkind string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nodeText(nameNode, src)
	fqn := name
	scope := store.ScopeGlobal
	if ownerFQN != "" {
		fqn = ownerFQN + "." + name
		scope = store.ScopeMember
	} else {
		st.topLevel[name] = true
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	body := node.ChildByFieldName("body")
	doc := pyLeadingDoc(body, src)

	sig := pySignature(node, src)

	result.Methods = append(result.Methods, parse.MethodDef{
		Name:      name,
		FQN:       fqn,
		OwnerFQN:  ownerFQN,
		Subkind:   subkind,
		Scope:     scope,
		Signature: sig,
		Doc:       doc,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Var-types из аннотированных параметров.
	if params := node.ChildByFieldName("parameters"); params != nil {
		e.collectParamTypes(params, src, result, fqn)
	}
}

// ───────── Pass 2: walkBody ─────────

// walkBody обходит произвольное поддерево, собирая calls и var-types
// в текущем scope (callerFQN — FQN method/function-обёртки).
func (e *pyExtractor) walkBody(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState, callerFQN string, callSeen map[string]bool) {
	switch node.Type() {
	case "function_definition", "async_function_definition":
		nm := node.ChildByFieldName("name")
		if nm == nil {
			return
		}
		funcName := nodeText(nm, src)
		// Внутри тела функции callerFQN = вложенное имя.
		// Имитируем nested-FQN: если callerFQN — synthetic <module> или функция, опускаем prefix; если класс — это уже отработано в parseClass.
		newCaller := funcName
		if callerFQN != parse.SyntheticModuleName && callerFQN != "" {
			newCaller = callerFQN + "." + funcName
		}
		if body := node.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.NamedChildCount()); i++ {
				e.walkBody(body.NamedChild(i), src, result, st, newCaller, callSeen)
			}
		}
		return

	case "class_definition":
		nm := node.ChildByFieldName("name")
		if nm == nil {
			return
		}
		clsName := nodeText(nm, src)
		newCaller := clsName
		if callerFQN != parse.SyntheticModuleName && callerFQN != "" {
			newCaller = callerFQN + "." + clsName
		}
		if body := node.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.NamedChildCount()); i++ {
				e.walkBody(body.NamedChild(i), src, result, st, newCaller, callSeen)
			}
		}
		return

	case "call":
		e.handleCall(node, src, result, callerFQN, callSeen)
		// args могут содержать nested-вызовы
		for i := 0; i < int(node.NamedChildCount()); i++ {
			e.walkBody(node.NamedChild(i), src, result, st, callerFQN, callSeen)
		}
		return

	case "annotated_assignment":
		e.collectVarType(node, src, result, scopeFromCaller(callerFQN))
	case "assignment":
		// без аннотации — но left=Name, right=call → выводим тип, если call в importMap
		e.collectAssignedType(node, src, result, st, scopeFromCaller(callerFQN))
	}

	// Default: рекурсия
	for i := 0; i < int(node.NamedChildCount()); i++ {
		e.walkBody(node.NamedChild(i), src, result, st, callerFQN, callSeen)
	}
}

func scopeFromCaller(caller string) string {
	if caller == parse.SyntheticModuleName {
		return ""
	}
	return caller
}

func (e *pyExtractor) handleCall(node *sitter.Node, src []byte, result *parse.ParseResult, callerFQN string, callSeen map[string]bool) {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return
	}
	line := int(node.StartPoint().Row) + 1

	calleeName, calleeOwner := pyCalleeParts(funcNode, src)
	if calleeName == "" {
		return
	}

	// Дедупликация: один callee на одного caller записывается один раз.
	key := callerFQN + "|" + calleeOwner + "|" + calleeName
	if callSeen[key] {
		return
	}
	callSeen[key] = true

	result.Calls = append(result.Calls, parse.CallRef{
		CallerFQN:   callerFQN,
		CalleeName:  calleeName,
		CalleeOwner: calleeOwner,
		Line:        line,
	})
}

// pyCalleeParts разделяет callee-выражение на (name, owner).
// "foo()" → ("foo", "")
// "obj.foo()" → ("foo", "obj")
// "a.b.foo()" → ("foo", "a.b")
func pyCalleeParts(funcNode *sitter.Node, src []byte) (name, owner string) {
	if funcNode.Type() == "attribute" {
		obj := funcNode.ChildByFieldName("object")
		attr := funcNode.ChildByFieldName("attribute")
		if attr == nil {
			return "", ""
		}
		name = nodeText(attr, src)
		if obj != nil {
			owner = nodeText(obj, src)
		}
		return
	}
	// identifier или иной — bare call
	return nodeText(funcNode, src), ""
}

// ───────── var-types ─────────

// collectVarType собирает тип из annotated_assignment: `x: Foo = ...` или `x: Foo`.
func (e *pyExtractor) collectVarType(node *sitter.Node, src []byte, result *parse.ParseResult, scopeFQN string) {
	// dive в annotated_assignment если узел — обёртка
	if node.Type() == "expression_statement" && node.NamedChildCount() > 0 {
		node = node.NamedChild(0)
	}
	if node.Type() != "annotated_assignment" {
		return
	}
	target := node.ChildByFieldName("target")
	annot := node.ChildByFieldName("type")
	if target == nil || annot == nil {
		return
	}
	if target.Type() != "identifier" {
		return
	}
	varName := nodeText(target, src)
	typeName := pyTypeName(annot, src)
	if typeName == "" {
		return
	}
	result.VarTypes = append(result.VarTypes, parse.VarType{
		ScopeFQN: scopeFQN,
		VarName:  varName,
		TypeName: typeName,
	})
}

// collectAssignedType — assignment вида `x = Foo(...)`: тип переменной = Foo (если importMap его знает).
func (e *pyExtractor) collectAssignedType(node *sitter.Node, src []byte, result *parse.ParseResult, st *pyState, scopeFQN string) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil || left.Type() != "identifier" {
		return
	}
	if right.Type() != "call" {
		return
	}
	funcNode := right.ChildByFieldName("function")
	if funcNode == nil {
		return
	}
	calleeName := nodeText(funcNode, src)
	// Берём только если это известный класс (importMap или topLevel)
	if !st.topLevel[calleeName] {
		if _, ok := st.importMap[calleeName]; !ok {
			return
		}
	}
	result.VarTypes = append(result.VarTypes, parse.VarType{
		ScopeFQN: scopeFQN,
		VarName:  nodeText(left, src),
		TypeName: calleeName,
	})
}

// collectParamTypes — типы параметров функции (для Pass 2 в engine).
func (e *pyExtractor) collectParamTypes(params *sitter.Node, src []byte, result *parse.ParseResult, scopeFQN string) {
	for i := 0; i < int(params.NamedChildCount()); i++ {
		p := params.NamedChild(i)
		switch p.Type() {
		case "typed_parameter":
			// child 0 — identifier, type field — annotation
			var name string
			if p.NamedChildCount() > 0 {
				name = nodeText(p.NamedChild(0), src)
			}
			annot := p.ChildByFieldName("type")
			if name == "" || annot == nil {
				continue
			}
			t := pyTypeName(annot, src)
			if t == "" {
				continue
			}
			result.VarTypes = append(result.VarTypes, parse.VarType{
				ScopeFQN: scopeFQN, VarName: name, TypeName: t,
			})
		case "typed_default_parameter":
			// grammar не выставляет field "name" — берём первый named-child (identifier)
			var name string
			if p.NamedChildCount() > 0 && p.NamedChild(0).Type() == "identifier" {
				name = nodeText(p.NamedChild(0), src)
			}
			annot := p.ChildByFieldName("type")
			if name == "" || annot == nil {
				continue
			}
			t := pyTypeName(annot, src)
			if t == "" {
				continue
			}
			result.VarTypes = append(result.VarTypes, parse.VarType{
				ScopeFQN: scopeFQN, VarName: name, TypeName: t,
			})
		}
	}
}

// pyTypeName возвращает «корневое» простое имя типа из аннотации.
// "Foo" → "Foo"; "List[Foo]" → "List"; "pkg.Foo" → "pkg.Foo".
func pyTypeName(annot *sitter.Node, src []byte) string {
	switch annot.Type() {
	case "type":
		// wrapper-узел в parameters: разворачиваем
		if annot.NamedChildCount() > 0 {
			return pyTypeName(annot.NamedChild(0), src)
		}
	case "identifier", "dotted_name":
		return nodeText(annot, src)
	case "subscript":
		// generic: "List[Foo]" — берём корневое имя
		val := annot.ChildByFieldName("value")
		if val != nil {
			return pyTypeName(val, src)
		}
	case "attribute":
		return nodeText(annot, src)
	}
	return ""
}

// ───────── helpers ─────────

// pyLeadingDoc возвращает первую строку docstring тела (≤120 chars).
func pyLeadingDoc(body *sitter.Node, src []byte) string {
	if body == nil || body.NamedChildCount() == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first.Type() != "expression_statement" || first.NamedChildCount() == 0 {
		return ""
	}
	str := first.NamedChild(0)
	if str.Type() != "string" {
		return ""
	}
	raw := nodeText(str, src)
	return cleanDoc(raw)
}

// pySignature формирует «def name(params) -> ret» (best-effort, без тела).
func pySignature(fn *sitter.Node, src []byte) string {
	name := fn.ChildByFieldName("name")
	params := fn.ChildByFieldName("parameters")
	ret := fn.ChildByFieldName("return_type")
	var b strings.Builder
	if fn.Type() == "async_function_definition" {
		b.WriteString("async ")
	}
	b.WriteString("def ")
	if name != nil {
		b.WriteString(nodeText(name, src))
	}
	if params != nil {
		b.WriteString(nodeText(params, src))
	}
	if ret != nil {
		b.WriteString(" -> ")
		b.WriteString(nodeText(ret, src))
	}
	return b.String()
}

// cleanDoc обрезает кавычки и берёт первую непустую строку (до 120 chars).
func cleanDoc(raw string) string {
	s := raw
	// Снимаем тройные/одинарные кавычки.
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		s = strings.TrimPrefix(s, q)
		s = strings.TrimSuffix(s, q)
	}
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
