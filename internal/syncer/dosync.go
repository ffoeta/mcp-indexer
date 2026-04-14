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
	for _, f := range diff.Added {
		hash, syncErr := indexEntry(tx, f, parsers, norm)
		if syncErr != nil {
			result.Errors = append(result.Errors, *syncErr)
			if hash != "" {
				nowHash[f.Key] = hash
			}
			continue
		}
		nowHash[f.Key] = hash
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
			}
			continue
		}
		nowHash[f.Key] = hash
		result.Modified++
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

	moduleID := ""
	moduleName := ""
	if lang == "python" {
		moduleName = index.PythonModuleName(f.RelPath)
		moduleID = index.ModuleID("py", moduleName)
		if err := sqlite.UpsertModule(tx, index.ModuleRow{ModuleID: moduleID, ModuleName: moduleName}); err != nil {
			return hash, &SyncError{Key: f.Key, Stage: "index", Code: "UPSERT_MODULE", Message: err.Error()}
		}
	}

	if err := sqlite.UpsertFile(tx, index.FileRow{
		FileID: fileID, Key: f.Key, RelPath: f.RelPath,
		Lang: lang, Hash: hash, ModuleID: moduleID,
	}); err != nil {
		return hash, &SyncError{Key: f.Key, Stage: "index", Code: "UPSERT_FILE", Message: err.Error()}
	}

	if moduleID != "" {
		_ = sqlite.InsertEdge(tx, index.EdgeRow{
			Type: "contains", FromID: moduleID, ToID: fileID, Confidence: 100,
		})
	}

	var postings []index.TermPosting
	for _, t := range norm.Tokenize(f.RelPath) {
		postings = append(postings, index.TermPosting{Term: t, DocID: fileID, Weight: weightPath})
	}
	if moduleName != "" {
		modDocID := index.ModuleID("py", moduleName)
		for _, t := range norm.Tokenize(moduleName) {
			postings = append(postings, index.TermPosting{Term: t, DocID: modDocID, Weight: weightModule})
		}
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
		_ = sqlite.InsertEdge(tx, index.EdgeRow{
			Type: "imports", FromID: fileID, ToID: index.ModuleID(lang, imp), Confidence: 100,
		})
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
			toID = index.ModuleID(lang, c.Module)
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
