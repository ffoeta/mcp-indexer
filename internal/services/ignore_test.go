package services

import (
	"os"
	"path/filepath"
	"testing"
)

func loadMatcherStr(t *testing.T, patterns string) *Matcher {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "service.ignore")
	os.WriteFile(p, []byte(patterns), 0o644)
	m, err := LoadMatcher(p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// B4: Ignore_MissingFile_TreatedAsNoIgnore
func TestIgnore_MissingFile_TreatedAsNoIgnore(t *testing.T) {
	m, err := LoadMatcher("/nonexistent/service.ignore")
	if err != nil {
		t.Fatal(err)
	}
	if m.Match("anything/file.py") {
		t.Error("empty matcher should not match")
	}
}

// B5: Ignore_CommentAndBlankLines_Ignored
func TestIgnore_CommentAndBlankLines_Ignored(t *testing.T) {
	m := loadMatcherStr(t, "# comment\n\n  # another\n")
	if m.Match("file.py") {
		t.Error("should not match with only comments/blanks")
	}
}

// B6: Ignore_DirPattern_Target
func TestIgnore_DirPattern_Target(t *testing.T) {
	m := loadMatcherStr(t, "target/\n")
	cases := []struct {
		path    string
		want    bool
	}{
		{"target/x.class", true},
		{"a/target/x.class", false}, // doublestar нужен для вложенности
		{"target", false},           // не файл, не матчит dir-suffix без слеша
	}
	for _, c := range cases {
		if got := m.Match(c.path); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// B7: Ignore_Doublestar_Pycache
func TestIgnore_Doublestar_Pycache(t *testing.T) {
	m := loadMatcherStr(t, "**/__pycache__/**\n")
	if !m.Match("pkg/__pycache__/foo.pyc") {
		t.Error("should match nested __pycache__")
	}
	if !m.Match("a/b/__pycache__/c.pyc") {
		t.Error("should match deep __pycache__")
	}
}

// B8: Ignore_FileGlob_Log
func TestIgnore_FileGlob_Log(t *testing.T) {
	m := loadMatcherStr(t, "*.log\n")
	if !m.Match("a.log") {
		t.Error("should match a.log")
	}
	if m.Match("a.log.txt") {
		t.Error("should not match a.log.txt")
	}
}

// B9: Ignore_PatternDoesNotMatchPartialSegments
func TestIgnore_PatternDoesNotMatchPartialSegments(t *testing.T) {
	m := loadMatcherStr(t, "node_modules/\n")
	if m.Match("my_node_modules/foo.js") {
		t.Error("node_modules/ should not match my_node_modules/")
	}
	if !m.Match("node_modules/foo.js") {
		t.Error("node_modules/ should match node_modules/foo.js")
	}
}

// B10: Ignore_NormalizesSlashes (tested via scan — matcher receives unix slashes)
func TestIgnore_MatchesUnixSlashPath(t *testing.T) {
	m := loadMatcherStr(t, "*.pyc\n")
	if !m.Match("pkg/module.pyc") {
		t.Error("should match .pyc with path")
	}
}

// B11: Ignore_AppliesToRelPathWithoutPrefix
// Паттерны видят rel_path (без src: prefix), так работает scan.go
func TestIgnore_AppliesToRelPath(t *testing.T) {
	m := loadMatcherStr(t, "*.py\n")
	if m.Match("src:pkg/a.py") {
		t.Error("matcher should NOT see pathPrefix — only rel_path should be matched")
	}
	if !m.Match("pkg/a.py") {
		t.Error("should match rel_path directly")
	}
}
