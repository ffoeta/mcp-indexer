package store

import (
	"database/sql"
	"fmt"
	"strings"

)

const batchInsert = 200 // строк на один multi-row INSERT

func UpsertFile(tx *sql.Tx, row FileRow) error {
	_, err := tx.Exec(
		`INSERT INTO files(file_id, key, rel_path, lang, hash) VALUES(?,?,?,?,?)
		 ON CONFLICT(file_id) DO UPDATE SET
		   key=excluded.key, rel_path=excluded.rel_path, lang=excluded.lang,
		   hash=excluded.hash`,
		row.FileID, row.Key, row.RelPath, row.Lang, row.Hash,
	)
	return wrap(err, "upsert file "+row.Key)
}

func InsertImports(tx *sql.Tx, rows []ImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT INTO imports(file_id, imp) VALUES(?,?)`)
	if err != nil {
		return wrap(err, "prepare imports")
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.FileID, r.Imp); err != nil {
			return wrap(err, "insert import "+r.Imp)
		}
	}
	return nil
}

func InsertSymbol(tx *sql.Tx, row SymbolRow) error {
	_, err := tx.Exec(
		`INSERT INTO symbols(symbol_id, file_id, kind, name, qualified, start_line, end_line)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(symbol_id) DO NOTHING`,
		row.SymbolID, row.FileID, row.Kind, row.Name, row.Qualified, row.StartLine, row.EndLine,
	)
	return wrap(err, "insert symbol "+row.Name)
}

func InsertEdge(tx *sql.Tx, row EdgeRow) error {
	_, err := tx.Exec(
		`INSERT INTO edges(type, from_id, to_id, confidence, aux) VALUES(?,?,?,?,?)`,
		row.Type, row.FromID, row.ToID, row.Confidence, row.Aux,
	)
	return wrap(err, "insert edge "+row.Type)
}

func InsertTermPostings(tx *sql.Tx, rows []TermPosting) error {
	if len(rows) == 0 {
		return nil
	}
	for i := 0; i < len(rows); i += batchInsert {
		end := i + batchInsert
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		ph := "(?,?,?)" + strings.Repeat(",(?,?,?)", len(chunk)-1)
		args := make([]any, 0, len(chunk)*3)
		for _, r := range chunk {
			args = append(args, r.Term, r.DocID, r.Weight)
		}
		if _, err := tx.Exec(
			`INSERT INTO term_postings(term, doc_id, weight) VALUES `+ph,
			args...,
		); err != nil {
			return wrap(err, "batch insert term_postings")
		}
	}
	return nil
}

func wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
