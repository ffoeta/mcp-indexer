// Package engine реализует полную индексацию проекта:
// scan → parse (Collect) → 3-pass резолюция (Resolve) → запись в SQLite + FTS5.
package engine

import (
	"database/sql"
	"fmt"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/parse"
)

// Index выполняет полную переиндексацию сервиса.
//   - WipeAll → очищаем индекс (full reindex, без incremental)
//   - Collect → parse + canonical FQN + maps
//   - Resolve → 3-pass резолюция вызовов, inherits, imports
//   - Write   → одной транзакцией пишем files/nodes/edges_*/search_idx
func Index(
	db *sql.DB,
	rootAbs string,
	cfg *services.Config,
	matcher *services.Matcher,
	parsers map[string]parse.Parser,
	norm *tokenize.Normalizer,
	svcDir string,
) error {
	_ = svcDir // ранее использовался для symbols_defined.json; больше не пишем

	if err := store.WipeAll(db); err != nil {
		return fmt.Errorf("wipe: %w", err)
	}

	cr, err := Collect(rootAbs, cfg, matcher, parsers)
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}

	rr := Resolve(cr, norm)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, f := range rr.Files {
		if err := store.InsertFile(tx, f); err != nil {
			return err
		}
	}
	// Сначала objects (на них ссылаются methods через owner_id).
	for _, n := range rr.Nodes {
		if n.Kind != store.KindObject {
			continue
		}
		if err := store.InsertNode(tx, n); err != nil {
			return err
		}
	}
	for _, n := range rr.Nodes {
		if n.Kind != store.KindMethod {
			continue
		}
		if err := store.InsertNode(tx, n); err != nil {
			return err
		}
	}
	for _, e := range rr.Calls {
		if err := store.InsertCallEdge(tx, e); err != nil {
			return err
		}
	}
	for _, e := range rr.Inherits {
		if err := store.InsertInheritEdge(tx, e); err != nil {
			return err
		}
	}
	for _, e := range rr.Imports {
		if err := store.InsertImportEdge(tx, e); err != nil {
			return err
		}
	}
	for _, d := range rr.FTSDocs {
		if err := store.InsertSearchDoc(tx, d); err != nil {
			return err
		}
	}
	if err := store.OptimizeFTS(tx); err != nil {
		return err
	}
	return tx.Commit()
}