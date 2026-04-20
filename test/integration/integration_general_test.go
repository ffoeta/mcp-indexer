//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"mcp-indexer/internal/app"
	sqliteq "mcp-indexer/internal/index/sqlite"
	"mcp-indexer/internal/services"
)

// setupApp creates an App backed by a fresh temp home directory.
func setupApp(t *testing.T) *app.App {
	t.Helper()
	t.Setenv("MCP_INDEXER_HOME", t.TempDir())
	a, err := app.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// setupService registers a service pointing at a temp directory
// pre-populated with a small multi-file Python fixture.
//
// Fixture layout:
//
//	root/
//	  pkg/__init__.py    — empty init, marks package
//	  pkg/collector.py   — Collector class, imports os/json
//	  pkg/runner.py      — RunnerService class, imports Collector
//	  main.py            — main() calls RunnerService
func setupService(t *testing.T) (a *app.App, svcID string, root string) {
	t.Helper()
	a = setupApp(t)
	root = t.TempDir()
	writeMultiFileFixture(t, root)
	svcID, err := a.AddService(root, "fixture", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return a, svcID, root
}

func writeMultiFileFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"pkg/__init__.py": "",

		"pkg/collector.py": `import os
import json

class Collector:
    def __init__(self, name):
        self.name = name

    def collect(self):
        return os.listdir(self.name)

    def to_json(self):
        return json.dumps({"name": self.name})

def create_collector(name):
    return Collector(name)
`,

		"pkg/runner.py": `from pkg.collector import Collector

class RunnerService:
    def __init__(self):
        self.collector = Collector("default")

    def run(self):
        return self.collector.collect()
`,

		"main.py": `from pkg.runner import RunnerService

def main():
    svc = RunnerService()
    svc.run()

if __name__ == "__main__":
    main()
`,
	}

	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// symbolNames returns symbol names for error messages.
func symbolNames(syms []sqliteq.SymbolSummary) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}

// copyFixture copies a testdata file into root and returns its destination path.
func copyFixture(t *testing.T, root, testdataRel string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(testdataRel)))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", testdataRel, err)
	}
	dst := filepath.Join(root, filepath.Base(testdataRel))
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// ---- general sync tests ----

// INT-1: full sync indexes all 4 fixture files
func TestIntegration_DoSync_IndexesAllFiles(t *testing.T) {
	a, svcID, _ := setupService(t)

	res, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 4 {
		t.Errorf("expected 4 added, got %d", res.Added)
	}
	if res.Modified != 0 {
		t.Errorf("expected 0 modified, got %d", res.Modified)
	}
	if res.Deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", res.Deleted)
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected sync errors: %v", res.Errors)
	}
}

// INT-2: second DoSync on unchanged tree is a no-op
func TestIntegration_DoSync_Idempotent(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	res2, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Added != 0 || res2.Modified != 0 || res2.Deleted != 0 {
		t.Errorf("expected no-op, got added=%d modified=%d deleted=%d",
			res2.Added, res2.Modified, res2.Deleted)
	}
}

// INT-3: modified file is detected and re-indexed on the next sync
func TestIntegration_DoSync_DetectsModification(t *testing.T) {
	a, svcID, root := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "pkg", "collector.py"),
		[]byte("import os\n\nclass Collector:\n    pass\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	res2, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Modified != 1 {
		t.Errorf("expected 1 modified, got %d", res2.Modified)
	}
}

// INT-4: deleted file is removed from index on the next sync
func TestIntegration_DoSync_DetectsDeletion(t *testing.T) {
	a, svcID, root := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "main.py")); err != nil {
		t.Fatal(err)
	}
	res2, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", res2.Deleted)
	}
}

// INT-5: PrepareSync after DoSync reports zero changes
func TestIntegration_PrepareSync_AfterSync_NoChanges(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	prep, err := a.PrepareSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Added != 0 || prep.MaybeModified != 0 || prep.Deleted != 0 {
		t.Errorf("expected no changes after sync, got added=%d maybeModified=%d deleted=%d",
			prep.Added, prep.MaybeModified, prep.Deleted)
	}
}

// INT-6: GetProjectOverview returns non-zero counts after sync
func TestIntegration_GetProjectOverview_HasData(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	overview, err := a.GetProjectOverview(svcID)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := overview.(*sqliteq.OverviewCounts)
	if !ok {
		t.Fatalf("expected *sqlite.OverviewCounts, got %T", overview)
	}
	if o.Files < 4 {
		t.Errorf("expected >=4 files, got %d", o.Files)
	}
	if o.Symbols == 0 {
		t.Error("expected symbols > 0 after sync")
	}
}

// INT-7: Search after modification reflects updated symbols
func TestIntegration_Search_ReflectsModification(t *testing.T) {
	a, svcID, root := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "pkg", "collector.py"),
		[]byte("class Harvester:\n    pass\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}

	resp, err := a.Search(svcID, "Collector", app.SearchLimits{Sym: 20, File: 10, Mod: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resp.Sym {
		if len(row) >= 3 {
			if name, ok := row[2].(string); ok && name == "Collector" {
				t.Error("Collector still present after removal")
			}
		}
	}

	resp2, err := a.Search(svcID, "Harvester", app.SearchLimits{Sym: 20, File: 10, Mod: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resp2.Sym {
		if len(row) >= 3 {
			if name, ok := row[2].(string); ok && name == "Harvester" {
				return
			}
		}
	}
	t.Error("Harvester not found after re-indexing")
}

// addServiceWithFile registers a service with a single file copied from testdata.
func addServiceWithFile(t *testing.T, a *app.App, svcID, testdataRel string) {
	t.Helper()
	root := t.TempDir()
	copyFixture(t, root, testdataRel)
	if _, err := a.AddService(root, svcID, "", "", nil); err != nil {
		t.Fatal(err)
	}

	// Ensure the service has .java in includeExt if needed
	cfgPath := services.ConfigPath(svcID)
	cfg, err := services.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// DefaultConfig already includes .py and .java — nothing to change
	_ = cfg
}
