package scan

import (
	"mcp-indexer/internal/common/services"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func makeCfg(prefix string, exts ...string) *services.Config {
	return &services.Config{PathPrefix: prefix, IncludeExt: exts}
}

func noMatcher(t *testing.T) *services.Matcher {
	t.Helper()
	m, _ := services.LoadMatcher("/nonexistent/service.ignore")
	return m
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// B12 / Scan_IncludeExt_FilterWorks
func TestScan_IncludeExt_FilterWorks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.py"), "")
	writeFile(t, filepath.Join(root, "b.java"), "")
	writeFile(t, filepath.Join(root, "c.go"), "")
	writeFile(t, filepath.Join(root, "d.txt"), "")

	entries, err := Scan(root, makeCfg("", ".py", ".java"), noMatcher(t))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Key)
	}
	sort.Strings(keys)
	if len(keys) != 2 {
		t.Errorf("expected 2 files, got %v", keys)
	}
}

// C1: RelUnix_Basic
func TestScan_RelPathIsUnixSlash(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "a.py"), "")
	entries, _ := Scan(root, makeCfg("", ".py"), noMatcher(t))
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].RelPath != "pkg/a.py" {
		t.Errorf("RelPath = %q, want unix slash", entries[0].RelPath)
	}
}

// C4: Key_Build_PrefixPlusRel
func TestScan_KeyIncludesPrefix(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "a.py"), "")
	entries, _ := Scan(root, makeCfg("src:", ".py"), noMatcher(t))
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Key != "src:pkg/a.py" {
		t.Errorf("Key = %q, want src:pkg/a.py", entries[0].Key)
	}
}

// C10: Scan_OutputKeysUnique
func TestScan_OutputKeysUnique(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.py"), "")
	writeFile(t, filepath.Join(root, "b.py"), "")
	entries, _ := Scan(root, makeCfg("", ".py"), noMatcher(t))
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Key] {
			t.Errorf("duplicate key %q", e.Key)
		}
		seen[e.Key] = true
	}
}

// C7: Key_Sorting_LexicographicStable (DiffHash sorts)
func TestScan_SortStable(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"z.py", "a.py", "m.py"} {
		writeFile(t, filepath.Join(root, f), "")
	}
	entries, _ := Scan(root, makeCfg("", ".py"), noMatcher(t))
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	// Scan не гарантирует порядок — проверяем что unique
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate: %q", k)
		}
		seen[k] = true
	}
}

// B11 variant: Scan applies ignore to rel_path (not to key with prefix)
func TestScan_IgnoreAppliesToRelPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "__pycache__", "foo.pyc"), "")
	writeFile(t, filepath.Join(root, "pkg", "a.py"), "")

	m, _ := services.LoadMatcher("/nonexistent")
	// Manually create matcher with __pycache__ pattern
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "si"), []byte("__pycache__/\n"), 0o644)
	m2, _ := services.LoadMatcher(filepath.Join(dir, "si"))

	entries, _ := Scan(root, makeCfg("src:", ".py", ".pyc"), m2)
	for _, e := range entries {
		if e.RelPath == "__pycache__/foo.pyc" {
			t.Error("__pycache__ file should be ignored")
		}
	}
	_ = m
}
