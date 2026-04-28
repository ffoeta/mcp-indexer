// Package query — слой запросов к индексу: FTS5 search, lookup нод/файлов,
// направленный walk по графу. Не содержит бизнес-логики MCP/JSON формата —
// это работа App-уровня.
package query

import (
	"database/sql"
	"errors"
	"fmt"
	"mcp-indexer/internal/common/store"
	"strconv"
	"strings"
)

// ───────── public types ─────────

// Hit — результат FTS-поиска.
type Hit struct {
	DocID    string  // canonical: "n:..." или "f:..."
	DocKind  string  // store.DocNode | store.DocFile
	ShortID  int64   // short_id из nodes/files
	Name     string  // оригинальное имя
	FQN      string  // оригинальный FQN (для file — пусто)
	Path     string  // rel_path файла, к которому относится doc
	Lang     string
	Line     int     // start_line для node
	Score    float64 // bm25 (smaller = better)
}

// NodeInfo — детальная сводка ноды для peek.
type NodeInfo struct {
	NodeID    string
	ShortID   int64
	Kind      string // object | method
	Subkind   string
	Name      string
	FQN       string
	OwnerID   string // canonical id владельца, "" если нет
	OwnerShort int64
	Scope     string
	Signature string
	Doc       string
	StartLine int
	EndLine   int
	File      FileBrief

	// Только для object:
	MethodCount     int
	ExtendsCount    int
	ImplementsCount int
	ExtendedByCount int

	// Только для method:
	CallsCount      int
	CalledByCount   int
	UnresolvedCalls int
}

// FileBrief — компактная инфа о файле (для вложения в NodeInfo и т.п.).
type FileBrief struct {
	FileID  string
	ShortID int64
	Path    string
	Lang    string
}

// FileInfo — overview файла для tool `file`.
type FileInfo struct {
	FileBrief
	Package string
	Imports []ImportRow
	Objects []NodeBrief
	Methods []NodeBrief // только top-level (без owner) + synthetic <module>
}

// ImportRow — запись edges_imports.
type ImportRow struct {
	Raw            string
	TargetFileID   string // "" если external
	TargetShortID  int64
	TargetPath     string
}

// NodeBrief — компактная запись о ноде (для списков в FileInfo и т.п.).
type NodeBrief struct {
	NodeID    string
	ShortID   int64
	Kind      string
	Subkind   string
	Name      string
	FQN       string
	OwnerID   string
	StartLine int
	EndLine   int
	// Для object — count методов (defines).
	MethodCount int
}

// EdgeKind — тип ребра для Walk.
type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeInherits EdgeKind = "inherits"
	EdgeImports  EdgeKind = "imports"
	EdgeDefines  EdgeKind = "defines" // псевдо-edge через nodes.owner_id
)

// Direction — направление обхода.
type Direction string

const (
	DirIn   Direction = "in"   // входящие рёбра (this — target)
	DirOut  Direction = "out"  // исходящие (this — source)
	DirBoth Direction = "both"
)

// WalkRow — одна запись в результате Walk.
type WalkRow struct {
	OtherID    string // canonical id другой стороны; "" для unresolved
	OtherKind  string // "node" | "file" | ""
	OtherShort int64
	OtherName  string
	OtherFQN   string
	OtherFile  string
	OtherLine  int

	Hint       string // для unresolved (callee_name + owner / parent_hint / import raw)
	Line       int    // строка использования (calls)
	Confidence int    // только calls
	Relation   string // только inherits
}

// WalkResult — пагинированный результат.
type WalkResult struct {
	Items []WalkRow
	Total int
}

// CodeRange — диапазон строк для извлечения исходника.
type CodeRange struct {
	FilePath  string
	StartLine int
	EndLine   int
	NodeID    string
	ShortID   int64
}

// Stats — счётчики для tool `stats`.
type Stats struct {
	Files             int
	Objects           int
	Methods           int
	CallsResolved     int
	CallsUnresolved   int
	Inherits          int
	ImportsResolved   int
	ImportsExternal   int
	FTSDocCount       int
}

// ───────── short_id ↔ canonical id ─────────

// ParseShortID разбирает строку вида "m412"/"o7"/"f3".
// Возвращает (kind ∈ {"method","object","file"}, num, ok).
func ParseShortID(s string) (kind string, num int64, ok bool) {
	if len(s) < 2 {
		return "", 0, false
	}
	switch s[0] {
	case 'm':
		kind = store.KindMethod
	case 'o':
		kind = store.KindObject
	case 'f':
		kind = "file"
	default:
		return "", 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return kind, n, true
}

// ResolveID — принимает либо short_id ("m412"/"o7"/"f3"), либо canonical id ("n:..."/"f:...").
// Возвращает canonical id и его kind ("node" | "file").
func ResolveID(db *sql.DB, idOrShort string) (canonicalID, docKind string, err error) {
	// Canonical префиксы.
	switch {
	case strings.HasPrefix(idOrShort, "n:"):
		return idOrShort, store.DocNode, nil
	case strings.HasPrefix(idOrShort, "f:"):
		return idOrShort, store.DocFile, nil
	}

	kind, num, ok := ParseShortID(idOrShort)
	if !ok {
		return "", "", fmt.Errorf("bad id %q", idOrShort)
	}
	if kind == "file" {
		var fid string
		err := db.QueryRow(`SELECT file_id FROM files WHERE short_id=?`, num).Scan(&fid)
		if err != nil {
			return "", "", err
		}
		return fid, store.DocFile, nil
	}
	var nid string
	err = db.QueryRow(`SELECT node_id FROM nodes WHERE short_id=? AND kind=?`, num, kind).Scan(&nid)
	if err != nil {
		return "", "", err
	}
	return nid, store.DocNode, nil
}

// ───────── search ─────────

// FTSearch выполняет MATCH-запрос. queryExpr — нормализованный текст
// (caller прогоняет tokenize.Normalizer и склеивает пробелами).
// kindFilter ∈ {"", "method", "object", "file"} — пустое значит без фильтра.
func FTSearch(db *sql.DB, queryExpr string, kindFilter string, limit int) ([]Hit, error) {
	if strings.TrimSpace(queryExpr) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	// bm25 column weights: name=10, fqn=8, path=4 (doc_id/doc_kind UNINDEXED ⇒ 0).
	// FTS5 column-order: doc_id, doc_kind, name, fqn, path.
	// bm25() аргументы — UNINDEXED колонки игнорируются.
	const ftsQuery = `
		SELECT s.doc_id, s.doc_kind, bm25(search_idx, 10.0, 8.0, 4.0) AS score
		FROM search_idx s
		WHERE s.search_idx MATCH ?
		ORDER BY score
		LIMIT ?
	`
	rows, err := db.Query(ftsQuery, queryExpr, limit*4) // запас под фильтр
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	var raw []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.DocID, &h.DocKind, &h.Score); err != nil {
			rows.Close()
			return nil, err
		}
		raw = append(raw, h)
	}
	rowsErr := rows.Err()
	rows.Close() // обязательно до подзапросов: MaxOpenConns(1)
	if rowsErr != nil {
		return nil, rowsErr
	}

	// Подгружаем поля по docID и применяем kind-фильтр.
	out := make([]Hit, 0, len(raw))
	for _, h := range raw {
		if h.DocKind == store.DocFile {
			if kindFilter != "" && kindFilter != "file" {
				continue
			}
			if err := fillFileHit(db, &h); err != nil {
				return nil, err
			}
		} else {
			var nodeKind string
			if err := fillNodeHit(db, &h, &nodeKind); err != nil {
				return nil, err
			}
			if kindFilter != "" && kindFilter != nodeKind {
				continue
			}
		}
		out = append(out, h)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func fillFileHit(db *sql.DB, h *Hit) error {
	return db.QueryRow(
		`SELECT short_id, rel_path, lang FROM files WHERE file_id=?`, h.DocID,
	).Scan(&h.ShortID, &h.Path, &h.Lang)
}

func fillNodeHit(db *sql.DB, h *Hit, kind *string) error {
	return db.QueryRow(`
		SELECT n.short_id, n.name, n.fqn, n.start_line, n.kind, f.rel_path, f.lang
		FROM nodes n JOIN files f ON f.file_id = n.file_id
		WHERE n.node_id = ?
	`, h.DocID).Scan(&h.ShortID, &h.Name, &h.FQN, &h.Line, kind, &h.Path, &h.Lang)
}

// ───────── GetNode ─────────

func GetNode(db *sql.DB, nodeID string) (*NodeInfo, error) {
	var n NodeInfo
	var owner sql.NullString
	err := db.QueryRow(`
		SELECT n.node_id, n.short_id, n.kind, n.subkind, n.name, n.fqn,
		       n.owner_id, n.scope, n.signature, n.doc, n.start_line, n.end_line,
		       f.file_id, f.short_id, f.rel_path, f.lang
		FROM nodes n JOIN files f ON f.file_id = n.file_id
		WHERE n.node_id = ?
	`, nodeID).Scan(
		&n.NodeID, &n.ShortID, &n.Kind, &n.Subkind, &n.Name, &n.FQN,
		&owner, &n.Scope, &n.Signature, &n.Doc, &n.StartLine, &n.EndLine,
		&n.File.FileID, &n.File.ShortID, &n.File.Path, &n.File.Lang,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("node %q not found", nodeID)
		}
		return nil, err
	}
	if owner.Valid {
		n.OwnerID = owner.String
		db.QueryRow(`SELECT short_id FROM nodes WHERE node_id=?`, owner.String).Scan(&n.OwnerShort)
	}

	switch n.Kind {
	case store.KindObject:
		db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE owner_id=?`, nodeID).Scan(&n.MethodCount)
		db.QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE child_id=? AND relation=?`, nodeID, store.RelExtends).Scan(&n.ExtendsCount)
		db.QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE child_id=? AND relation=?`, nodeID, store.RelImplements).Scan(&n.ImplementsCount)
		db.QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE parent_id=?`, nodeID).Scan(&n.ExtendedByCount)
	case store.KindMethod:
		db.QueryRow(`SELECT COUNT(*) FROM edges_calls WHERE caller_id=?`, nodeID).Scan(&n.CallsCount)
		db.QueryRow(`SELECT COUNT(*) FROM edges_calls WHERE callee_id=?`, nodeID).Scan(&n.CalledByCount)
		db.QueryRow(`SELECT COUNT(*) FROM edges_calls WHERE caller_id=? AND callee_id IS NULL`, nodeID).Scan(&n.UnresolvedCalls)
	}
	return &n, nil
}

// ───────── GetFile ─────────

// GetFile принимает path или canonical fileID.
func GetFile(db *sql.DB, pathOrID string) (*FileInfo, error) {
	var fi FileInfo
	q := `SELECT file_id, short_id, rel_path, lang, package FROM files WHERE %s = ?`
	col := "file_id"
	if !strings.HasPrefix(pathOrID, "f:") {
		col = "rel_path"
	}
	err := db.QueryRow(fmt.Sprintf(q, col), pathOrID).Scan(
		&fi.FileID, &fi.ShortID, &fi.Path, &fi.Lang, &fi.Package,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("file %q not found", pathOrID)
		}
		return nil, err
	}

	// imports
	rows, err := db.Query(`
		SELECT e.raw, e.target_file_id,
		       COALESCE(t.short_id, 0), COALESCE(t.rel_path, '')
		FROM edges_imports e
		LEFT JOIN files t ON t.file_id = e.target_file_id
		WHERE e.file_id = ?
		ORDER BY e.raw
	`, fi.FileID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ir ImportRow
		var tgt sql.NullString
		if err := rows.Scan(&ir.Raw, &tgt, &ir.TargetShortID, &ir.TargetPath); err != nil {
			rows.Close()
			return nil, err
		}
		if tgt.Valid {
			ir.TargetFileID = tgt.String
		}
		fi.Imports = append(fi.Imports, ir)
	}
	rows.Close()

	// objects (count методов считаем одним JOIN — иначе deadlock с MaxOpenConns(1))
	objRows, err := db.Query(`
		SELECT n.node_id, n.short_id, n.kind, n.subkind, n.name, n.fqn,
		       COALESCE(n.owner_id, ''), n.start_line, n.end_line,
		       (SELECT COUNT(*) FROM nodes c WHERE c.owner_id = n.node_id) AS method_count
		FROM nodes n WHERE n.file_id=? AND n.kind=?
		ORDER BY n.start_line
	`, fi.FileID, store.KindObject)
	if err != nil {
		return nil, err
	}
	for objRows.Next() {
		var nb NodeBrief
		if err := objRows.Scan(&nb.NodeID, &nb.ShortID, &nb.Kind, &nb.Subkind, &nb.Name, &nb.FQN, &nb.OwnerID, &nb.StartLine, &nb.EndLine, &nb.MethodCount); err != nil {
			objRows.Close()
			return nil, err
		}
		fi.Objects = append(fi.Objects, nb)
	}
	objRows.Close()

	// methods (top-level: owner_id IS NULL)
	mRows, err := db.Query(`
		SELECT n.node_id, n.short_id, n.kind, n.subkind, n.name, n.fqn,
		       COALESCE(n.owner_id, ''), n.start_line, n.end_line
		FROM nodes n WHERE n.file_id=? AND n.kind=? AND n.owner_id IS NULL
		ORDER BY n.start_line
	`, fi.FileID, store.KindMethod)
	if err != nil {
		return nil, err
	}
	for mRows.Next() {
		var nb NodeBrief
		if err := mRows.Scan(&nb.NodeID, &nb.ShortID, &nb.Kind, &nb.Subkind, &nb.Name, &nb.FQN, &nb.OwnerID, &nb.StartLine, &nb.EndLine); err != nil {
			mRows.Close()
			return nil, err
		}
		fi.Methods = append(fi.Methods, nb)
	}
	mRows.Close()

	return &fi, nil
}

// ───────── Walk ─────────

// Walk возвращает рёбра вокруг nodeID/fileID.
//   - EdgeCalls: nodeID должен быть method.
//   - EdgeInherits: nodeID должен быть object.
//   - EdgeImports: nodeID должен быть file_id.
//   - EdgeDefines: object→methods (out) или method→owner (in).
func Walk(db *sql.DB, id string, edge EdgeKind, dir Direction, limit, offset int) (*WalkResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if dir == "" {
		dir = DirBoth
	}
	switch edge {
	case EdgeCalls:
		return walkCalls(db, id, dir, limit, offset)
	case EdgeInherits:
		return walkInherits(db, id, dir, limit, offset)
	case EdgeImports:
		return walkImports(db, id, dir, limit, offset)
	case EdgeDefines:
		return walkDefines(db, id, dir, limit, offset)
	}
	return nil, fmt.Errorf("unknown edge kind %q", edge)
}

func walkCalls(db *sql.DB, nodeID string, dir Direction, limit, offset int) (*WalkResult, error) {
	res := &WalkResult{}
	if dir == DirOut || dir == DirBoth {
		// исходящие: this — caller
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_calls WHERE caller_id=?`, nodeID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.callee_id, e.callee_name, e.callee_owner, e.line, e.confidence,
			       COALESCE(n.short_id, 0), COALESCE(n.name, ''), COALESCE(n.fqn, ''),
			       COALESCE(f.rel_path, ''), COALESCE(n.start_line, 0)
			FROM edges_calls e
			LEFT JOIN nodes n ON n.node_id = e.callee_id
			LEFT JOIN files f ON f.file_id = n.file_id
			WHERE e.caller_id=?
			ORDER BY e.line
			LIMIT ? OFFSET ?
		`, nodeID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			var calleeID sql.NullString
			var calleeName, calleeOwner string
			var nName, nFQN string
			if err := rows.Scan(&calleeID, &calleeName, &calleeOwner, &w.Line, &w.Confidence,
				&w.OtherShort, &nName, &nFQN, &w.OtherFile, &w.OtherLine); err != nil {
				rows.Close()
				return nil, err
			}
			if calleeID.Valid {
				w.OtherID = calleeID.String
				w.OtherKind = store.DocNode
				w.OtherName = nName
				w.OtherFQN = nFQN
			} else {
				if calleeOwner != "" {
					w.Hint = calleeOwner + "." + calleeName
				} else {
					w.Hint = calleeName
				}
			}
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	if dir == DirIn || dir == DirBoth {
		// входящие: this — callee
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_calls WHERE callee_id=?`, nodeID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.caller_id, e.line, e.confidence,
			       n.short_id, n.name, n.fqn, f.rel_path, n.start_line
			FROM edges_calls e
			JOIN nodes n ON n.node_id = e.caller_id
			JOIN files f ON f.file_id = n.file_id
			WHERE e.callee_id=?
			ORDER BY n.fqn, e.line
			LIMIT ? OFFSET ?
		`, nodeID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			if err := rows.Scan(&w.OtherID, &w.Line, &w.Confidence,
				&w.OtherShort, &w.OtherName, &w.OtherFQN, &w.OtherFile, &w.OtherLine); err != nil {
				rows.Close()
				return nil, err
			}
			w.OtherKind = store.DocNode
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	return res, nil
}

func walkInherits(db *sql.DB, nodeID string, dir Direction, limit, offset int) (*WalkResult, error) {
	res := &WalkResult{}
	if dir == DirOut || dir == DirBoth {
		// out: my parents
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE child_id=?`, nodeID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.parent_id, e.parent_hint, e.relation,
			       COALESCE(n.short_id, 0), COALESCE(n.name, ''), COALESCE(n.fqn, ''),
			       COALESCE(f.rel_path, ''), COALESCE(n.start_line, 0)
			FROM edges_inherits e
			LEFT JOIN nodes n ON n.node_id = e.parent_id
			LEFT JOIN files f ON f.file_id = n.file_id
			WHERE e.child_id=?
			LIMIT ? OFFSET ?
		`, nodeID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			var parentID sql.NullString
			if err := rows.Scan(&parentID, &w.Hint, &w.Relation,
				&w.OtherShort, &w.OtherName, &w.OtherFQN, &w.OtherFile, &w.OtherLine); err != nil {
				rows.Close()
				return nil, err
			}
			if parentID.Valid {
				w.OtherID = parentID.String
				w.OtherKind = store.DocNode
			}
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	if dir == DirIn || dir == DirBoth {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE parent_id=?`, nodeID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.child_id, e.relation,
			       n.short_id, n.name, n.fqn, f.rel_path, n.start_line
			FROM edges_inherits e
			JOIN nodes n ON n.node_id = e.child_id
			JOIN files f ON f.file_id = n.file_id
			WHERE e.parent_id=?
			LIMIT ? OFFSET ?
		`, nodeID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			if err := rows.Scan(&w.OtherID, &w.Relation,
				&w.OtherShort, &w.OtherName, &w.OtherFQN, &w.OtherFile, &w.OtherLine); err != nil {
				rows.Close()
				return nil, err
			}
			w.OtherKind = store.DocNode
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	return res, nil
}

func walkImports(db *sql.DB, fileID string, dir Direction, limit, offset int) (*WalkResult, error) {
	res := &WalkResult{}
	if dir == DirOut || dir == DirBoth {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_imports WHERE file_id=?`, fileID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.target_file_id, e.raw,
			       COALESCE(t.short_id, 0), COALESCE(t.rel_path, ''), COALESCE(t.lang, '')
			FROM edges_imports e
			LEFT JOIN files t ON t.file_id = e.target_file_id
			WHERE e.file_id=?
			ORDER BY e.raw
			LIMIT ? OFFSET ?
		`, fileID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			var tgtID sql.NullString
			var lang string
			if err := rows.Scan(&tgtID, &w.Hint,
				&w.OtherShort, &w.OtherFile, &lang); err != nil {
				rows.Close()
				return nil, err
			}
			if tgtID.Valid {
				w.OtherID = tgtID.String
				w.OtherKind = store.DocFile
				w.OtherName = w.OtherFile
			}
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	if dir == DirIn || dir == DirBoth {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM edges_imports WHERE target_file_id=?`, fileID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT e.file_id, e.raw, f.short_id, f.rel_path
			FROM edges_imports e JOIN files f ON f.file_id = e.file_id
			WHERE e.target_file_id=?
			ORDER BY f.rel_path
			LIMIT ? OFFSET ?
		`, fileID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			if err := rows.Scan(&w.OtherID, &w.Hint, &w.OtherShort, &w.OtherFile); err != nil {
				rows.Close()
				return nil, err
			}
			w.OtherKind = store.DocFile
			w.OtherName = w.OtherFile
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	return res, nil
}

// walkDefines: object→methods (out) или method→owner-object (in).
func walkDefines(db *sql.DB, nodeID string, dir Direction, limit, offset int) (*WalkResult, error) {
	res := &WalkResult{}
	if dir == DirOut || dir == DirBoth {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE owner_id=?`, nodeID).Scan(&total)
		res.Total += total
		rows, err := db.Query(`
			SELECT n.node_id, n.short_id, n.name, n.fqn, f.rel_path, n.start_line
			FROM nodes n JOIN files f ON f.file_id = n.file_id
			WHERE n.owner_id=?
			ORDER BY n.start_line
			LIMIT ? OFFSET ?
		`, nodeID, limit, offset)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var w WalkRow
			if err := rows.Scan(&w.OtherID, &w.OtherShort, &w.OtherName, &w.OtherFQN, &w.OtherFile, &w.OtherLine); err != nil {
				rows.Close()
				return nil, err
			}
			w.OtherKind = store.DocNode
			res.Items = append(res.Items, w)
		}
		rows.Close()
	}
	if dir == DirIn || dir == DirBoth {
		// in: owner of this node
		var ownerID sql.NullString
		db.QueryRow(`SELECT owner_id FROM nodes WHERE node_id=?`, nodeID).Scan(&ownerID)
		if ownerID.Valid {
			res.Total++
			var w WalkRow
			err := db.QueryRow(`
				SELECT n.node_id, n.short_id, n.name, n.fqn, f.rel_path, n.start_line
				FROM nodes n JOIN files f ON f.file_id = n.file_id
				WHERE n.node_id=?
			`, ownerID.String).Scan(&w.OtherID, &w.OtherShort, &w.OtherName, &w.OtherFQN, &w.OtherFile, &w.OtherLine)
			if err == nil {
				w.OtherKind = store.DocNode
				res.Items = append(res.Items, w)
			}
		}
	}
	return res, nil
}

// ───────── code range ─────────

// GetCodeRange возвращает file path + start_line/end_line для извлечения исходника.
// Caller (App) сам читает с диска.
func GetCodeRange(db *sql.DB, nodeID string) (*CodeRange, error) {
	var cr CodeRange
	cr.NodeID = nodeID
	err := db.QueryRow(`
		SELECT f.rel_path, n.start_line, n.end_line, n.short_id
		FROM nodes n JOIN files f ON f.file_id = n.file_id
		WHERE n.node_id = ?
	`, nodeID).Scan(&cr.FilePath, &cr.StartLine, &cr.EndLine, &cr.ShortID)
	if err != nil {
		return nil, err
	}
	return &cr, nil
}

// ───────── tree (file list with counts) ─────────

// FileTreeRow — компактная запись о файле для tree-режима UI.
type FileTreeRow struct {
	ShortID int64
	Path    string
	Lang    string
	Package string
	Objects int
	Methods int
}

// ListFiles возвращает все файлы сервиса с агрегатами objects/methods.
// Один SQL вместо N+1 — JOIN с GROUP BY по nodes.kind.
func ListFiles(db *sql.DB) ([]FileTreeRow, error) {
	rows, err := db.Query(`
		SELECT f.short_id, f.rel_path, f.lang, f.package,
		       COALESCE(SUM(CASE WHEN n.kind='object' THEN 1 ELSE 0 END), 0) AS objects,
		       COALESCE(SUM(CASE WHEN n.kind='method' THEN 1 ELSE 0 END), 0) AS methods
		FROM files f LEFT JOIN nodes n ON n.file_id = f.file_id
		GROUP BY f.file_id
		ORDER BY f.rel_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileTreeRow
	for rows.Next() {
		var r FileTreeRow
		if err := rows.Scan(&r.ShortID, &r.Path, &r.Lang, &r.Package, &r.Objects, &r.Methods); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ───────── graph ─────────

// GraphNode — узел графа (file / object / method).
type GraphNode struct {
	Kind    string // "file" | "object" | "method"
	ShortID int64
	Name    string // для file — basename; для node — name
	Path    string // rel_path: для file — путь файла; для node — путь файла-владельца
	Lang    string
	Subkind string // только для node
	Line    int    // start_line для node
	OwnerID int64  // для node — short_id владельца (object), 0 если нет
	FileID  int64  // для node — short_id файла, в котором лежит
}

// GraphEdge — ребро графа.
// FromKind/ToKind — "file" | "node" (для построения short-id префикса в App).
type GraphEdge struct {
	FromKind string
	FromID   int64
	ToKind   string
	ToID     int64
	Type     string // "calls" | "inherits" | "imports" | "defines"
	Relation string // для inherits: extends | implements
}

// GraphData — узлы и рёбра.
type GraphData struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// LoadGraph выгружает полный граф сервиса.
// Включает: все files, все nodes, рёбра calls/inherits/imports (только resolved), defines (file→node, object→method).
func LoadGraph(db *sql.DB) (*GraphData, error) {
	g := &GraphData{}

	// files
	frows, err := db.Query(`SELECT short_id, rel_path, lang FROM files`)
	if err != nil {
		return nil, fmt.Errorf("graph files: %w", err)
	}
	for frows.Next() {
		var n GraphNode
		n.Kind = "file"
		if err := frows.Scan(&n.ShortID, &n.Path, &n.Lang); err != nil {
			frows.Close()
			return nil, err
		}
		// basename
		if i := strings.LastIndex(n.Path, "/"); i >= 0 {
			n.Name = n.Path[i+1:]
		} else {
			n.Name = n.Path
		}
		g.Nodes = append(g.Nodes, n)
	}
	frows.Close()

	// nodes (object|method) с file_id и owner_id, через JOIN получаем short_id связанных
	nrows, err := db.Query(`
		SELECT n.short_id, n.kind, n.subkind, n.name, n.start_line,
		       f.short_id, f.rel_path,
		       COALESCE(o.short_id, 0)
		FROM nodes n
		JOIN files f ON f.file_id = n.file_id
		LEFT JOIN nodes o ON o.node_id = n.owner_id
	`)
	if err != nil {
		return nil, fmt.Errorf("graph nodes: %w", err)
	}
	for nrows.Next() {
		var n GraphNode
		if err := nrows.Scan(&n.ShortID, &n.Kind, &n.Subkind, &n.Name, &n.Line,
			&n.FileID, &n.Path, &n.OwnerID); err != nil {
			nrows.Close()
			return nil, err
		}
		g.Nodes = append(g.Nodes, n)
		// defines edge: owner → node (если owner есть, иначе file → node)
		if n.OwnerID != 0 {
			g.Edges = append(g.Edges, GraphEdge{
				FromKind: "node", FromID: n.OwnerID,
				ToKind: "node", ToID: n.ShortID,
				Type: "defines",
			})
		} else {
			g.Edges = append(g.Edges, GraphEdge{
				FromKind: "file", FromID: n.FileID,
				ToKind: "node", ToID: n.ShortID,
				Type: "defines",
			})
		}
	}
	nrows.Close()

	// calls (resolved only)
	crows, err := db.Query(`
		SELECT n1.short_id, n2.short_id
		FROM edges_calls e
		JOIN nodes n1 ON n1.node_id = e.caller_id
		JOIN nodes n2 ON n2.node_id = e.callee_id
		WHERE e.callee_id IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("graph calls: %w", err)
	}
	for crows.Next() {
		var e GraphEdge
		e.FromKind, e.ToKind, e.Type = "node", "node", "calls"
		if err := crows.Scan(&e.FromID, &e.ToID); err != nil {
			crows.Close()
			return nil, err
		}
		g.Edges = append(g.Edges, e)
	}
	crows.Close()

	// inherits (resolved only)
	irows, err := db.Query(`
		SELECT n1.short_id, n2.short_id, e.relation
		FROM edges_inherits e
		JOIN nodes n1 ON n1.node_id = e.child_id
		JOIN nodes n2 ON n2.node_id = e.parent_id
		WHERE e.parent_id IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("graph inherits: %w", err)
	}
	for irows.Next() {
		var e GraphEdge
		e.FromKind, e.ToKind, e.Type = "node", "node", "inherits"
		if err := irows.Scan(&e.FromID, &e.ToID, &e.Relation); err != nil {
			irows.Close()
			return nil, err
		}
		g.Edges = append(g.Edges, e)
	}
	irows.Close()

	// imports (resolved only)
	mrows, err := db.Query(`
		SELECT f1.short_id, f2.short_id
		FROM edges_imports e
		JOIN files f1 ON f1.file_id = e.file_id
		JOIN files f2 ON f2.file_id = e.target_file_id
		WHERE e.target_file_id IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("graph imports: %w", err)
	}
	for mrows.Next() {
		var e GraphEdge
		e.FromKind, e.ToKind, e.Type = "file", "file", "imports"
		if err := mrows.Scan(&e.FromID, &e.ToID); err != nil {
			mrows.Close()
			return nil, err
		}
		g.Edges = append(g.Edges, e)
	}
	mrows.Close()

	return g, nil
}

// ───────── stats ─────────

func GetStats(db *sql.DB) (*Stats, error) {
	var s Stats
	queries := []struct {
		dst *int
		sql string
	}{
		{&s.Files, `SELECT COUNT(*) FROM files`},
		{&s.Objects, `SELECT COUNT(*) FROM nodes WHERE kind='object'`},
		{&s.Methods, `SELECT COUNT(*) FROM nodes WHERE kind='method'`},
		{&s.CallsResolved, `SELECT COUNT(*) FROM edges_calls WHERE callee_id IS NOT NULL`},
		{&s.CallsUnresolved, `SELECT COUNT(*) FROM edges_calls WHERE callee_id IS NULL`},
		{&s.Inherits, `SELECT COUNT(*) FROM edges_inherits`},
		{&s.ImportsResolved, `SELECT COUNT(*) FROM edges_imports WHERE target_file_id IS NOT NULL`},
		{&s.ImportsExternal, `SELECT COUNT(*) FROM edges_imports WHERE target_file_id IS NULL`},
		{&s.FTSDocCount, `SELECT COUNT(*) FROM search_idx`},
	}
	for _, q := range queries {
		if err := db.QueryRow(q.sql).Scan(q.dst); err != nil {
			return nil, fmt.Errorf("%s: %w", q.sql, err)
		}
	}
	return &s, nil
}