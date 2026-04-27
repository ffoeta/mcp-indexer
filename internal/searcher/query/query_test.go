package query

import (
	"database/sql"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/engine"
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/parse/treesitter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ───────── helpers ─────────

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupIndex(t *testing.T, files map[string]string) *sql.DB {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, root, rel, content)
	}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := services.DefaultConfig()
	matcher := &services.Matcher{}
	parsers := map[string]parse.Parser{
		".py":   treesitter.NewPython(),
		".java": treesitter.NewJava(),
	}
	norm := tokenize.New(tokenize.DefaultStopSet())
	if err := engine.Index(s.DB(), root, &cfg, matcher, parsers, norm, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}

func normalize(s string) string {
	n := tokenize.New(tokenize.DefaultStopSet())
	return strings.Join(n.Tokenize(s), " ")
}

// ───────── ParseShortID / ResolveID ─────────

func TestParseShortID(t *testing.T) {
	cases := []struct {
		in    string
		kind  string
		num   int64
		ok    bool
	}{
		{"m412", store.KindMethod, 412, true},
		{"o7", store.KindObject, 7, true},
		{"f3", "file", 3, true},
		{"x12", "", 0, false},
		{"m", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		k, n, ok := ParseShortID(c.in)
		if k != c.kind || n != c.num || ok != c.ok {
			t.Errorf("ParseShortID(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.in, k, n, ok, c.kind, c.num, c.ok)
		}
	}
}

func TestResolveID_FromShort(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": "class Foo:\n    def bar(self):\n        pass\n",
	})

	// Найдём short_id класса Foo
	var fooShort int64
	db.QueryRow(`SELECT short_id FROM nodes WHERE name='Foo'`).Scan(&fooShort)
	if fooShort == 0 {
		t.Fatal("Foo node not indexed")
	}

	cid, kind, err := ResolveID(db, "o"+itoa(fooShort))
	if err != nil {
		t.Fatal(err)
	}
	if kind != store.DocNode || !strings.HasPrefix(cid, "n:") {
		t.Errorf("unexpected: cid=%q kind=%q", cid, kind)
	}

	// Canonical напрямую
	cid2, kind2, err := ResolveID(db, cid)
	if err != nil {
		t.Fatal(err)
	}
	if cid2 != cid || kind2 != kind {
		t.Errorf("canonical passthrough failed")
	}
}

// ───────── FTSearch ─────────

func TestFTSearch_FindsByName(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"svc.py": `
class OrderService:
    def process_order(self):
        pass
`,
	})
	hits, err := FTSearch(db, normalize("order"), "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits for 'order'")
	}
	foundService := false
	for _, h := range hits {
		if h.Name == "OrderService" {
			foundService = true
		}
	}
	if !foundService {
		t.Errorf("OrderService not in hits: %+v", hits)
	}
}

func TestFTSearch_KindFilter(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"order.py": "class Order:\n    pass\n",
	})
	hits, err := FTSearch(db, normalize("order"), store.KindObject, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.DocKind != store.DocNode {
			t.Errorf("expected only nodes, got file: %+v", h)
		}
	}
	if len(hits) == 0 {
		t.Errorf("expected ≥1 object hit")
	}
}

// ───────── GetNode ─────────

func TestGetNode_Object_Counts(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
class Animal:
    def eat(self):
        pass

class Cat(Animal):
    def meow(self):
        pass
`,
	})
	var catID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='Cat'`).Scan(&catID)
	if catID == "" {
		t.Fatal("Cat not indexed")
	}
	n, err := GetNode(db, catID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != store.KindObject || n.MethodCount != 1 {
		t.Errorf("Cat: kind=%q methods=%d, want object/1", n.Kind, n.MethodCount)
	}
	if n.ExtendsCount != 1 {
		t.Errorf("Cat extends should be 1: %+v", n)
	}
}

func TestGetNode_Method_Counts(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
def helper():
    pass

def main():
    helper()
    helper()  # dedup → 1
`,
	})
	var helperID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='helper'`).Scan(&helperID)
	n, err := GetNode(db, helperID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != store.KindMethod {
		t.Errorf("kind=%q", n.Kind)
	}
	if n.CalledByCount != 1 {
		t.Errorf("helper called_by=%d, want 1", n.CalledByCount)
	}
}

// ───────── GetFile ─────────

func TestGetFile_PythonOverview(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"main.py": `
import os
import logging

def helper():
    pass

class Service:
    def run(self):
        pass
`,
	})
	fi, err := GetFile(db, "main.py")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Lang != "python" {
		t.Errorf("lang=%q", fi.Lang)
	}
	if len(fi.Imports) < 2 {
		t.Errorf("imports=%d, want ≥2", len(fi.Imports))
	}
	if len(fi.Objects) != 1 || fi.Objects[0].Name != "Service" {
		t.Errorf("objects=%+v", fi.Objects)
	}
	if fi.Objects[0].MethodCount != 1 {
		t.Errorf("Service methods=%d, want 1", fi.Objects[0].MethodCount)
	}
	// methods top-level: <module> + helper
	hasModule, hasHelper := false, false
	for _, m := range fi.Methods {
		if m.Name == parse.SyntheticModuleName {
			hasModule = true
		}
		if m.Name == "helper" {
			hasHelper = true
		}
	}
	if !hasModule || !hasHelper {
		t.Errorf("top-level methods missing: %+v", fi.Methods)
	}
}

// ───────── Walk ─────────

func TestWalk_CallsIn(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
def helper():
    pass

def caller():
    helper()
`,
	})
	var helperID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='helper'`).Scan(&helperID)

	res, err := Walk(db, helperID, EdgeCalls, DirIn, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].OtherName != "caller" {
		t.Errorf("expected 1 caller, got %+v", res.Items)
	}
}

func TestWalk_CallsOut_ResolvedAndUnresolved(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
def helper():
    pass

def caller():
    helper()
    external_unknown_func()
`,
	})
	var callerID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='caller'`).Scan(&callerID)

	res, err := Walk(db, callerID, EdgeCalls, DirOut, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) < 2 {
		t.Fatalf("expected ≥2 outgoing, got %+v", res.Items)
	}
	resolvedCount, unresolvedCount := 0, 0
	for _, w := range res.Items {
		if w.OtherID != "" {
			resolvedCount++
		} else {
			unresolvedCount++
			if w.Hint == "" {
				t.Error("unresolved should have non-empty hint")
			}
		}
	}
	if resolvedCount == 0 {
		t.Error("expected resolved call to helper")
	}
	if unresolvedCount == 0 {
		t.Error("expected unresolved external call")
	}
}

func TestWalk_InheritsOut(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
class Animal:
    pass
class Cat(Animal):
    pass
`,
	})
	var catID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='Cat'`).Scan(&catID)
	res, err := Walk(db, catID, EdgeInherits, DirOut, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Hint != "Animal" {
		t.Errorf("expected Animal parent, got %+v", res.Items)
	}
	if res.Items[0].OtherName != "Animal" {
		t.Errorf("parent should be resolved to Animal node")
	}
}

func TestWalk_DefinesOut_ObjectMethods(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
class Service:
    def a(self): pass
    def b(self): pass
    def c(self): pass
`,
	})
	var svcID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='Service'`).Scan(&svcID)
	res, err := Walk(db, svcID, EdgeDefines, DirOut, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Errorf("expected 3 methods, got %+v", res.Items)
	}
}

func TestWalk_ImportsOut(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": "from b import Foo\n",
		"b.py": "class Foo: pass\n",
	})
	var aID string
	db.QueryRow(`SELECT file_id FROM files WHERE rel_path='a.py'`).Scan(&aID)
	res, err := Walk(db, aID, EdgeImports, DirOut, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 import, got %+v", res.Items)
	}
	if res.Items[0].OtherID == "" {
		t.Errorf("import should be resolved to b.py: %+v", res.Items[0])
	}
}

// ───────── GetCodeRange ─────────

func TestGetCodeRange(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
def foo():
    pass

class Bar:
    def m(self):
        pass
`,
	})
	var fooID string
	db.QueryRow(`SELECT node_id FROM nodes WHERE name='foo'`).Scan(&fooID)
	cr, err := GetCodeRange(db, fooID)
	if err != nil {
		t.Fatal(err)
	}
	if cr.FilePath != "a.py" || cr.StartLine == 0 {
		t.Errorf("bad code range: %+v", cr)
	}
}

// ───────── GetStats ─────────

func TestGetStats_NonZero(t *testing.T) {
	db := setupIndex(t, map[string]string{
		"a.py": `
class Foo:
    def m(self):
        pass
`,
	})
	s, err := GetStats(db)
	if err != nil {
		t.Fatal(err)
	}
	if s.Files == 0 || s.Objects == 0 || s.Methods == 0 {
		t.Errorf("counts zero: %+v", s)
	}
}

// ───────── itoa for short IDs ─────────

func itoa(n int64) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == ' ' {
			return -1
		}
		return r
	}, formatInt(n)))
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
