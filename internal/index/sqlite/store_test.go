package sqlite

import (
	"mcp-indexer/internal/index"
	"os"
	"path/filepath"
	"testing"
)

// H1: Store_Open_CreatesDB
func TestStore_Open_CreatesDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Error("DB file not created")
	}
}

// H2: Store_Open_IdempotentDDL
func TestStore_Open_IdempotentDDL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	s2.Close()
}

// H3: Store_WAL_Mode
func TestStore_WAL_Mode(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var mode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %q", mode)
	}
}

// H4: Store_ForeignKeys_On
func TestStore_ForeignKeys_On(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var fk int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Error("expected foreign_keys=ON")
	}
}

// H5: Store_Tables_AllExist
func TestStore_Tables_AllExist(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tables := []string{"files", "symbols", "imports", "edges", "term_postings"}
	for _, tbl := range tables {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

// H6: Store_Begin_Rollback_NoLeaks
func TestStore_Begin_Rollback_NoLeaks(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx.Exec(`INSERT INTO files(file_id,key,rel_path,lang,hash) VALUES('f:x','x','x','go','b3:aa')`)
	tx.Rollback()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n)
	if n != 0 {
		t.Error("rollback should leave no rows")
	}
}

// H7: Store_UpsertFile_And_Query
func TestStore_UpsertFile_And_Query(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertFile(tx, index.FileRow{
		FileID: "f:a.py", Key: "a.py", RelPath: "a.py", Lang: "python", Hash: "b3:cafe",
	}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var hash string
	s.DB().QueryRow(`SELECT hash FROM files WHERE key='a.py'`).Scan(&hash)
	if hash != "b3:cafe" {
		t.Errorf("expected hash b3:cafe, got %q", hash)
	}
}

// H8: Store_CreatesMissingDir
func TestStore_CreatesMissingDir(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "a", "b", "c", "test.db")
	s, err := Open(nested)
	if err != nil {
		t.Fatalf("Open in nested dir: %v", err)
	}
	s.Close()
}
