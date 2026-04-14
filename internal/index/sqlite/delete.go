package sqlite

import (
	"database/sql"
	"fmt"
	"mcp-indexer/internal/index"
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

	// 3. Удаляем edges (нет FK → вручную)
	for _, id := range allIDs {
		if _, err := tx.Exec(
			`DELETE FROM edges WHERE from_id = ? OR to_id = ?`, id, id,
		); err != nil {
			return fmt.Errorf("delete edges for %q: %w", id, err)
		}
	}

	// 4. Удаляем term_postings (нет FK → вручную)
	for _, id := range allIDs {
		if _, err := tx.Exec(
			`DELETE FROM term_postings WHERE doc_id = ?`, id,
		); err != nil {
			return fmt.Errorf("delete postings for %q: %w", id, err)
		}
	}

	// 5. Удаляем файл — CASCADE удаляет symbols + imports
	if _, err := tx.Exec(`DELETE FROM files WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete file %q: %w", key, err)
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
