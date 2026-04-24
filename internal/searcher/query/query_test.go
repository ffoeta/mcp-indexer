package query

import (
	"testing"
)

// L1: Search_ReturnsHits_ByTerm
func TestSearch_ReturnsHits_ByTerm(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	insertPosting(t, tx, "foo", fileID, 100)
	tx.Commit()

	hits, err := Search(s.DB(), []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocID != fileID {
		t.Errorf("expected hit for fileID, got %v", hits)
	}
}

// L2: Search_EmptyTerms_ReturnsNil
func TestSearch_EmptyTerms_ReturnsNil(t *testing.T) {
	s := openTestStore(t)
	hits, err := Search(s.DB(), []string{})
	if err != nil {
		t.Fatal(err)
	}
	if hits != nil {
		t.Error("expected nil for empty terms")
	}
}

// L3: Search_ScoresSummed_MultiTermHit
func TestSearch_ScoresSummed_MultiTermHit(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	insertPosting(t, tx, "foo", fileID, 100)
	insertPosting(t, tx, "bar", fileID, 50)
	tx.Commit()

	hits, err := Search(s.DB(), []string{"foo", "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].Score != 150 {
		t.Errorf("expected score 150, got %v", hits[0].Score)
	}
}

// L4: Search_SortedByScoreDesc
func TestSearch_SortedByScoreDesc(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileA := insertFile(t, tx, "a.py", "python")
	fileB := insertFile(t, tx, "b.py", "python")
	insertPosting(t, tx, "foo", fileA, 10)
	insertPosting(t, tx, "foo", fileB, 100)
	tx.Commit()

	hits, err := Search(s.DB(), []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatal("expected 2 hits")
	}
	if hits[0].Score < hits[1].Score {
		t.Error("hits must be sorted by score descending")
	}
}

// L5: Search_UnknownTerm_ReturnsEmpty
func TestSearch_UnknownTerm_ReturnsEmpty(t *testing.T) {
	s := openTestStore(t)
	hits, err := Search(s.DB(), []string{"zzz_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("expected no hits for unknown term, got %v", hits)
	}
}

// M1: GetFileContext_ByKey
func TestGetFileContext_ByKey(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	insertFile(t, tx, "a.py", "python")
	tx.Exec(`INSERT INTO imports(file_id, imp) VALUES('f:a.py','os')`)
	tx.Commit()

	ctx, err := GetFileContext(s.DB(), "a.py")
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil result")
	}
	if ctx.Key != "a.py" {
		t.Errorf("expected key=a.py, got %q", ctx.Key)
	}
	if len(ctx.Imports) != 1 || ctx.Imports[0] != "os" {
		t.Errorf("expected imports=[os], got %v", ctx.Imports)
	}
}

// M2: GetFileContext_ByRelPath_Fallback
func TestGetFileContext_ByRelPath_Fallback(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	insertFile(t, tx, "src:pkg/a.py", "python")
	tx.Commit()

	ctx, err := GetFileContext(s.DB(), "src:pkg/a.py")
	if err != nil {
		t.Fatal(err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil result")
	}
}

// M3: GetFileContext_NotFound_ReturnsNil
func TestGetFileContext_NotFound_ReturnsNil(t *testing.T) {
	s := openTestStore(t)
	ctx, err := GetFileContext(s.DB(), "ghost.py")
	if err != nil {
		t.Fatal(err)
	}
	if ctx != nil {
		t.Error("expected nil for missing file")
	}
}

// M4: GetFileContext_IncludesSymbols
func TestGetFileContext_IncludesSymbols(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	insertSymbol(t, tx, "s:py:Foo:a.py:1", fileID, "class", "Foo")
	tx.Commit()

	ctx, err := GetFileContext(s.DB(), "a.py")
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Symbols) != 1 || ctx.Symbols[0].Name != "Foo" {
		t.Errorf("expected symbol Foo, got %v", ctx.Symbols)
	}
}

// M5: GetSymbolContext_Found
func TestGetSymbolContext_Found(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	tx.Commit()

	sym, err := GetSymbolContext(s.DB(), symID)
	if err != nil {
		t.Fatal(err)
	}
	if sym == nil {
		t.Fatal("expected non-nil result")
	}
	if sym.Name != "Foo" {
		t.Errorf("expected name=Foo, got %q", sym.Name)
	}
	if sym.FileKey != "a.py" {
		t.Errorf("expected file key=a.py, got %q", sym.FileKey)
	}
}

// M5b: GetSymbolContext_HasRelPath
func TestGetSymbolContext_HasRelPath(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "pkg/a.py", "python")
	symID := "s:py:Foo:pkg/a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	tx.Commit()

	sym, err := GetSymbolContext(s.DB(), symID)
	if err != nil {
		t.Fatal(err)
	}
	if sym == nil {
		t.Fatal("expected non-nil result")
	}
	if sym.RelPath != "pkg/a.py" {
		t.Errorf("expected relPath=pkg/a.py, got %q", sym.RelPath)
	}
}

// M6: GetSymbolContext_NotFound_ReturnsNil
func TestGetSymbolContext_NotFound_ReturnsNil(t *testing.T) {
	s := openTestStore(t)
	sym, err := GetSymbolContext(s.DB(), "s:py:Ghost:x.py:0")
	if err != nil {
		t.Fatal(err)
	}
	if sym != nil {
		t.Error("expected nil for unknown symbol")
	}
}

// M12: GetCallers_FindsBySymbolId
func TestGetCallers_FindsBySymbolId(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	callerFileID := insertFile(t, tx, "caller.py", "python")
	targetFileID := insertFile(t, tx, "target.py", "python")
	symID := "s:py:Foo:target.py:1"
	insertSymbol(t, tx, symID, targetFileID, "class", "Foo")
	insertEdge(t, tx, "calls", callerFileID, symID)
	tx.Commit()

	callers, err := GetCallers(s.DB(), symID)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller, got %d", len(callers))
	}
	if callers[0].FileKey != "caller.py" {
		t.Errorf("expected fileKey=caller.py, got %q", callers[0].FileKey)
	}
	if callers[0].Via != symID {
		t.Errorf("expected Via=%q, got %q", symID, callers[0].Via)
	}
}

// M14: GetCallers_Empty_NoCallers
func TestGetCallers_Empty_NoCallers(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	tx.Commit()

	callers, err := GetCallers(s.DB(), symID)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 0 {
		t.Errorf("expected 0 callers, got %d", len(callers))
	}
}

// M7: GetNeighbors_DirectEdges
func TestGetNeighbors_DirectEdges(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertEdge(t, tx, "defines", fileID, symID)
	tx.Commit()

	edges, err := GetNeighbors(s.DB(), fileID, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Error("expected at least one neighbor edge")
	}
	found := false
	for _, e := range edges {
		if e[0] == "defines" && e[1] == fileID && e[2] == symID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected defines edge, got %v", edges)
	}
}

// M8: GetNeighbors_BFS_Depth2
func TestGetNeighbors_BFS_Depth2(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileA := insertFile(t, tx, "a.py", "python")
	fileB := insertFile(t, tx, "b.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileA, "class", "Foo")
	insertEdge(t, tx, "defines", fileA, symID)
	insertEdge(t, tx, "calls", symID, fileB)
	tx.Commit()

	edges, err := GetNeighbors(s.DB(), fileA, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) < 2 {
		t.Errorf("expected ≥2 edges at depth=2, got %d: %v", len(edges), edges)
	}
}

// M9: GetNeighbors_EdgeTypeFilter
func TestGetNeighbors_EdgeTypeFilter(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileA := insertFile(t, tx, "a.py", "python")
	fileB := insertFile(t, tx, "b.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileA, "class", "Foo")
	insertEdge(t, tx, "defines", fileA, symID)
	insertEdge(t, tx, "imports", fileA, fileB)
	tx.Commit()

	edges, err := GetNeighbors(s.DB(), fileA, 1, []string{"imports"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e[0] != "imports" {
			t.Errorf("expected only imports edges, got type %q", e[0])
		}
	}
}

// M10: GetNeighbors_ReturnsTriple_TypeFromTo
func TestGetNeighbors_ReturnsTriple_TypeFromTo(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertEdge(t, tx, "defines", fileID, symID)
	tx.Commit()

	edges, err := GetNeighbors(s.DB(), fileID, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("no edges")
	}
	e := edges[0]
	if e[0] == "" || e[1] == "" || e[2] == "" {
		t.Errorf("edge triple has empty fields: %v", e)
	}
}

// M11: GetOverview_Counts
func TestGetOverview_Counts(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertEdge(t, tx, "defines", fileID, symID)
	tx.Commit()

	ov, err := GetOverview(s.DB())
	if err != nil {
		t.Fatal(err)
	}
	if ov.Files != 1 {
		t.Errorf("expected 1 file, got %d", ov.Files)
	}
	if ov.Symbols != 1 {
		t.Errorf("expected 1 symbol, got %d", ov.Symbols)
	}
	if ov.Edges != 1 {
		t.Errorf("expected 1 edge, got %d", ov.Edges)
	}
}
