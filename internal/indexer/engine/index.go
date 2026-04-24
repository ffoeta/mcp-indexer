package engine

import (
	"database/sql"
	"fmt"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/parse"
)

// Index выполняет полную индексацию сервиса:
// WipeAll → Phase1 (Collect) → Phase2 (Resolve) → write to DB.
func Index(
	db *sql.DB,
	rootAbs string,
	cfg *services.Config,
	matcher *services.Matcher,
	parsers map[string]parse.Parser,
	norm *tokenize.Normalizer,
	svcDir string,
) error {
	// Очищаем индекс перед переиндексацией
	if err := store.WipeAll(db); err != nil {
		return fmt.Errorf("wipe index: %w", err)
	}

	// Phase 1: collect
	cr, err := Collect(rootAbs, cfg, matcher, parsers, svcDir)
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}

	// Phase 2: resolve
	rr := Resolve(cr, norm)

	// Записываем в БД одной транзакцией
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, row := range rr.Files {
		if err := store.UpsertFile(tx, row); err != nil {
			return fmt.Errorf("upsert file %s: %w", row.Key, err)
		}
	}

	for _, row := range rr.Symbols {
		if err := store.InsertSymbol(tx, row); err != nil {
			return fmt.Errorf("insert symbol %s: %w", row.Name, err)
		}
	}

	if err := store.InsertImports(tx, rr.Imports); err != nil {
		return fmt.Errorf("insert imports: %w", err)
	}

	for _, row := range rr.Edges {
		if err := store.InsertEdge(tx, row); err != nil {
			return fmt.Errorf("insert edge %s %s→%s: %w", row.Type, row.FromID, row.ToID, err)
		}
	}

	if err := store.InsertTermPostings(tx, rr.Postings); err != nil {
		return fmt.Errorf("insert postings: %w", err)
	}

	return tx.Commit()
}
