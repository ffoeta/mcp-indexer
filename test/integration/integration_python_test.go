//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-indexer/internal/app"
	sqliteq "mcp-indexer/internal/index/sqlite"
)

// INT-8: Search finds Collector class by name
func TestIntegration_Python_Search_FindsCollectorClass(t *testing.T) {
	a, svcID, _ := setupService(t)

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
				return
			}
		}
	}
	t.Errorf("Collector class not found; sym=%v", resp.Sym)
}

// INT-9: Search finds pkg.collector module
func TestIntegration_Python_Search_FindsPkgModule(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	resp, err := a.Search(svcID, "collector", app.SearchLimits{Sym: 20, File: 10, Mod: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resp.Mod {
		if len(row) >= 2 {
			if name, ok := row[1].(string); ok && strings.Contains(name, "collector") {
				return
			}
		}
	}
	t.Errorf("collector module not found; mod=%v", resp.Mod)
}

// INT-10: GetFileContext returns context for collector.py
func TestIntegration_Python_GetFileContext_CollectorPy(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	ctx, err := a.GetFileContext(svcID, "pkg/collector.py")
	if err != nil {
		t.Fatalf("GetFileContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil context for pkg/collector.py")
	}
}

// INT-11: GetNeighbors returns edges for pkg.collector module
func TestIntegration_Python_GetNeighbors_CollectorModule(t *testing.T) {
	a, svcID, _ := setupService(t)

	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	edges, err := a.GetNeighbors(svcID, "m:py:pkg.collector", 1, nil)
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	rows, ok := edges.([]sqliteq.NeighborEdge)
	if !ok {
		t.Fatalf("expected []sqlite.NeighborEdge, got %T", edges)
	}
	if len(rows) == 0 {
		t.Error("expected at least one edge for pkg.collector module")
	}
}

// INT-12: большой Python файл — символы, импорты и граф корректно индексируются
func TestIntegration_Python_LargeFile_FullIndex(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	copyFixture(t, root, "python/large_pipeline.py")

	svcID, err := a.AddService(root, "py-large", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("expected 1 added, got %d", res.Added)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected sync errors: %+v", res.Errors)
	}

	// --- GetFileContext ---

	raw, err := a.GetFileContext(svcID, "large_pipeline.py")
	if err != nil {
		t.Fatalf("GetFileContext: %v", err)
	}
	fc, ok := raw.(*sqliteq.FileContextRow)
	if !ok {
		t.Fatalf("expected *sqlite.FileContextRow, got %T", raw)
	}

	if fc.Lang != "python" {
		t.Errorf("expected lang=python, got %q", fc.Lang)
	}
	if fc.ModuleName != "large_pipeline" {
		t.Errorf("expected module=large_pipeline, got %q", fc.ModuleName)
	}

	importSet := make(map[string]bool, len(fc.Imports))
	for _, imp := range fc.Imports {
		importSet[imp] = true
	}
	for _, imp := range []string{"os", "json", "logging", "re", "datetime", "pathlib", "collections", "typing"} {
		if !importSet[imp] {
			t.Errorf("import %q not found; got %v", imp, fc.Imports)
		}
	}

	wantSymbols := map[string]string{
		"BaseProcessor":        "class",
		"FileProcessor":        "class",
		"JsonProcessor":        "class",
		"LogProcessor":         "class",
		"PipelineOrchestrator": "class",
		"build_pipeline":       "function",
		"load_config":          "function",
		"save_results":         "function",
	}
	symByName := make(map[string]sqliteq.SymbolSummary, len(fc.Symbols))
	for _, s := range fc.Symbols {
		symByName[s.Name] = s
	}
	for name, wantKind := range wantSymbols {
		sym, found := symByName[name]
		if !found {
			t.Errorf("symbol %q not found; got %v", name, symbolNames(fc.Symbols))
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: expected kind=%q, got %q", name, wantKind, sym.Kind)
		}
		if sym.StartLine == 0 {
			t.Errorf("symbol %q: StartLine must be > 0", name)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine(%d) < StartLine(%d)", name, sym.EndLine, sym.StartLine)
		}
	}

	// --- Search ---

	for _, query := range []string{"PipelineOrchestrator", "FileProcessor", "build_pipeline"} {
		resp, err := a.Search(svcID, query, app.SearchLimits{Sym: 20, File: 10, Mod: 5})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		found := false
		for _, row := range resp.Sym {
			if len(row) >= 3 {
				if name, ok := row[2].(string); ok && name == query {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("Search(%q): not found in %v", query, resp.Sym)
		}
	}

	// --- GetNeighbors: модуль large_pipeline ---

	edges, err := a.GetNeighbors(svcID, "m:py:large_pipeline", 1, nil)
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	rows, ok := edges.([]sqliteq.NeighborEdge)
	if !ok {
		t.Fatalf("expected []sqlite.NeighborEdge, got %T", edges)
	}
	foundContains := false
	for _, e := range rows {
		if e[0] == "contains" {
			foundContains = true
			break
		}
	}
	if !foundContains {
		t.Errorf("expected 'contains' edge from module; got %v", rows)
	}
}

// INT-13: связанные Python файлы с замыканиями — проверка cross-file индексации
//
// Фикстуры: testdata/python/linked/events.py + dispatcher.py
//
// Ключевые свойства:
//   - make_logger_handler, make_filter_handler, build_dispatcher — top-level функции, ИНДЕКСИРУЮТСЯ
//   - Вложенные функции внутри них (замыкания) — НЕ индексируются как символы
//   - dispatcher.py импортирует из events → import edge events→dispatcher
func TestIntegration_Python_LinkedFiles_WithClosures(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	copyLinkedPythonFixtures(t, root)

	svcID, err := a.AddService(root, "py-linked", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 {
		t.Fatalf("expected 2 files added, got %d", res.Added)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected sync errors: %+v", res.Errors)
	}

	// --- events.py: проверяем символы и замыкания ---

	evRaw, err := a.GetFileContext(svcID, "events.py")
	if err != nil {
		t.Fatalf("GetFileContext(events.py): %v", err)
	}
	evFC := evRaw.(*sqliteq.FileContextRow)

	// Top-level функции-фабрики должны быть проиндексированы
	wantEventSyms := map[string]string{
		"EventBus":             "class",
		"make_logger_handler":  "function",
		"make_filter_handler":  "function",
	}
	for name, kind := range wantEventSyms {
		found := false
		for _, s := range evFC.Symbols {
			if s.Name == name && s.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("events.py: symbol %q (kind=%s) not found; got %v", name, kind, symbolNames(evFC.Symbols))
		}
	}

	// Замыкания (вложенные функции "handle") НЕ должны быть в символах
	for _, s := range evFC.Symbols {
		if s.Name == "handle" {
			t.Errorf("events.py: closure 'handle' must NOT be indexed as symbol, but found: %+v", s)
		}
	}

	// --- dispatcher.py: проверяем символы, замыкания и импорты ---

	dispRaw, err := a.GetFileContext(svcID, "dispatcher.py")
	if err != nil {
		t.Fatalf("GetFileContext(dispatcher.py): %v", err)
	}
	dispFC := dispRaw.(*sqliteq.FileContextRow)

	wantDispSyms := map[string]string{
		"Dispatcher":       "class",
		"build_dispatcher": "function",
	}
	for name, kind := range wantDispSyms {
		found := false
		for _, s := range dispFC.Symbols {
			if s.Name == name && s.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("dispatcher.py: symbol %q (kind=%s) not found; got %v", name, kind, symbolNames(dispFC.Symbols))
		}
	}

	// Замыкание on_error внутри build_dispatcher НЕ должно быть в символах
	for _, s := range dispFC.Symbols {
		if s.Name == "on_error" {
			t.Errorf("dispatcher.py: closure 'on_error' must NOT be indexed as symbol, but found: %+v", s)
		}
	}

	// dispatcher.py импортирует из events
	importSet := make(map[string]bool)
	for _, imp := range dispFC.Imports {
		importSet[imp] = true
	}
	if !importSet["events"] {
		t.Errorf("dispatcher.py: expected import 'events', got %v", dispFC.Imports)
	}

	// --- Search поперёк обоих файлов ---

	for _, query := range []string{"EventBus", "Dispatcher", "build_dispatcher"} {
		resp, err := a.Search(svcID, query, app.SearchLimits{Sym: 20, File: 10, Mod: 5})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		found := false
		for _, row := range resp.Sym {
			if len(row) >= 3 {
				if name, ok := row[2].(string); ok && name == query {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("Search(%q): not found", query)
		}
	}

	// --- Import edge: dispatcher → events module ---

	edges, err := a.GetNeighbors(svcID, "f:dispatcher.py", 1, []string{"imports"})
	if err != nil {
		t.Fatalf("GetNeighbors(dispatcher.py): %v", err)
	}
	rows := edges.([]sqliteq.NeighborEdge)
	foundImportEdge := false
	for _, e := range rows {
		if e[0] == "imports" && e[2] == "m:python:events" {
			foundImportEdge = true
			break
		}
	}
	if !foundImportEdge {
		t.Errorf("expected imports edge dispatcher→m:python:events; got %v", rows)
	}
}

func copyLinkedPythonFixtures(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"events.py", "dispatcher.py"} {
		src := filepath.Join("testdata", "python", "linked", name)
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// copyPythonFile copies src python fixture into root, preserving the filename.
// Kept for use in table-driven tests if needed.
func copyPythonFile(t *testing.T, root, name string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "python", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
}
