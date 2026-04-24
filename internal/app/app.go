package app

import (
	"bufio"
	"fmt"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/engine"
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/parse/java"
	"mcp-indexer/internal/indexer/parse/python"
	"mcp-indexer/internal/searcher/query"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// App — центральный объект приложения.
type App struct {
	Registry *services.Registry

	mu     sync.Mutex
	stores map[string]*store.Store
}

// New загружает реестр.
func New() (*App, error) {
	reg, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	return &App{
		Registry: reg,
		stores:   make(map[string]*store.Store),
	}, nil
}

// NewFromRegistry создаёт App с уже загруженным реестром (для тестов).
func NewFromRegistry(reg *services.Registry) *App {
	return &App{
		Registry: reg,
		stores:   make(map[string]*store.Store),
	}
}

// ---- service management ----

func (a *App) AddService(rootAbs, svcID, description string, mainEntities []string) (string, error) {
	abs, err := filepath.Abs(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("rootAbs inaccessible: %w", err)
	}
	if svcID == "" {
		svcID = filepath.Base(abs)
	}
	svcID = sanitizeID(svcID)

	entry := services.ServiceEntry{RootAbs: abs, Description: description, MainEntities: mainEntities}
	if err := a.Registry.Add(svcID, entry); err != nil {
		return "", err
	}

	dir := services.ServiceDir(svcID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cfgPath := services.ConfigPath(svcID)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := services.SaveConfig(cfgPath, services.DefaultConfig()); err != nil {
			return "", err
		}
	}
	ignorePath := services.IgnoreFilePath(svcID)
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(ignorePath, []byte("# service.ignore — doublestar glob patterns\n"), 0o644); err != nil {
			return "", err
		}
	}

	if err := a.Registry.Save(); err != nil {
		return "", err
	}

	// Автоматически индексируем при добавлении
	if err := a.index(svcID); err != nil {
		return svcID, fmt.Errorf("index %s: %w", svcID, err)
	}
	return svcID, nil
}

// Reindex удаляет весь индекс сервиса и переиндексирует с нуля.
func (a *App) Reindex(svcID string) error {
	if _, ok := a.Registry.Get(svcID); !ok {
		return fmt.Errorf("service %q not found", svcID)
	}
	return a.index(svcID)
}

// index выполняет полную индексацию сервиса.
func (a *App) index(svcID string) error {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return fmt.Errorf("service %q not found", svcID)
	}
	st, err := a.getStore(svcID)
	if err != nil {
		return err
	}
	cfg, err := services.LoadConfig(services.ConfigPath(svcID))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	matcher, err := services.LoadMatcher(services.IgnoreFilePath(svcID))
	if err != nil {
		return fmt.Errorf("load matcher: %w", err)
	}
	norm := buildNorm(cfg)
	parsers := buildParsers()
	svcDir := services.ServiceDir(svcID)
	return engine.Index(st.DB(), entry.RootAbs, cfg, matcher, parsers, norm, svcDir)
}

func (a *App) UpdateServiceMeta(svcID, description string, mainEntities []string) error {
	if err := a.Registry.UpdateMeta(svcID, description, mainEntities); err != nil {
		return err
	}
	return a.Registry.Save()
}

func (a *App) GetServiceInfo(svcID string) (interface{}, error) {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	return map[string]interface{}{
		"serviceId":    svcID,
		"rootAbs":      entry.RootAbs,
		"description":  entry.Description,
		"mainEntities": entry.MainEntities,
	}, nil
}

func (a *App) GetServiceConfig(svcID string) (interface{}, error) {
	if _, ok := a.Registry.Get(svcID); !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	cfg, err := services.LoadConfig(services.ConfigPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// ---- query ----

func (a *App) GetProjectOverview(svcID string) (interface{}, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	return query.GetOverview(st.DB())
}

func (a *App) Search(svcID, queryStr string, limits SearchLimits) (*SearchResponse, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cfg, _ := services.LoadConfig(services.ConfigPath(svcID))
	terms := buildNorm(cfg).Tokenize(queryStr)

	hits, err := query.Search(st.DB(), terms)
	if err != nil {
		return nil, err
	}

	resp := &SearchResponse{}
	for _, h := range hits {
		docID := h.DocID
		switch {
		case strings.HasPrefix(docID, "s:") && limits.Sym > 0 && len(resp.Sym) < limits.Sym:
			row, err := query.GetSymbolContext(st.DB(), docID)
			if err != nil || row == nil {
				continue
			}
			resp.Sym = append(resp.Sym, []interface{}{
				row.SymbolID, row.Kind, row.Name, row.FileKey, row.StartLine, row.EndLine,
			})
		case strings.HasPrefix(docID, "f:") && limits.File > 0 && len(resp.File) < limits.File:
			key := strings.TrimPrefix(docID, "f:")
			resp.File = append(resp.File, []interface{}{key})
		}
	}
	return resp, nil
}

func (a *App) GetFileContext(svcID, path string) (interface{}, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	row, err := query.GetFileContext(st.DB(), path)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("file %q not found", path)
	}
	return row, nil
}

func (a *App) GetSymbolContext(svcID, symbolID string) (interface{}, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	row, err := query.GetSymbolContext(st.DB(), symbolID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("symbol %q not found", symbolID)
	}
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return row, nil
	}
	code, _ := readLines(filepath.Join(entry.RootAbs, row.RelPath), row.StartLine, row.EndLine)
	return map[string]interface{}{
		"symbolId":  row.SymbolID,
		"fileKey":   row.FileKey,
		"kind":      row.Kind,
		"name":      row.Name,
		"qualified": row.Qualified,
		"startLine": row.StartLine,
		"endLine":   row.EndLine,
		"code":      code,
	}, nil
}

// SymbolFullResponse — полный контекст символа: метаданные + код + callers.
type SymbolFullResponse struct {
	SymbolID  string               `json:"symbolId"`
	FileKey   string               `json:"fileKey"`
	Kind      string               `json:"kind"`
	Name      string               `json:"name"`
	Qualified string               `json:"qualified"`
	StartLine int                  `json:"startLine"`
	EndLine   int                  `json:"endLine"`
	Code      string               `json:"code"`
	Callers   []query.CallerRef  `json:"callers"`
	Edges     []query.NeighborEdge `json:"edges"`
}

func (a *App) GetSymbolFull(svcID, symbolID string, edgeDepth int) (*SymbolFullResponse, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	row, err := query.GetSymbolContext(st.DB(), symbolID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("symbol %q not found", symbolID)
	}

	callers, err := query.GetCallers(st.DB(), symbolID)
	if err != nil {
		return nil, err
	}

	edges, err := query.GetNeighbors(st.DB(), symbolID, edgeDepth, nil)
	if err != nil {
		return nil, err
	}

	var code string
	if entry, ok := a.Registry.Get(svcID); ok {
		code, _ = readLines(filepath.Join(entry.RootAbs, row.RelPath), row.StartLine, row.EndLine)
	}

	return &SymbolFullResponse{
		SymbolID:  row.SymbolID,
		FileKey:   row.FileKey,
		Kind:      row.Kind,
		Name:      row.Name,
		Qualified: row.Qualified,
		StartLine: row.StartLine,
		EndLine:   row.EndLine,
		Code:      code,
		Callers:   callers,
		Edges:     edges,
	}, nil
}

// readLines читает строки [startLine, endLine] из файла (1-based, включительно).
func readLines(absPath string, startLine, endLine int) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		if line > endLine {
			break
		}
		if line >= startLine {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(scanner.Text())
		}
	}
	return sb.String(), scanner.Err()
}

func (a *App) GetNeighbors(svcID, nodeID string, depth int, edgeTypes []string) (interface{}, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	return query.GetNeighbors(st.DB(), nodeID, depth, edgeTypes)
}

func (a *App) GetAllEdges(svcID string) ([]query.NeighborEdge, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	return query.GetAllEdges(st.DB())
}

func (a *App) ListServicesSorted() []string {
	ids := a.Registry.List()
	sort.Strings(ids)
	return ids
}

// ---- internal ----

func (a *App) getStore(svcID string) (*store.Store, error) {
	if _, ok := a.Registry.Get(svcID); !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.stores[svcID]; ok {
		return s, nil
	}
	s, err := store.Open(services.DBPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("open db for %s: %w", svcID, err)
	}
	a.stores[svcID] = s
	return s, nil
}

func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.stores {
		s.Close()
	}
}

func buildNorm(cfg *services.Config) *tokenize.Normalizer {
	if cfg == nil || len(cfg.Search.StopWords) == 0 {
		return tokenize.New(tokenize.DefaultStopSet())
	}
	return tokenize.New(tokenize.BuildStopSet(cfg.Search.StopWords))
}

func buildParsers() map[string]parse.Parser {
	return map[string]parse.Parser{
		".py":   python.New(""),
		".java": java.New(),
	}
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
