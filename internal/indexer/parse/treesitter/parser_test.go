package treesitter

import (
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

// ---- Python ----

func TestPython_ValidFile_ImportsAndSymbols(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "hello.py")
	writeFile(t, f, `
import os
import sys

class Greeter:
    def greet(self, name):
        pass

def main():
    pass
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsStr(res.Imports, "os") {
		t.Errorf("expected 'os' in imports, got %v", res.Imports)
	}
	if !containsSym(res.Symbols, "Greeter") {
		t.Errorf("expected Greeter symbol, got %v", res.Symbols)
	}
	if !containsSym(res.Symbols, "main") {
		t.Errorf("expected main symbol, got %v", res.Symbols)
	}
}

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
		t.Fatalf("expected *parse.ParseError, got %T: %v", err, err)
	}
	if pe.Line <= 0 {
		t.Errorf("expected line > 0, got %d", pe.Line)
	}
}

func TestPython_MethodsUnderClass(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "cls.py")
	writeFile(t, f, `
class MyClass:
    def method_a(self):
        pass
    def method_b(self):
        pass
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if !containsQualified(res.Symbols, "MyClass.method_a") {
		t.Errorf("expected MyClass.method_a, got %v", res.Symbols)
	}
}

func TestPython_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.py")
	writeFile(t, f, "")

	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatalf("empty file should parse ok: %v", err)
	}
	if len(res.Imports) != 0 || len(res.Symbols) != 0 {
		t.Errorf("expected no imports/symbols, got %+v", res)
	}
}

func TestPython_MissingFile(t *testing.T) {
	_, err := NewPython().Parse("/nonexistent/ghost.py")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestPython_Calls_ModuleResolved(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "caller.py")
	writeFile(t, f, `
import os
import os.path

x = os.path.join("a", "b")
y = os.getenv("HOME")
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCallModule(res.Calls, "os.path") {
		t.Errorf("expected call resolved to os.path, got %+v", res.Calls)
	}
}

func TestPython_Calls_LocalResolved(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "local.py")
	writeFile(t, f, `
class Builder:
    pass

def make():
    b = Builder()
    return b
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCallLocal(res.Calls, "Builder") {
		t.Errorf("expected call resolved to local Builder, got %+v", res.Calls)
	}
}

func TestPython_Calls_Deduplicated(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "dedup.py")
	writeFile(t, f, `
import os

a = os.getenv("A")
b = os.getenv("B")
c = os.getenv("C")
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range res.Calls {
		if c.Module == "os" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry for module os (dedup), got %d: %+v", count, res.Calls)
	}
}

func TestPython_Calls_EmptyForNoCallsFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "nocalls.py")
	writeFile(t, f, `
x = 1
y = "hello"
`)
	res, err := NewPython().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Calls) != 0 {
		t.Errorf("expected no calls, got %+v", res.Calls)
	}
}

// ---- Java ----

func TestJava_Imports(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Svc.java")
	writeFile(t, f, `
import com.example.service.UserService;
import java.util.List;
import java.util.*;

public class Svc {}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsStr(res.Imports, "com.example.service.UserService") {
		t.Errorf("expected UserService import, got %v", res.Imports)
	}
	if !containsStr(res.Imports, "java.util.List") {
		t.Errorf("expected java.util.List import, got %v", res.Imports)
	}
}

func TestJava_ClassDefinition(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "UserService.java")
	writeFile(t, f, `
public class UserService extends BaseService {
}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsSym(res.Symbols, "UserService") {
		t.Errorf("expected UserService symbol, got %v", res.Symbols)
	}
}

func TestJava_MethodsInClass(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Repo.java")
	writeFile(t, f, `
public class UserRepository {
    public User findById(Long id) {
        return null;
    }
    public void save(User user) {}
}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsQualified(res.Symbols, "UserRepository.findById") {
		t.Errorf("expected UserRepository.findById, got %v", res.Symbols)
	}
	if !containsQualified(res.Symbols, "UserRepository.save") {
		t.Errorf("expected UserRepository.save, got %v", res.Symbols)
	}
}

func TestJava_Interface(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Repo.java")
	writeFile(t, f, `
public interface UserRepository {
    User findById(Long id);
}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsSym(res.Symbols, "UserRepository") {
		t.Errorf("expected UserRepository symbol, got %v", res.Symbols)
	}
}

func TestJava_Calls_ObjectCreation(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Factory.java")
	writeFile(t, f, `
import com.example.UserService;

public class Factory {
    public UserService create() {
        return new UserService();
    }
}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsCallModule(res.Calls, "com.example.UserService") {
		t.Errorf("expected call to com.example.UserService, got %+v", res.Calls)
	}
}

func TestJava_Calls_MethodInvocation(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "OrderService.java")
	writeFile(t, f, `
import com.example.UserService;

public class OrderService {
    public void process() {
        UserService.create();
    }
}
`)
	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !containsCallModule(res.Calls, "com.example.UserService") {
		t.Errorf("expected call to com.example.UserService, got %+v", res.Calls)
	}
}

func TestJava_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Empty.java")
	writeFile(t, f, "")

	res, err := NewJava().Parse(f)
	if err != nil {
		t.Fatalf("empty file should parse ok: %v", err)
	}
	if len(res.Symbols) != 0 {
		t.Errorf("expected no symbols, got %v", res.Symbols)
	}
}

// ---- helpers ----

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func containsSym(syms []parse.SymbolDef, name string) bool {
	for _, s := range syms {
		if s.Name == name {
			return true
		}
	}
	return false
}

func containsQualified(syms []parse.SymbolDef, q string) bool {
	for _, s := range syms {
		if s.Qualified == q {
			return true
		}
	}
	return false
}

func containsCallModule(calls []parse.CallRef, mod string) bool {
	for _, c := range calls {
		if c.Module == mod {
			return true
		}
	}
	return false
}

func containsCallLocal(calls []parse.CallRef, local string) bool {
	for _, c := range calls {
		if c.Local == local {
			return true
		}
	}
	return false
}
