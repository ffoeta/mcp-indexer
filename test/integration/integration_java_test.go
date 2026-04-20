//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"mcp-indexer/internal/app"
	sqliteq "mcp-indexer/internal/index/sqlite"
)

// INT-13: большой Java файл — символы, импорты и граф корректно индексируются
func TestIntegration_Java_LargeFile_FullIndex(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	copyFixture(t, root, "java/LargePipeline.java")

	svcID, err := a.AddService(root, "java-large", "", nil)
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

	raw, err := a.GetFileContext(svcID, "LargePipeline.java")
	if err != nil {
		t.Fatalf("GetFileContext: %v", err)
	}
	fc, ok := raw.(*sqliteq.FileContextRow)
	if !ok {
		t.Fatalf("expected *sqlite.FileContextRow, got %T", raw)
	}

	if fc.Lang != "java" {
		t.Errorf("expected lang=java, got %q", fc.Lang)
	}
	// Java не создаёт модули — module_name всегда пустой
	if fc.ModuleName != "" {
		t.Errorf("expected empty module for java, got %q", fc.ModuleName)
	}

	importSet := make(map[string]bool, len(fc.Imports))
	for _, imp := range fc.Imports {
		importSet[imp] = true
	}
	for _, imp := range []string{
		"java.util.List",
		"java.util.ArrayList",
		"java.util.Map",
		"java.util.HashMap",
		"java.util.Optional",
		"java.io.IOException",
	} {
		if !importSet[imp] {
			t.Errorf("import %q not found; got %v", imp, fc.Imports)
		}
	}

	// Ищем класс по Name + Kind (конструктор тоже называется ClassName, но kind=function)
	wantClasses := []string{"Processor", "BaseProcessor", "FileProcessor", "JsonProcessor", "PipelineOrchestrator"}
	for _, name := range wantClasses {
		var found *sqliteq.SymbolSummary
		for i := range fc.Symbols {
			if fc.Symbols[i].Name == name && fc.Symbols[i].Kind == "class" {
				found = &fc.Symbols[i]
				break
			}
		}
		if found == nil {
			t.Errorf("class symbol %q not found; got %v", name, symbolNames(fc.Symbols))
			continue
		}
		if found.StartLine == 0 {
			t.Errorf("class %q: StartLine must be > 0", name)
		}
		if found.EndLine < found.StartLine {
			t.Errorf("class %q: EndLine(%d) < StartLine(%d)", name, found.EndLine, found.StartLine)
		}
	}

	// Проверяем методы через qualified name
	wantQualified := []string{
		"BaseProcessor.configure",
		"BaseProcessor.getName",
		"FileProcessor.process",
		"FileProcessor.getStats",
		"FileProcessor.clear",
		"PipelineOrchestrator.run",
		"PipelineOrchestrator.reset",
		"PipelineOrchestrator.summary",
	}
	qualifiedSet := make(map[string]bool, len(fc.Symbols))
	for _, s := range fc.Symbols {
		qualifiedSet[s.Qualified] = true
	}
	for _, q := range wantQualified {
		if !qualifiedSet[q] {
			t.Errorf("qualified symbol %q not found; symbols=%v", q, symbolNames(fc.Symbols))
		}
	}

	// --- Search ---

	for _, query := range []string{"PipelineOrchestrator", "FileProcessor", "BaseProcessor"} {
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

	// --- GetNeighbors: по fileId — ожидаем defines и imports рёбра ---

	edges, err := a.GetNeighbors(svcID, "f:LargePipeline.java", 1, nil)
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	rows, ok := edges.([]sqliteq.NeighborEdge)
	if !ok {
		t.Fatalf("expected []sqlite.NeighborEdge, got %T", edges)
	}
	if len(rows) == 0 {
		t.Error("expected edges for LargePipeline.java file node")
	}

	edgeTypes := make(map[string]bool)
	for _, e := range rows {
		edgeTypes[e[0]] = true
	}
	if !edgeTypes["defines"] {
		t.Errorf("expected 'defines' edge; got types %v", edgeTypes)
	}
	if !edgeTypes["imports"] {
		t.Errorf("expected 'imports' edge; got types %v", edgeTypes)
	}
}

// INT-14: связанные Java файлы со статическими методами и static import
//
// Фикстуры: testdata/java/linked/{EventBus,Repository,OrderService}.java
//
// Ключевые свойства:
//   - Статические методы (EventBus.create, isValidEventType, OrderService.withInMemoryRepo) — ИНДЕКСИРУЮТСЯ
//   - Статические поля (MAX_HANDLERS, DEFAULT_STATUS) — НЕ индексируются
//   - import static — ИНДЕКСИРУЕТСЯ как полное qualified имя (tree-sitter не различает static/non-static imports)
//   - OrderService использует EventBus.create() → call edge
func TestIntegration_Java_LinkedFiles_WithStaticMembers(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	copyLinkedJavaFixtures(t, root)

	svcID, err := a.AddService(root, "java-linked", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := a.DoSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 {
		t.Fatalf("expected 3 files added, got %d", res.Added)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected sync errors: %+v", res.Errors)
	}

	// --- EventBus.java ---

	ebRaw, err := a.GetFileContext(svcID, "EventBus.java")
	if err != nil {
		t.Fatalf("GetFileContext(EventBus.java): %v", err)
	}
	ebFC := ebRaw.(*sqliteq.FileContextRow)

	if ebFC.Lang != "java" {
		t.Errorf("expected lang=java, got %q", ebFC.Lang)
	}

	// Статические методы должны быть проиндексированы
	wantEBMethods := []string{"create", "isValidEventType"}
	ebQualified := make(map[string]bool)
	for _, s := range ebFC.Symbols {
		ebQualified[s.Qualified] = true
	}
	for _, m := range wantEBMethods {
		q := "EventBus." + m
		if !ebQualified[q] {
			t.Errorf("EventBus.java: static method %q not found; symbols=%v", q, symbolNames(ebFC.Symbols))
		}
	}

	// import static индексируется как полное qualified имя (поведение парсера)
	importSet := make(map[string]bool)
	for _, imp := range ebFC.Imports {
		importSet[imp] = true
	}
	if !importSet["java.util.Collections.emptyList"] {
		t.Errorf("EventBus.java: expected static import 'java.util.Collections.emptyList' to be indexed; got %v", ebFC.Imports)
	}

	// Статические поля НЕ должны быть в символах
	for _, s := range ebFC.Symbols {
		if s.Name == "MAX_HANDLERS" {
			t.Errorf("EventBus.java: static field MAX_HANDLERS must NOT be indexed as symbol")
		}
	}

	// --- Repository.java: интерфейс ---

	repoRaw, err := a.GetFileContext(svcID, "Repository.java")
	if err != nil {
		t.Fatalf("GetFileContext(Repository.java): %v", err)
	}
	repoFC := repoRaw.(*sqliteq.FileContextRow)

	repoFound := false
	for i := range repoFC.Symbols {
		if repoFC.Symbols[i].Name == "Repository" && repoFC.Symbols[i].Kind == "class" {
			repoFound = true
			break
		}
	}
	if !repoFound {
		t.Errorf("Repository.java: interface Repository not found as class symbol; got %v", symbolNames(repoFC.Symbols))
	}

	// --- OrderService.java ---

	osRaw, err := a.GetFileContext(svcID, "OrderService.java")
	if err != nil {
		t.Fatalf("GetFileContext(OrderService.java): %v", err)
	}
	osFC := osRaw.(*sqliteq.FileContextRow)

	// Классы OrderService и InMemoryRepository должны быть проиндексированы
	wantOSClasses := []string{"OrderService", "InMemoryRepository"}
	for _, name := range wantOSClasses {
		found := false
		for i := range osFC.Symbols {
			if osFC.Symbols[i].Name == name && osFC.Symbols[i].Kind == "class" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("OrderService.java: class %q not found; got %v", name, symbolNames(osFC.Symbols))
		}
	}

	// Статический метод withInMemoryRepo должен быть проиндексирован
	osWithFactory := false
	for _, s := range osFC.Symbols {
		if s.Qualified == "OrderService.withInMemoryRepo" {
			osWithFactory = true
			break
		}
	}
	if !osWithFactory {
		t.Errorf("OrderService.java: static method OrderService.withInMemoryRepo not found")
	}

	// Статическое поле DEFAULT_STATUS НЕ должно быть в символах
	for _, s := range osFC.Symbols {
		if s.Name == "DEFAULT_STATUS" {
			t.Errorf("OrderService.java: static field DEFAULT_STATUS must NOT be indexed as symbol")
		}
	}

	// import static индексируется как полное qualified имя (поведение парсера)
	osImportSet := make(map[string]bool)
	for _, imp := range osFC.Imports {
		osImportSet[imp] = true
	}
	if !osImportSet["java.util.Collections.unmodifiableList"] {
		t.Errorf("OrderService.java: expected static import 'java.util.Collections.unmodifiableList' to be indexed; got %v", osFC.Imports)
	}

	// --- Search поперёк всех трёх файлов ---

	for _, query := range []string{"EventBus", "OrderService", "Repository"} {
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

	// --- GetNeighbors: OrderService.java должен иметь defines и imports рёбра ---

	edges, err := a.GetNeighbors(svcID, "f:OrderService.java", 1, nil)
	if err != nil {
		t.Fatalf("GetNeighbors(OrderService.java): %v", err)
	}
	rows := edges.([]sqliteq.NeighborEdge)
	edgeTypes := make(map[string]bool)
	for _, e := range rows {
		edgeTypes[e[0]] = true
	}
	if !edgeTypes["defines"] {
		t.Errorf("expected 'defines' edges for OrderService.java; got %v", edgeTypes)
	}
	if !edgeTypes["imports"] {
		t.Errorf("expected 'imports' edges for OrderService.java; got %v", edgeTypes)
	}
}

func copyLinkedJavaFixtures(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"EventBus.java", "Repository.java", "OrderService.java"} {
		src := filepath.Join("testdata", "java", "linked", name)
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// copyJavaFile copies a java fixture into root, preserving the filename.
// Kept for use in table-driven tests if needed.
func copyJavaFile(t *testing.T, root, name string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "java", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
}
