package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed ddl.sql
var ddlSQL string

// dropLegacy сбрасывает таблицы старой схемы, если они существуют.
// Никакой обратной совместимости — full reindex после миграции.
const dropLegacy = `
DROP TABLE IF EXISTS term_postings;
DROP TABLE IF EXISTS edges;
DROP TABLE IF EXISTS imports;
DROP TABLE IF EXISTS symbols;
DROP TABLE IF EXISTS modules;
`

// Store управляет SQLite-базой одного сервиса.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(dropLegacy); err != nil {
		db.Close()
		return nil, fmt.Errorf("drop legacy schema: %w", err)
	}
	if _, err := db.Exec(ddlSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init DDL: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) Begin() (*sql.Tx, error) { return s.db.Begin() }