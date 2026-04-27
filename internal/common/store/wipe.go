package store

import "database/sql"

// WipeAll удаляет все данные из индекса (перед полной переиндексацией).
// FTS5 search_idx тоже очищается.
func WipeAll(db *sql.DB) error {
	tables := []string{
		"edges_calls",
		"edges_inherits",
		"edges_imports",
		"nodes",
		"files",
	}
	for _, t := range tables {
		if _, err := db.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`DELETE FROM search_idx`); err != nil {
		return err
	}
	return nil
}