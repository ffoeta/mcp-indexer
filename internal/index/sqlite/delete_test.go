package sqlite

import (
	"mcp-indexer/internal/index"
	"testing"
)

// I1: Delete_RemovesFile
func TestDelete_RemovesFile(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	insertFile(t, tx, "a.py", "python")
	tx.Commit()

	tx2, _ := s.Begin()
	if err := DeleteFileByKey(tx2, "a.py"); err != nil {
		tx2.Rollback()
		t.Fatal(err)
	}
	tx2.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE key='a.py'`).Scan(&n)
	if n != 0 {
		t.Error("file should be deleted")
	}
}

// I2: Delete_CascadesSymbols
func TestDelete_CascadesSymbols(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	insertSymbol(t, tx, "s:py:Foo:a.py:1", fileID, "class", "Foo")
	tx.Commit()

	tx2, _ := s.Begin()
	DeleteFileByKey(tx2, "a.py")
	tx2.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&n)
	if n != 0 {
		t.Error("symbols should be cascade-deleted")
	}
}

// I3: Delete_CascadesImports
func TestDelete_CascadesImports(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	tx.Exec(`INSERT INTO imports(file_id, imp) VALUES(?,?)`, fileID, "os")
	tx.Commit()

	tx2, _ := s.Begin()
	DeleteFileByKey(tx2, "a.py")
	tx2.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM imports`).Scan(&n)
	if n != 0 {
		t.Error("imports should be cascade-deleted")
	}
}

// I4: Delete_CleansEdges
func TestDelete_CleansEdges(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertEdge(t, tx, "defines", fileID, symID)
	tx.Commit()

	tx2, _ := s.Begin()
	DeleteFileByKey(tx2, "a.py")
	tx2.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n)
	if n != 0 {
		t.Errorf("edges should be deleted, got %d", n)
	}
}

// I5: Delete_CleansTermPostings
func TestDelete_CleansTermPostings(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:1"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertPosting(t, tx, "foo", fileID, 100)
	insertPosting(t, tx, "bar", symID, 80)
	tx.Commit()

	tx2, _ := s.Begin()
	DeleteFileByKey(tx2, "a.py")
	tx2.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM term_postings`).Scan(&n)
	if n != 0 {
		t.Errorf("term_postings should be deleted, got %d", n)
	}
}

// I6: Delete_NonExistent_NoError
func TestDelete_NonExistent_NoError(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	if err := DeleteFileByKey(tx, "ghost.py"); err != nil {
		tx.Rollback()
		t.Errorf("delete non-existent should not error: %v", err)
		return
	}
	tx.Commit()
}

// I7: Delete_LeavesOtherFilesIntact
func TestDelete_LeavesOtherFilesIntact(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileA := insertFile(t, tx, "a.py", "python")
	fileB := insertFile(t, tx, "b.py", "python")
	insertSymbol(t, tx, "s:py:A:a.py:1", fileA, "class", "A")
	insertSymbol(t, tx, "s:py:B:b.py:1", fileB, "class", "B")
	insertEdge(t, tx, "defines", fileA, "s:py:A:a.py:1")
	insertEdge(t, tx, "defines", fileB, "s:py:B:b.py:1")
	tx.Commit()

	tx2, _ := s.Begin()
	DeleteFileByKey(tx2, "a.py")
	tx2.Commit()

	var nFiles, nSymbols int
	s.DB().QueryRow(`SELECT COUNT(*) FROM files`).Scan(&nFiles)
	s.DB().QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&nSymbols)
	if nFiles != 1 {
		t.Errorf("expected 1 file remaining, got %d", nFiles)
	}
	if nSymbols != 1 {
		t.Errorf("expected 1 symbol remaining, got %d", nSymbols)
	}

	var edgeCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edgeCount)
	if edgeCount != 1 {
		t.Errorf("expected 1 edge remaining (b.py→B), got %d", edgeCount)
	}
}

// I8: Delete_CollectsSymbolsBeforeDelete
func TestDelete_CollectsSymbolsBeforeDelete(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fileID := insertFile(t, tx, "a.py", "python")
	symID := "s:py:Foo:a.py:5"
	insertSymbol(t, tx, symID, fileID, "class", "Foo")
	insertEdge(t, tx, "base", symID, index.UnresolvedID("Bar"))
	insertPosting(t, tx, "foo", symID, 100)
	tx.Commit()

	tx2, _ := s.Begin()
	if err := DeleteFileByKey(tx2, "a.py"); err != nil {
		tx2.Rollback()
		t.Fatal(err)
	}
	tx2.Commit()

	var edgeN, postN int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges WHERE from_id=? OR to_id=?`, symID, symID).Scan(&edgeN)
	s.DB().QueryRow(`SELECT COUNT(*) FROM term_postings WHERE doc_id=?`, symID).Scan(&postN)
	if edgeN != 0 {
		t.Errorf("symbol edges not cleaned: %d remain", edgeN)
	}
	if postN != 0 {
		t.Errorf("symbol postings not cleaned: %d remain", postN)
	}
}
