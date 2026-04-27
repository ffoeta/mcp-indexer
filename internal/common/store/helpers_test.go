package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mkFile вставляет файл с минимальными полями. Возвращает FileID.
func mkFile(t *testing.T, tx *sql.Tx, shortID int64, key, lang string) string {
	t.Helper()
	row := FileRow{
		FileID:  FileID(key),
		ShortID: shortID,
		Key:     key,
		RelPath: key,
		Lang:    lang,
		Hash:    "b3:aabb",
	}
	if err := InsertFile(tx, row); err != nil {
		t.Fatalf("InsertFile %q: %v", key, err)
	}
	return row.FileID
}

// mkNode вставляет ноду. Возвращает NodeID.
func mkNode(t *testing.T, tx *sql.Tx, shortID int64, fileID, kind, subkind, fqn string, line int) string {
	t.Helper()
	nid := NodeID("python", kind, fqn, fileID, line)
	row := NodeRow{
		NodeID:    nid,
		ShortID:   shortID,
		FileID:    fileID,
		Kind:      kind,
		Subkind:   subkind,
		Name:      fqn,
		FQN:       fqn,
		Scope:     ScopeGlobal,
		StartLine: line,
		EndLine:   line + 5,
	}
	if err := InsertNode(tx, row); err != nil {
		t.Fatalf("InsertNode %q: %v", fqn, err)
	}
	return nid
}