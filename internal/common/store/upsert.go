package store

import (
	"database/sql"
	"fmt"
)

// InsertFile вставляет файл. ShortID должен быть назначен caller-ом до вызова.
func InsertFile(tx *sql.Tx, row FileRow) error {
	_, err := tx.Exec(
		`INSERT INTO files(file_id, short_id, key, rel_path, lang, package, hash)
		 VALUES(?,?,?,?,?,?,?)`,
		row.FileID, row.ShortID, row.Key, row.RelPath, row.Lang, row.Package, row.Hash,
	)
	return wrap(err, "insert file "+row.Key)
}

// InsertNode вставляет ноду (object или method).
// OwnerID="" → пишется NULL.
func InsertNode(tx *sql.Tx, row NodeRow) error {
	var owner sql.NullString
	if row.OwnerID != "" {
		owner = sql.NullString{String: row.OwnerID, Valid: true}
	}
	_, err := tx.Exec(
		`INSERT INTO nodes(node_id, short_id, file_id, kind, subkind, name, fqn,
		                   owner_id, scope, signature, doc, start_line, end_line)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.NodeID, row.ShortID, row.FileID, row.Kind, row.Subkind, row.Name, row.FQN,
		owner, row.Scope, row.Signature, row.Doc, row.StartLine, row.EndLine,
	)
	return wrap(err, "insert node "+row.FQN)
}

// InsertCallEdge вставляет ребро вызова. CalleeID="" → NULL (unresolved).
func InsertCallEdge(tx *sql.Tx, row CallEdge) error {
	var callee sql.NullString
	if row.CalleeID != "" {
		callee = sql.NullString{String: row.CalleeID, Valid: true}
	}
	_, err := tx.Exec(
		`INSERT INTO edges_calls(caller_id, callee_id, callee_name, callee_owner, line, confidence)
		 VALUES(?,?,?,?,?,?)`,
		row.CallerID, callee, row.CalleeName, row.CalleeOwner, row.Line, row.Confidence,
	)
	return wrap(err, "insert call "+row.CalleeName)
}

// InsertInheritEdge вставляет ребро наследования. ParentID="" → NULL.
func InsertInheritEdge(tx *sql.Tx, row InheritEdge) error {
	var parent sql.NullString
	if row.ParentID != "" {
		parent = sql.NullString{String: row.ParentID, Valid: true}
	}
	_, err := tx.Exec(
		`INSERT INTO edges_inherits(child_id, parent_id, parent_hint, relation)
		 VALUES(?,?,?,?)`,
		row.ChildID, parent, row.ParentHint, row.Relation,
	)
	return wrap(err, "insert inherit "+row.ParentHint)
}

// InsertImportEdge вставляет ребро импорта. TargetFileID="" → NULL (external).
func InsertImportEdge(tx *sql.Tx, row ImportEdge) error {
	var tgt sql.NullString
	if row.TargetFileID != "" {
		tgt = sql.NullString{String: row.TargetFileID, Valid: true}
	}
	_, err := tx.Exec(
		`INSERT INTO edges_imports(file_id, target_file_id, raw) VALUES(?,?,?)`,
		row.FileID, tgt, row.Raw,
	)
	return wrap(err, "insert import "+row.Raw)
}

// InsertSearchDoc добавляет документ в FTS5 search_idx.
// Тексты должны быть pre-tokenized caller-ом (camel-split + stemmer).
func InsertSearchDoc(tx *sql.Tx, doc SearchDoc) error {
	_, err := tx.Exec(
		`INSERT INTO search_idx(doc_id, doc_kind, name, fqn, path) VALUES(?,?,?,?,?)`,
		doc.DocID, doc.DocKind, doc.Name, doc.FQN, doc.Path,
	)
	return wrap(err, "insert search_idx "+doc.DocID)
}

// OptimizeFTS просит FTS5 слить сегменты. Вызывать после батч-инсертов.
func OptimizeFTS(tx *sql.Tx) error {
	_, err := tx.Exec(`INSERT INTO search_idx(search_idx) VALUES('optimize')`)
	return wrap(err, "fts5 optimize")
}

func wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}