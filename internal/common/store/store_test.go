package store

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestStore_WAL_Mode(t *testing.T) {
	s := openTestStore(t)
	var mode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %q", mode)
	}
}

func TestStore_ForeignKeys_On(t *testing.T) {
	s := openTestStore(t)
	var fk int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Error("expected foreign_keys=ON")
	}
}

func TestStore_Tables_AllExist(t *testing.T) {
	s := openTestStore(t)
	tables := []string{
		"files",
		"nodes",
		"edges_calls",
		"edges_inherits",
		"edges_imports",
		"search_idx",
	}
	for _, tbl := range tables {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE name=?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

func TestStore_LegacyTables_Dropped(t *testing.T) {
	s := openTestStore(t)
	legacy := []string{"symbols", "imports", "edges", "term_postings", "modules"}
	for _, tbl := range legacy {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err == nil {
			t.Errorf("legacy table %q should be dropped, but exists", tbl)
		}
	}
}

func TestStore_Begin_Rollback_NoLeaks(t *testing.T) {
	s := openTestStore(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mkFile(t, tx, 1, "x.py", "python")
	tx.Rollback()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n)
	if n != 0 {
		t.Error("rollback should leave no rows")
	}
}

func TestStore_CreatesMissingDir(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "a", "b", "c", "test.db")
	s, err := Open(nested)
	if err != nil {
		t.Fatalf("Open in nested dir: %v", err)
	}
	s.Close()
}