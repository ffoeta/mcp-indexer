// Package testutil provides shared test helpers.
package testutil

import (
	"database/sql"
	"mcp-indexer/internal/common/store"
	"os"
	"path/filepath"
	"testing"
)

// NewTestStore opens a fresh SQLite store in a temp dir.
func NewTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// WriteFile writes content to path, creating intermediate dirs.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// InsertFile inserts a minimal file row into DB.
func InsertFile(t *testing.T, tx *sql.Tx, key, lang string) string {
	t.Helper()
	fileID := store.FileID(key)
	_, err := tx.Exec(
		`INSERT INTO files(file_id, key, rel_path, lang, hash) VALUES(?,?,?,?,?)`,
		fileID, key, key, lang, "b3:aabb",
	)
	if err != nil {
		t.Fatalf("InsertFile %q: %v", key, err)
	}
	return fileID
}

// InsertSymbol inserts a minimal symbol row.
func InsertSymbol(t *testing.T, tx *sql.Tx, symID, fileID, kind, name string) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO symbols(symbol_id, file_id, kind, name, qualified, start_line, end_line)
		 VALUES(?,?,?,?,?,1,10)`,
		symID, fileID, kind, name, name,
	)
	if err != nil {
		t.Fatalf("InsertSymbol %q: %v", symID, err)
	}
}

// InsertEdge inserts an edge row.
func InsertEdge(t *testing.T, tx *sql.Tx, typ, from, to string) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO edges(type, from_id, to_id, confidence) VALUES(?,?,?,100)`,
		typ, from, to,
	)
	if err != nil {
		t.Fatalf("InsertEdge %q->%q: %v", from, to, err)
	}
}

// InsertPosting inserts a term posting.
func InsertPosting(t *testing.T, tx *sql.Tx, term, docID string, weight float64) {
	t.Helper()
	_, err := tx.Exec(
		`INSERT INTO term_postings(term, doc_id, weight) VALUES(?,?,?)`,
		term, docID, weight,
	)
	if err != nil {
		t.Fatalf("InsertPosting %q: %v", term, err)
	}
}

// CountRows counts rows in a table optionally filtered by WHERE clause.
func CountRows(t *testing.T, db *sql.DB, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("CountRows %q: %v", query, err)
	}
	return n
}
