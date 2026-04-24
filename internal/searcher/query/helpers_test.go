package query

import (
	"database/sql"
	"mcp-indexer/internal/common/store"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertFile(t *testing.T, tx *sql.Tx, key, lang string) string {
	t.Helper()
	fileID := store.FileID(key)
	_, err := tx.Exec(
		`INSERT INTO files(file_id, key, rel_path, lang, hash) VALUES(?,?,?,?,?)`,
		fileID, key, key, lang, "b3:aabb",
	)
	if err != nil {
		t.Fatalf("insertFile %q: %v", key, err)
	}
	return fileID
}

func insertSymbol(t *testing.T, tx *sql.Tx, symID, fileID, kind, name string) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO symbols(symbol_id, file_id, kind, name, qualified, start_line, end_line)
		 VALUES(?,?,?,?,?,1,10)`,
		symID, fileID, kind, name, name,
	)
	if err != nil {
		t.Fatalf("insertSymbol %q: %v", symID, err)
	}
}

func insertEdge(t *testing.T, tx *sql.Tx, typ, from, to string) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO edges(type, from_id, to_id, confidence) VALUES(?,?,?,100)`,
		typ, from, to,
	)
	if err != nil {
		t.Fatalf("insertEdge %q->%q: %v", from, to, err)
	}
}

func insertPosting(t *testing.T, tx *sql.Tx, term, docID string, weight float64) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO term_postings(term, doc_id, weight) VALUES(?,?,?)`,
		term, docID, weight,
	)
	if err != nil {
		t.Fatalf("insertPosting %q: %v", term, err)
	}
}
