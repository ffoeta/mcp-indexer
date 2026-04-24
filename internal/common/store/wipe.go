package store

import "database/sql"

// WipeAll удаляет все данные из индекса (перед полной переиндексацией).
func WipeAll(db *sql.DB) error {
	tables := []string{"term_postings", "edges", "imports", "symbols", "files"}
	for _, t := range tables {
		if _, err := db.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	return nil
}
