package treesitter

import (
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/indexer/parse"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func parsePy(t *testing.T, content string) *parse.ParseResult {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "x.py")
	writeFile(t, f, content)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return res
}

func findMethod(res *parse.ParseResult, fqn string) *parse.MethodDef {
	for i := range res.Methods {
		if res.Methods[i].FQN == fqn {
			return &res.Methods[i]
		}
	}
	return nil
}

func findObject(res *parse.ParseResult, fqn string) *parse.ObjectDef {
	for i := range res.Objects {
		if res.Objects[i].FQN == fqn {
			return &res.Objects[i]
		}
	}
	return nil
}

// ───────── synthetic <module> ─────────

func TestPython_SyntheticModuleAlwaysPresent(t *testing.T) {
	res := parsePy(t, ``)
	m := findMethod(res, parse.SyntheticModuleName)
	if m == nil {
		t.Fatal("synthetic <module> not found in empty file")
	}
	if m.Subkind != store.SubModule {
		t.Errorf("subkind=%q, want %q", m.Subkind, store.SubModule)
	}
}

// ───────── imports ─────────

func TestPython_Imports_Plain(t *testing.T) {
	res := parsePy(t, "import os\nimport sys\n")
	want := map[string]bool{"os": false, "sys": false}
	for _, imp := range res.Imports {
		if _, ok := want[imp.Raw]; ok {
			want[imp.Raw] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("import %q not found in %+v", k, res.Imports)
		}
	}
}

func TestPython_Imports_AliasAndFrom(t *testing.T) {
	res := parsePy(t, `
import os.path as op
from pkg.mod import foo, bar as baz
`)
	if len(res.Imports) == 0 {
		t.Fatal("no imports")
	}
	// alias записан в ImportRef
	gotOpAlias := false
	for _, i := range res.Imports {
		if i.Raw == "os.path" && i.Alias == "op" {
			gotOpAlias = true
		}
	}
	if !gotOpAlias {
		t.Errorf("alias 'op' for 'os.path' not captured: %+v", res.Imports)
	}
}

// ───────── objects + methods ─────────

func TestPython_Class_WithMethodsAndOwnerFQN(t *testing.T) {
	res := parsePy(t, `
class Greeter:
    """Says hi."""
    def __init__(self):
        pass
    def greet(self, name):
        pass
`)
	obj := findObject(res, "Greeter")
	if obj == nil {
		t.Fatal("Greeter object missing")
	}
	if obj.Subkind != store.SubClass {
		t.Errorf("subkind=%q, want class", obj.Subkind)
	}
	if obj.Doc != "Says hi." {
		t.Errorf("doc=%q, want 'Says hi.'", obj.Doc)
	}

	init := findMethod(res, "Greeter.__init__")
	if init == nil || init.OwnerFQN != "Greeter" || init.Subkind != store.SubCtor {
		t.Errorf("ctor missing or wrong owner/subkind: %+v", init)
	}
	greet := findMethod(res, "Greeter.greet")
	if greet == nil || greet.OwnerFQN != "Greeter" || greet.Subkind != store.SubMethod {
		t.Errorf("greet missing or wrong owner/subkind: %+v", greet)
	}
	if greet != nil && greet.Scope != store.ScopeMember {
		t.Errorf("greet scope=%q, want member", greet.Scope)
	}
}

func TestPython_TopLevelFunction_NoOwner(t *testing.T) {
	res := parsePy(t, `
def main():
    pass
`)
	m := findMethod(res, "main")
	if m == nil {
		t.Fatal("main not found")
	}
	if m.OwnerFQN != "" {
		t.Errorf("owner=%q, want empty", m.OwnerFQN)
	}
	if m.Subkind != store.SubFn {
		t.Errorf("subkind=%q, want fn", m.Subkind)
	}
	if m.Scope != store.ScopeGlobal {
		t.Errorf("scope=%q, want global", m.Scope)
	}
}

func TestPython_NestedClass(t *testing.T) {
	res := parsePy(t, `
class Outer:
    class Inner:
        def f(self):
            pass
`)
	if findObject(res, "Outer") == nil {
		t.Error("Outer missing")
	}
	if findObject(res, "Outer.Inner") == nil {
		t.Error("Outer.Inner missing")
	}
	if findMethod(res, "Outer.Inner.f") == nil {
		t.Error("Outer.Inner.f missing")
	}
}

func TestPython_ClassWithBases(t *testing.T) {
	res := parsePy(t, `
class Cat(Animal, Pettable):
    pass
`)
	obj := findObject(res, "Cat")
	if obj == nil {
		t.Fatal("Cat missing")
	}
	names := []string{}
	for _, b := range obj.Bases {
		names = append(names, b.Name)
		if b.Relation != store.RelExtends {
			t.Errorf("relation=%q, want extends", b.Relation)
		}
	}
	if len(names) != 2 || names[0] != "Animal" || names[1] != "Pettable" {
		t.Errorf("bases=%v, want [Animal Pettable]", names)
	}
}

// ───────── calls ─────────

func TestPython_ModuleLevelCall_AttributedToSyntheticModule(t *testing.T) {
	res := parsePy(t, `
import logging
logging.info("hello")
`)
	found := false
	for _, c := range res.Calls {
		if c.CallerFQN == parse.SyntheticModuleName && c.CalleeName == "info" && c.CalleeOwner == "logging" {
			found = true
		}
	}
	if !found {
		t.Errorf("module-level call not attributed to <module>: %+v", res.Calls)
	}
}

func TestPython_MethodCall_HasCorrectCaller(t *testing.T) {
	res := parsePy(t, `
class A:
    def x(self):
        self.y()

    def y(self):
        pass
`)
	found := false
	for _, c := range res.Calls {
		if c.CallerFQN == "A.x" && c.CalleeName == "y" {
			found = true
		}
	}
	if !found {
		t.Errorf("A.x → y call missing: %+v", res.Calls)
	}
}

func TestPython_Calls_Deduplicated(t *testing.T) {
	res := parsePy(t, `
import os
a = os.getenv("A")
b = os.getenv("B")
c = os.getenv("C")
`)
	count := 0
	for _, c := range res.Calls {
		if c.CallerFQN == parse.SyntheticModuleName && c.CalleeName == "getenv" && c.CalleeOwner == "os" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dedup failed: %d entries, want 1; calls=%+v", count, res.Calls)
	}
}

func TestPython_BareCall_NoOwner(t *testing.T) {
	res := parsePy(t, `
def helper():
    pass
def main():
    helper()
`)
	found := false
	for _, c := range res.Calls {
		if c.CallerFQN == "main" && c.CalleeName == "helper" && c.CalleeOwner == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("bare call helper() missing: %+v", res.Calls)
	}
}

// ───────── var-types ─────────

func TestPython_VarType_AnnotatedAssignment(t *testing.T) {
	res := parsePy(t, `
class Foo: pass
class Bar:
    def f(self):
        x: Foo = Foo()
`)
	found := false
	for _, vt := range res.VarTypes {
		if vt.ScopeFQN == "Bar.f" && vt.VarName == "x" && vt.TypeName == "Foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("var-type x:Foo in Bar.f missing: %+v", res.VarTypes)
	}
}

func TestPython_VarType_TypedParameter(t *testing.T) {
	res := parsePy(t, `
def handler(req: Request, count: int = 0):
    pass
`)
	hasReq, hasCount := false, false
	for _, vt := range res.VarTypes {
		if vt.ScopeFQN == "handler" && vt.VarName == "req" && vt.TypeName == "Request" {
			hasReq = true
		}
		if vt.ScopeFQN == "handler" && vt.VarName == "count" && vt.TypeName == "int" {
			hasCount = true
		}
	}
	if !hasReq {
		t.Errorf("typed parameter req:Request missing: %+v", res.VarTypes)
	}
	if !hasCount {
		t.Errorf("typed default param count:int missing: %+v", res.VarTypes)
	}
}

// ───────── error-paths ─────────

func TestPython_SyntaxError_ReturnsParseError(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "broken.py")
	writeFile(t, f, "def foo(\n  # unclosed\n")
	_, err := NewPython().Parse(f)
	if err == nil {
		t.Fatal("expected error for syntax error file")
	}
	pe, ok := err.(*parse.ParseError)
	if !ok {
		t.Fatalf("type=%T, want *parse.ParseError", err)
	}
	if pe.Line <= 0 {
		t.Errorf("expected line > 0, got %d", pe.Line)
	}
}

func TestPython_MissingFile_Errors(t *testing.T) {
	_, err := NewPython().Parse("/nonexistent/ghost.py")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestPython_EmptyFile_OnlyModule(t *testing.T) {
	res := parsePy(t, "")
	if len(res.Methods) != 1 || res.Methods[0].Name != parse.SyntheticModuleName {
		t.Errorf("empty file should have only <module> method, got %+v", res.Methods)
	}
	if len(res.Objects) != 0 || len(res.Calls) != 0 {
		t.Errorf("empty file should have no objects/calls: %+v", res)
	}
}
