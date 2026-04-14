package sqlite

import (
	"database/sql"
	"fmt"
	"mcp-indexer/internal/index"
	"strings"
)

// DeleteFileByKey удаляет файл и всё связанное:
//   - edges где from_id / to_id = fileId или любого symbolId файла
//   - term_postings для fileId и symbolIds
//   - файл (CASCADE удаляет symbols + imports)
func DeleteFileByKey(tx *sql.Tx, key string) error {
	fileID := index.FileID(key)

	// 1. Собираем symbolIds (нужны до CASCADE-удаления файла)
	symIDs, err := collectSymbolIDs(tx, fileID)
	if err != nil {
		return fmt.Errorf("collect symbols for %q: %w", key, err)
	}

	// 2. Собираем все docIDs (fileId + symbolIds)
	allIDs := make([]string, 0, len(symIDs)+1)
	allIDs = append(allIDs, fileID)
	allIDs = append(allIDs, symIDs...)

	// 3. Удаляем edges (нет FK → вручную), батчами по 400 IDs
	// edges использует список дважды (from_id IN + to_id IN), поэтому 400 * 2 < 999
	if err := deleteEdgesByIDs(tx, allIDs); err != nil {
		return fmt.Errorf("delete edges for file %q: %w", key, err)
	}

	// 4. Удаляем term_postings (нет FK → вручную), батчами по 800 IDs
	if err := deletePostingsByIDs(tx, allIDs); err != nil {
		return fmt.Errorf("delete postings for file %q: %w", key, err)
	}

	// 5. Удаляем файл — CASCADE удаляет symbols + imports
	if _, err := tx.Exec(`DELETE FROM files WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete file %q: %w", key, err)
	}
	return nil
}

const batchEdge = 400   // edges: 2 списка × 400 = 800 < 999
const batchPosting = 800 // postings: 1 список × 800 < 999

// deleteEdgesByIDs удаляет edges где from_id или to_id входит в ids.
func deleteEdgesByIDs(tx *sql.Tx, ids []string) error {
	for i := 0; i < len(ids); i += batchEdge {
		end := i + batchEdge
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		ph := "?" + strings.Repeat(",?", len(chunk)-1)
		args := make([]any, 0, len(chunk)*2)
		for _, id := range chunk {
			args = append(args, id)
		}
		for _, id := range chunk {
			args = append(args, id)
		}
		if _, err := tx.Exec(
			`DELETE FROM edges WHERE from_id IN (`+ph+`) OR to_id IN (`+ph+`)`,
			args...,
		); err != nil {
			return err
		}
	}
	return nil
}

// deletePostingsByIDs удаляет term_postings для указанных doc_id.
func deletePostingsByIDs(tx *sql.Tx, ids []string) error {
	for i := 0; i < len(ids); i += batchPosting {
		end := i + batchPosting
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		ph := "?" + strings.Repeat(",?", len(chunk)-1)
		args := make([]any, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}
		if _, err := tx.Exec(
			`DELETE FROM term_postings WHERE doc_id IN (`+ph+`)`,
			args...,
		); err != nil {
			return err
		}
	}
	return nil
}

func collectSymbolIDs(tx *sql.Tx, fileID string) ([]string, error) {
	rows, err := tx.Query(`SELECT symbol_id FROM symbols WHERE file_id = ?`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
