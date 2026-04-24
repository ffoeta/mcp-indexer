package python

import (
	"mcp-indexer/internal/indexer/parse"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.WriteFile(path, []byte(content), 0o644)
}

func TestRunner_Parse_ValidFile(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "hello.py")
	writeFile(t, pyFile, `
import os
import sys

class Greeter:
    def greet(self, name):
        pass

def main():
    pass
`)
	r := New("")
	res, err := r.Parse(pyFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	foundOs := false
	for _, imp := range res.Imports {
		if imp == "os" {
			foundOs = true
		}
	}
	if !foundOs {
		t.Errorf("expected 'os' in imports, got %v", res.Imports)
	}
	foundGreeter := false
	for _, sym := range res.Symbols {
		if sym.Name == "Greeter" {
			foundGreeter = true
		}
	}
	if !foundGreeter {
		t.Errorf("expected Greeter symbol, got %v", res.Symbols)
	}
}

func TestRunner_Parse_SyntaxError_ReturnsParseError(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "broken.py")
	writeFile(t, pyFile, "def foo(\n  # unclosed\n")

	r := New("")
	_, err := r.Parse(pyFile)
	if err == nil {
		t.Fatal("expected error for syntax error file")
	}
	pe, ok := err.(*parse.ParseError)
	if !ok {
		t.Fatalf("expected *parse.ParseError, got %T: %v", err, err)
	}
	if pe.Line <= 0 {
		t.Errorf("expected line > 0 for syntax error, got %d", pe.Line)
	}
}

func TestRunner_Parse_MethodsUnderClass(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "cls.py")
	writeFile(t, pyFile, `
class MyClass:
    def method_a(self):
        pass
    def method_b(self):
        pass
`)
	r := New("")
	res, err := r.Parse(pyFile)
	if err != nil {
		t.Fatal(err)
	}
	foundQualified := false
	for _, sym := range res.Symbols {
		if sym.Qualified == "MyClass.method_a" {
			foundQualified = true
		}
	}
	if !foundQualified {
		t.Errorf("expected MyClass.method_a in symbols, got %v", res.Symbols)
	}
}

func TestRunner_Parse_EmptyFile_OK(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "empty.py")
	writeFile(t, pyFile, "")

	r := New("")
	res, err := r.Parse(pyFile)
	if err != nil {
		t.Fatalf("empty file should parse ok: %v", err)
	}
	if len(res.Imports) != 0 || len(res.Symbols) != 0 {
		t.Errorf("empty file should have no imports/symbols, got %+v", res)
	}
}

func TestRunner_Parse_MissingFile_Errors(t *testing.T) {
	r := New("")
	_, err := r.Parse("/nonexistent/ghost.py")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRunner_Parse_Calls_ModuleResolved(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "caller.py")
	writeFile(t, pyFile, `
import os
import os.path

x = os.path.join("a", "b")
y = os.getenv("HOME")
`)
	res, err := New("").Parse(pyFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, c := range res.Calls {
		if c.Module == "os.path" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected call resolved to module os.path, got %+v", res.Calls)
	}
}

func TestRunner_Parse_Calls_LocalResolved(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "local.py")
	writeFile(t, pyFile, `
class Builder:
    pass

def make():
    b = Builder()
    return b
`)
	res, err := New("").Parse(pyFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, c := range res.Calls {
		if c.Local == "Builder" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected call resolved to local Builder, got %+v", res.Calls)
	}
}

func TestRunner_Parse_Calls_Deduplicated(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "dedup.py")
	writeFile(t, pyFile, `
import os

a = os.getenv("A")
b = os.getenv("B")
c = os.getenv("C")
`)
	res, err := New("").Parse(pyFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	osCount := 0
	for _, c := range res.Calls {
		if c.Module == "os" {
			osCount++
		}
	}
	if osCount != 1 {
		t.Errorf("expected exactly 1 call entry for module os, got %d: %+v", osCount, res.Calls)
	}
}

func TestRunner_Parse_Calls_EmptyForNoCallsFile(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "nocalls.py")
	writeFile(t, pyFile, `
x = 1
y = "hello"
`)
	res, err := New("").Parse(pyFile)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Calls) != 0 {
		t.Errorf("expected no calls, got %+v", res.Calls)
	}
}
