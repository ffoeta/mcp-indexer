package query

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"mcp-indexer/internal/common/store"
)

// SearchHit — результат поиска по одному doc_id.
type SearchHit struct {
	DocID string
	Score float64
}

// Search ищет термины в term_postings.
// Типы (sym/file/mod) с нулевым лимитом в prefixes не запрашиваются.
func Search(db *sql.DB, terms []string) ([]SearchHit, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	scores := make(map[string]float64)
	for _, term := range terms {
		rows, err := db.Query(`SELECT doc_id, weight FROM term_postings WHERE term = ?`, term)
		if err != nil {
			return nil, fmt.Errorf("query postings %q: %w", term, err)
		}
		for rows.Next() {
			var docID string
			var w float64
			if err := rows.Scan(&docID, &w); err != nil {
				rows.Close()
				return nil, err
			}
			scores[docID] += w
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	hits := make([]SearchHit, 0, len(scores))
	for docID, score := range scores {
		hits = append(hits, SearchHit{DocID: docID, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}

// FileContextRow — результат getFileContext.
type FileContextRow struct {
	FileID     string
	Key        string
	RelPath    string
	Lang       string
	ModuleName string // вычисляется из rel_path, для Python
	Imports    []string
	Symbols    []SymbolSummary
}

type SymbolSummary struct {
	SymbolID  string
	Kind      string
	Name      string
	Qualified string
	StartLine int
	EndLine   int
}

// GetFileContext ищет файл по key (полный ключ) или по rel_path.
func GetFileContext(db *sql.DB, keyOrPath string) (*FileContextRow, error) {
	// Пробуем key сначала, потом rel_path
	row, err := getFileContextByField(db, "key", keyOrPath)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row, err = getFileContextByField(db, "rel_path", keyOrPath)
		if err != nil {
			return nil, err
		}
	}
	return row, nil
}

func getFileContextByField(db *sql.DB, field, value string) (*FileContextRow, error) {
	q := fmt.Sprintf(
		`SELECT file_id, key, rel_path, lang FROM files WHERE %s = ?`, field,
	)
	row := db.QueryRow(q, value)
	var r FileContextRow
	if err := row.Scan(&r.FileID, &r.Key, &r.RelPath, &r.Lang); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get file by %s=%q: %w", field, value, err)
	}
	if r.Lang == "python" {
		r.ModuleName = store.PythonModuleName(r.RelPath)
	}

	// imports
	irows, err := db.Query(`SELECT imp FROM imports WHERE file_id = ? ORDER BY imp`, r.FileID)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var imp string
		if err := irows.Scan(&imp); err != nil {
			return nil, err
		}
		r.Imports = append(r.Imports, imp)
	}
	if err := irows.Err(); err != nil {
		return nil, err
	}

	// symbols
	srows, err := db.Query(
		`SELECT symbol_id, kind, name, qualified, start_line, end_line
		 FROM symbols WHERE file_id = ? ORDER BY start_line`, r.FileID,
	)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var s SymbolSummary
		if err := srows.Scan(&s.SymbolID, &s.Kind, &s.Name, &s.Qualified, &s.StartLine, &s.EndLine); err != nil {
			return nil, err
		}
		r.Symbols = append(r.Symbols, s)
	}
	return &r, srows.Err()
}

// SymbolContextRow — результат getSymbolContext.
type SymbolContextRow struct {
	SymbolID  string
	FileKey   string
	RelPath   string
	Kind      string
	Name      string
	Qualified string
	StartLine int
	EndLine   int
}

func GetSymbolContext(db *sql.DB, symbolID string) (*SymbolContextRow, error) {
	row := db.QueryRow(
		`SELECT s.symbol_id, f.key, f.rel_path, s.kind, s.name, s.qualified, s.start_line, s.end_line
		 FROM symbols s JOIN files f ON s.file_id = f.file_id
		 WHERE s.symbol_id = ?`, symbolID,
	)
	var r SymbolContextRow
	if err := row.Scan(&r.SymbolID, &r.FileKey, &r.RelPath, &r.Kind, &r.Name, &r.Qualified, &r.StartLine, &r.EndLine); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get symbol %q: %w", symbolID, err)
	}
	return &r, nil
}

// CallerRef — файл, вызывающий модуль/символ.
type CallerRef struct {
	FileKey string `json:"fileKey"`
	Via     string `json:"via"` // moduleId или symbolId, на который указывает calls-ребро
}

// GetCallers возвращает файлы с calls-рёбрами в symbolId.
func GetCallers(db *sql.DB, symbolID string) ([]CallerRef, error) {
	rows, err := db.Query(
		`SELECT COALESCE(f.key, e.from_id), e.to_id
		 FROM edges e
		 LEFT JOIN files f ON f.file_id = e.from_id
		 WHERE e.type = 'calls' AND e.to_id = ?`, symbolID,
	)
	if err != nil {
		return nil, fmt.Errorf("get callers: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	callers := []CallerRef{}
	for rows.Next() {
		var fileKey, toID string
		if err := rows.Scan(&fileKey, &toID); err != nil {
			return nil, err
		}
		if seen[fileKey] {
			continue
		}
		seen[fileKey] = true
		callers = append(callers, CallerRef{FileKey: fileKey, Via: toID})
	}
	return callers, rows.Err()
}

// BuildModuleFileMap возвращает map moduleName → fileID для всех Python-файлов в транзакции.
func BuildModuleFileMap(tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.Query(`SELECT file_id, rel_path FROM files WHERE lang = 'python'`)
	if err != nil {
		return nil, fmt.Errorf("query files for module map: %w", err)
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var fileID, relPath string
		if err := rows.Scan(&fileID, &relPath); err != nil {
			return nil, err
		}
		m[store.PythonModuleName(relPath)] = fileID
	}
	return m, rows.Err()
}

// NeighborEdge — ребро в графе (формат basic: тройка [type, from, to]).
type NeighborEdge [3]string // [edgeType, fromID, toID]

// GetNeighbors делает BFS по edges до depth.
// Возвращает список уникальных рёбер в формате [type, from, to].
func GetNeighbors(db *sql.DB, nodeID string, depth int, edgeTypes []string) ([]NeighborEdge, error) {
	typeFilter := buildTypeFilter(edgeTypes)

	visited := map[string]bool{nodeID: true}
	frontier := []string{nodeID}
	result := []NeighborEdge{}
	edgeSeen := map[string]bool{}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		var nextFrontier []string
		for _, cur := range frontier {
			edges, err := directEdges(db, cur, typeFilter)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				key := e[0] + "|" + e[1] + "|" + e[2]
				if !edgeSeen[key] {
					edgeSeen[key] = true
					result = append(result, e)
				}
				// Добавляем соседний узел в следующий frontier
				neighbor := e[2]
				if e[1] != cur { // входящее ребро
					neighbor = e[1]
				}
				if !visited[neighbor] {
					visited[neighbor] = true
					nextFrontier = append(nextFrontier, neighbor)
				}
			}
		}
		frontier = nextFrontier
	}
	return result, nil
}

func directEdges(db *sql.DB, nodeID, typeFilter string) ([]NeighborEdge, error) {
	var result []NeighborEdge

	outQ := `SELECT type, from_id, to_id FROM edges WHERE from_id = ?` + typeFilter
	rows, err := db.Query(outQ, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query out edges: %w", err)
	}
	for rows.Next() {
		var e NeighborEdge
		if err := rows.Scan(&e[0], &e[1], &e[2]); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	inQ := `SELECT type, from_id, to_id FROM edges WHERE to_id = ?` + typeFilter
	rows, err = db.Query(inQ, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query in edges: %w", err)
	}
	for rows.Next() {
		var e NeighborEdge
		if err := rows.Scan(&e[0], &e[1], &e[2]); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, e)
	}
	rows.Close()
	return result, rows.Err()
}

// GetAllEdges возвращает все рёбра из таблицы edges.
func GetAllEdges(db *sql.DB) ([]NeighborEdge, error) {
	rows, err := db.Query(`SELECT type, from_id, to_id FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("query all edges: %w", err)
	}
	defer rows.Close()
	var result []NeighborEdge
	for rows.Next() {
		var e NeighborEdge
		if err := rows.Scan(&e[0], &e[1], &e[2]); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func buildTypeFilter(types []string) string {
	if len(types) == 0 {
		return ""
	}
	quoted := make([]string, len(types))
	for i, t := range types {
		// types — enum значения, не user-input; quotes безопасны
		quoted[i] = "'" + strings.ReplaceAll(t, "'", "''") + "'"
	}
	return " AND type IN (" + strings.Join(quoted, ",") + ")"
}

// OverviewCounts — сводка для getProjectOverview.
type OverviewCounts struct {
	Files   int `json:"files"`
	Symbols int `json:"symbols"`
	Edges   int `json:"edges"`
}

func GetOverview(db *sql.DB) (*OverviewCounts, error) {
	var o OverviewCounts
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&o.Files); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&o.Symbols); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&o.Edges); err != nil {
		return nil, err
	}
	return &o, nil
}
