package syncer

import (
	"database/sql"
	"fmt"
	"mcp-indexer/internal/index"
	"mcp-indexer/internal/index/sqlite"
	"mcp-indexer/internal/parse"
	"mcp-indexer/internal/services"
	"mcp-indexer/internal/tokenize"
	"path/filepath"
	"strings"
)

const (
	weightName      = 100.0
	weightQualified = 80.0
	weightModule    = 60.0
	weightPath      = 40.0
	weightImport    = 30.0
)

// DoSyncResult — результат doSync.
type DoSyncResult struct {
	Added    int         `json:"added"`
	Modified int         `json:"modified"`
	Deleted  int         `json:"deleted"`
	Errors   []SyncError `json:"errors"`
}

// DoSync выполняет полный цикл синхронизации.
func DoSync(
	svc services.ServiceEntry,
	svcID string,
	store *sqlite.Store,
	parsers map[string]parse.Parser,
	norm *tokenize.Normalizer,
) (*DoSyncResult, error) {
	cfg, err := services.LoadConfig(services.ConfigPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	matcher, err := services.LoadMatcher(services.IgnoreFilePath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load ignore: %w", err)
	}

	current, err := Scan(svc.RootAbs, cfg, matcher)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// Фаза 1: stat-diff — без чтения файлов, только mtime/size
	savedStat, err := services.LoadFileStat(services.FileStatPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load file-stat: %w", err)
	}
	statDiff := DiffStat(current, savedStat)

	// Ранний выход: ничего не изменилось
	if len(statDiff.Added) == 0 && len(statDiff.MaybeModified) == 0 && len(statDiff.Deleted) == 0 {
		return &DoSyncResult{Errors: []SyncError{}}, nil
	}

	// Фаза 2: хэш только для кандидатов (Added + MaybeModified)
	candidates := make(map[string]struct{}, len(statDiff.Added)+len(statDiff.MaybeModified))
	for _, k := range statDiff.Added {
		candidates[k] = struct{}{}
	}
	for _, k := range statDiff.MaybeModified {
		candidates[k] = struct{}{}
	}

	savedMap, err := services.LoadFileMap(services.FileMapPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load file-map: %w", err)
	}

	diff := DiffHashCandidates(current, savedMap, candidates)

	result := &DoSyncResult{
		Errors: diff.ReadErrors,
	}
	if result.Errors == nil {
		result.Errors = []SyncError{}
	}

	tx, err := store.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// nowHash — финальная карта, строится на базе savedMap
	nowHash := make(services.FileMap, len(savedMap))
	for k, v := range savedMap {
		nowHash[k] = v
	}

	// Удаляем deleted
	for _, key := range diff.Deleted {
		if err := sqlite.DeleteFileByKey(tx, key); err != nil {
			result.Errors = append(result.Errors, SyncError{
				Key: key, Stage: "index", Code: "DELETE_ERROR", Message: err.Error(),
			})
			continue
		}
		delete(nowHash, key)
		result.Deleted++
	}

	// Индексируем added
	var changedFileIDs []string
	for _, f := range diff.Added {
		hash, syncErr := indexEntry(tx, f, parsers, norm)
		if syncErr != nil {
			result.Errors = append(result.Errors, *syncErr)
			if hash != "" {
				nowHash[f.Key] = hash
				changedFileIDs = append(changedFileIDs, index.FileID(f.Key))
			}
			continue
		}
		nowHash[f.Key] = hash
		changedFileIDs = append(changedFileIDs, index.FileID(f.Key))
		result.Added++
	}

	// Индексируем modified
	for _, f := range diff.Modified {
		if err := sqlite.DeleteFileByKey(tx, f.Key); err != nil {
			result.Errors = append(result.Errors, SyncError{
				Key: f.Key, Stage: "index", Code: "DELETE_ERROR", Message: err.Error(),
			})
			continue
		}
		hash, syncErr := indexEntry(tx, f, parsers, norm)
		if syncErr != nil {
			result.Errors = append(result.Errors, *syncErr)
			if hash != "" {
				nowHash[f.Key] = hash
				changedFileIDs = append(changedFileIDs, index.FileID(f.Key))
			}
			continue
		}
		nowHash[f.Key] = hash
		changedFileIDs = append(changedFileIDs, index.FileID(f.Key))
		result.Modified++
	}

	// Резолюция import edges: file→file для внутренних зависимостей
	if err := resolveImportEdges(tx, changedFileIDs); err != nil {
		result.Errors = append(result.Errors, SyncError{
			Stage: "index", Code: "RESOLVE_IMPORTS", Message: err.Error(),
		})
	}

	// Резолюция base edges: x:ClassName → реальный symbolId (если уникальное совпадение)
	if err := resolveBaseEdges(tx); err != nil {
		result.Errors = append(result.Errors, SyncError{
			Stage: "index", Code: "RESOLVE_BASES", Message: err.Error(),
		})
	}

	// Резолюция calls edges: x:FQN → fileId Java-класса (если уникальное совпадение по простому имени)
	if err := resolveCallEdges(tx); err != nil {
		result.Errors = append(result.Errors, SyncError{
			Stage: "index", Code: "RESOLVE_CALLS", Message: err.Error(),
		})
	}

	// Фаза 1: commit DB
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Фаза 2: атомарно записываем карты (только после успешного commit)
	if err := services.SaveFileMap(services.FileMapPath(svcID), nowHash); err != nil {
		result.Errors = append(result.Errors, SyncError{
			Stage: "index", Code: "MAP_WRITE_ERROR",
			Message: err.Error(), Hint: "re-run doSync to reconcile",
		})
		return result, nil
	}

	// file-stat.json строится из текущего скана
	nowStat := make(services.FileStat, len(current))
	for _, f := range current {
		nowStat[f.Key] = [2]int64{f.ModTime.UnixNano(), f.Size}
	}
	if err := services.SaveFileStat(services.FileStatPath(svcID), nowStat); err != nil {
		result.Errors = append(result.Errors, SyncError{
			Stage: "index", Code: "MAP_WRITE_ERROR",
			Message: err.Error(), Hint: "re-run doSync to reconcile",
		})
	}
	return result, nil
}

// indexEntry вставляет файл в индекс. Возвращает (hash, *SyncError).
// При parse error: file-запись создана, hash возвращается, SyncError ненулевой.
func indexEntry(
	tx *sql.Tx,
	f FileEntry,
	parsers map[string]parse.Parser,
	norm *tokenize.Normalizer,
) (string, *SyncError) {
	hash, err := HashFile(f.AbsPath)
	if err != nil {
		return "", &SyncError{
			Key: f.Key, AbsPath: f.AbsPath, Stage: "hash", Code: "HASH_ERROR",
			Message: err.Error(), Hint: "check file permissions",
		}
	}

	lang := langFromExt(filepath.Ext(f.AbsPath))
	fileID := index.FileID(f.Key)

	if err := sqlite.UpsertFile(tx, index.FileRow{
		FileID: fileID, Key: f.Key, RelPath: f.RelPath,
		Lang: lang, Hash: hash,
	}); err != nil {
		return hash, &SyncError{Key: f.Key, Stage: "index", Code: "UPSERT_FILE", Message: err.Error()}
	}

	var postings []index.TermPosting
	for _, t := range norm.Tokenize(f.RelPath) {
		postings = append(postings, index.TermPosting{Term: t, DocID: fileID, Weight: weightPath})
	}

	ext := filepath.Ext(f.AbsPath)
	p, hasParserer := parsers[ext]
	if !hasParserer {
		_ = sqlite.InsertTermPostings(tx, postings)
		return hash, nil
	}

	pr, parseErr := p.Parse(f.AbsPath)
	if parseErr != nil {
		_ = sqlite.InsertTermPostings(tx, postings)
		if pe, ok := parseErr.(*parse.ParseError); ok {
			var pos *Pos
			if pe.Line > 0 {
				pos = &Pos{Line: pe.Line, Col: pe.Col}
			}
			return hash, &SyncError{
				Key: f.Key, AbsPath: f.AbsPath, Stage: "parse",
				Code: "PARSE_ERROR", Message: pe.Message, Pos: pos,
				Hint: "fix syntax error and re-sync",
			}
		}
		return hash, &SyncError{
			Key: f.Key, AbsPath: f.AbsPath, Stage: "parse",
			Code: "PARSE_ERROR", Message: parseErr.Error(),
			Hint: "fix syntax error and re-sync",
		}
	}

	imports := make([]index.ImportRow, len(pr.Imports))
	for i, imp := range pr.Imports {
		imports[i] = index.ImportRow{FileID: fileID, Imp: imp}
		for _, t := range norm.Tokenize(imp) {
			postings = append(postings, index.TermPosting{Term: t, DocID: fileID, Weight: weightImport})
		}
	}
	if err := sqlite.InsertImports(tx, imports); err != nil {
		return hash, &SyncError{Key: f.Key, Stage: "index", Code: "INSERT_IMPORTS", Message: err.Error()}
	}

	localSymbols := make(map[string]string) // name → symID для резолюции calls
	for _, sym := range pr.Symbols {
		qualified := sym.Qualified
		if qualified == "" {
			qualified = sym.Name
		}
		symID := index.SymbolID(lang, qualified, f.Key, sym.StartLine)
		localSymbols[sym.Name] = symID
		if err := sqlite.InsertSymbol(tx, index.SymbolRow{
			SymbolID: symID, FileID: fileID, Kind: sym.Kind,
			Name: sym.Name, Qualified: qualified,
			StartLine: sym.StartLine, EndLine: sym.EndLine,
		}); err != nil {
			return hash, &SyncError{Key: f.Key, Stage: "index", Code: "INSERT_SYMBOL", Message: err.Error()}
		}
		_ = sqlite.InsertEdge(tx, index.EdgeRow{
			Type: "defines", FromID: fileID, ToID: symID, Confidence: 100,
		})
		for _, base := range sym.Bases {
			_ = sqlite.InsertEdge(tx, index.EdgeRow{
				Type: "base", FromID: symID, ToID: index.UnresolvedID(base),
				Confidence: 30, Aux: base,
			})
		}
		for _, t := range norm.Tokenize(sym.Name) {
			postings = append(postings, index.TermPosting{Term: t, DocID: symID, Weight: weightName})
		}
		for _, t := range norm.Tokenize(qualified) {
			postings = append(postings, index.TermPosting{Term: t, DocID: symID, Weight: weightQualified})
		}
	}

	// Emit calls edges (дедуплицированы по target)
	seenEdge := make(map[string]bool)
	for _, c := range pr.Calls {
		var toID string
		switch {
		case c.Module != "":
			toID = index.UnresolvedID(c.Module)
		case c.Local != "":
			if symID, ok := localSymbols[c.Local]; ok {
				toID = symID
			} else {
				toID = index.UnresolvedID(c.Local)
			}
		default:
			toID = index.UnresolvedID(c.Name)
		}
		if !seenEdge[toID] {
			seenEdge[toID] = true
			_ = sqlite.InsertEdge(tx, index.EdgeRow{
				Type: "calls", FromID: fileID, ToID: toID, Confidence: 70,
			})
		}
	}

	if err := sqlite.InsertTermPostings(tx, postings); err != nil {
		return hash, &SyncError{Key: f.Key, Stage: "index", Code: "INSERT_POSTINGS", Message: err.Error()}
	}
	return hash, nil
}

// resolveImportEdges строит file→file edges типа "imports" для изменённых файлов.
// Вызывается после того, как все новые/изменённые файлы уже записаны в транзакцию,
// чтобы moduleMap включал и только что добавленные файлы.
func resolveImportEdges(tx *sql.Tx, fileIDs []string) error {
	if len(fileIDs) == 0 {
		return nil
	}
	moduleMap, err := sqlite.BuildModuleFileMap(tx)
	if err != nil {
		return err
	}
	for _, fileID := range fileIDs {
		rows, err := tx.Query(`SELECT imp FROM imports WHERE file_id = ?`, fileID)
		if err != nil {
			return fmt.Errorf("query imports for %s: %w", fileID, err)
		}
		for rows.Next() {
			var imp string
			if err := rows.Scan(&imp); err != nil {
				rows.Close()
				return err
			}
			if targetID, ok := moduleMap[imp]; ok && targetID != fileID {
				_ = sqlite.InsertEdge(tx, index.EdgeRow{
					Type: "imports", FromID: fileID, ToID: targetID, Confidence: 100,
				})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// resolveBaseEdges заменяет x:ClassName → реальный symbolId в base edges.
// Резолюция выполняется только если имя однозначно (ровно один класс с таким именем).
func resolveBaseEdges(tx *sql.Tx) error {
	// Строим карту name → []symbolId для всех class-символов
	rows, err := tx.Query(`SELECT symbol_id, name FROM symbols WHERE kind = 'class'`)
	if err != nil {
		return fmt.Errorf("query class symbols: %w", err)
	}
	nameToSyms := map[string][]string{}
	for rows.Next() {
		var symID, name string
		if err := rows.Scan(&symID, &name); err != nil {
			rows.Close()
			return err
		}
		nameToSyms[name] = append(nameToSyms[name], symID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Находим все неразрешённые base edges
	edgeRows, err := tx.Query(`SELECT from_id, to_id, aux FROM edges WHERE type = 'base' AND to_id LIKE 'x:%'`)
	if err != nil {
		return fmt.Errorf("query unresolved base edges: %w", err)
	}
	type unresEdge struct{ fromID, toID, aux string }
	var unresolved []unresEdge
	for edgeRows.Next() {
		var e unresEdge
		if err := edgeRows.Scan(&e.fromID, &e.toID, &e.aux); err != nil {
			edgeRows.Close()
			return err
		}
		unresolved = append(unresolved, e)
	}
	edgeRows.Close()
	if err := edgeRows.Err(); err != nil {
		return err
	}

	for _, e := range unresolved {
		syms := nameToSyms[e.aux]
		if len(syms) != 1 {
			continue // неоднозначно или не найдено — оставляем unresolved
		}
		if _, err := tx.Exec(
			`UPDATE edges SET to_id = ? WHERE type = 'base' AND from_id = ? AND to_id = ?`,
			syms[0], e.fromID, e.toID,
		); err != nil {
			return fmt.Errorf("resolve base edge %s→%s: %w", e.fromID, e.toID, err)
		}
	}
	return nil
}

// resolveCallEdges заменяет x:FQN → fileId для Java-классов, присутствующих в индексе.
// Резолюция по простому имени класса (последний компонент FQN). Пропускает неоднозначные совпадения.
func resolveCallEdges(tx *sql.Tx) error {
	// Строим карту: простое имя класса → fileId для Java-файлов.
	// Ключ — имя файла без расширения (последний компонент relPath).
	rows, err := tx.Query(`SELECT file_id, rel_path FROM files WHERE lang = 'java'`)
	if err != nil {
		return fmt.Errorf("query java files: %w", err)
	}
	simpleToFile := map[string][]string{} // simpleName → []fileId
	for rows.Next() {
		var fileID, relPath string
		if err := rows.Scan(&fileID, &relPath); err != nil {
			rows.Close()
			return err
		}
		// relPath: "src/main/java/ru/hh/.../ClassName.java" → "ClassName"
		base := filepath.Base(relPath)
		if ext := filepath.Ext(base); ext == ".java" {
			simpleName := base[:len(base)-len(ext)]
			simpleToFile[simpleName] = append(simpleToFile[simpleName], fileID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Находим все calls edges с unresolved target
	edgeRows, err := tx.Query(`SELECT from_id, to_id FROM edges WHERE type = 'calls' AND to_id LIKE 'x:%'`)
	if err != nil {
		return fmt.Errorf("query unresolved calls: %w", err)
	}
	type callEdge struct{ fromID, toID string }
	var unresolved []callEdge
	for edgeRows.Next() {
		var e callEdge
		if err := edgeRows.Scan(&e.fromID, &e.toID); err != nil {
			edgeRows.Close()
			return err
		}
		unresolved = append(unresolved, e)
	}
	edgeRows.Close()
	if err := edgeRows.Err(); err != nil {
		return err
	}

	for _, e := range unresolved {
		// Извлекаем FQN из "x:ru.hh...ClassName" → простое имя "ClassName"
		fqn := e.toID[2:] // strip "x:"
		simpleName := fqn
		if idx := strings.LastIndex(fqn, "."); idx >= 0 {
			simpleName = fqn[idx+1:]
		}
		files := simpleToFile[simpleName]
		if len(files) != 1 {
			continue // не найдено или неоднозначно
		}
		if _, err := tx.Exec(
			`UPDATE edges SET to_id = ? WHERE type = 'calls' AND from_id = ? AND to_id = ?`,
			files[0], e.fromID, e.toID,
		); err != nil {
			return fmt.Errorf("resolve call edge %s→%s: %w", e.fromID, e.toID, err)
		}
	}
	return nil
}

func langFromExt(ext string) string {
	switch ext {
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".go":
		return "go"
	default:
		return "unknown"
	}
}
