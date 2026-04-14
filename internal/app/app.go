package app

import (
	"fmt"
	"mcp-indexer/internal/parse"
	"mcp-indexer/internal/parse/java"
	"mcp-indexer/internal/parse/python"
	"mcp-indexer/internal/services"
	"mcp-indexer/internal/syncer"
	"mcp-indexer/internal/tokenize"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sqliteq "mcp-indexer/internal/index/sqlite"
)

// App — центральный объект приложения.
type App struct {
	Registry *services.Registry

	mu     sync.Mutex
	stores map[string]*sqliteq.Store
}

// New загружает реестр.
func New() (*App, error) {
	reg, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	return &App{
		Registry: reg,
		stores:   make(map[string]*sqliteq.Store),
	}, nil
}

// NewFromRegistry создаёт App с уже загруженным реестром (для тестов).
func NewFromRegistry(reg *services.Registry) *App {
	return &App{
		Registry: reg,
		stores:   make(map[string]*sqliteq.Store),
	}
}

// ---- service management ----

func (a *App) AddService(rootAbs, svcID, name string) (string, error) {
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

	entry := services.ServiceEntry{RootAbs: abs, Name: name}
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
	return svcID, nil
}

func (a *App) GetServiceInfo(svcID string) (interface{}, error) {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	cfg, _ := services.LoadConfig(services.ConfigPath(svcID))
	return map[string]interface{}{
		"serviceId": svcID,
		"rootAbs":   entry.RootAbs,
		"name":      entry.Name,
		"config":    cfg,
	}, nil
}

// ---- sync ----

func (a *App) PrepareSync(svcID string) (*syncer.PrepareSyncResult, error) {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	return syncer.PrepareSync(entry, svcID)
}

func (a *App) DoSync(svcID string) (*syncer.DoSyncResult, error) {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cfg, err := services.LoadConfig(services.ConfigPath(svcID))
	if err != nil {
		return nil, err
	}
	return syncer.DoSync(entry, svcID, store, buildParsers(), buildNorm(cfg))
}

// ---- query ----

func (a *App) GetProjectOverview(svcID string) (interface{}, error) {
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	return sqliteq.GetOverview(store.DB())
}

func (a *App) Search(svcID, query string, limits SearchLimits) (*SearchResponse, error) {
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cfg, _ := services.LoadConfig(services.ConfigPath(svcID))
	terms := buildNorm(cfg).Tokenize(query)

	hits, err := sqliteq.Search(store.DB(), terms)
	if err != nil {
		return nil, err
	}

	resp := &SearchResponse{}
	for _, h := range hits {
		docID := h.DocID
		switch {
		case strings.HasPrefix(docID, "s:") && limits.Sym > 0 && len(resp.Sym) < limits.Sym:
			row, err := sqliteq.GetSymbolContext(store.DB(), docID)
			if err != nil || row == nil {
				continue
			}
			resp.Sym = append(resp.Sym, []interface{}{
				row.SymbolID, row.Kind, row.Name, row.FileKey, row.StartLine, row.EndLine,
			})
		case strings.HasPrefix(docID, "f:") && limits.File > 0 && len(resp.File) < limits.File:
			key := strings.TrimPrefix(docID, "f:")
			resp.File = append(resp.File, []interface{}{key})
		case strings.HasPrefix(docID, "m:") && limits.Mod > 0 && len(resp.Mod) < limits.Mod:
			var modName string
			if err := store.DB().QueryRow(
				`SELECT module_name FROM modules WHERE module_id=?`, docID,
			).Scan(&modName); err == nil {
				resp.Mod = append(resp.Mod, []interface{}{docID, modName})
			}
		}
	}
	return resp, nil
}

func (a *App) GetFileContext(svcID, path string) (interface{}, error) {
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	row, err := sqliteq.GetFileContext(store.DB(), path)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("file %q not found", path)
	}
	return row, nil
}

func (a *App) GetSymbolContext(svcID, symbolID string) (interface{}, error) {
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	row, err := sqliteq.GetSymbolContext(store.DB(), symbolID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("symbol %q not found", symbolID)
	}
	return row, nil
}

func (a *App) GetNeighbors(svcID, nodeID string, depth int, edgeTypes []string) (interface{}, error) {
	store, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	return sqliteq.GetNeighbors(store.DB(), nodeID, depth, edgeTypes)
}

func (a *App) ListServicesSorted() []string {
	ids := a.Registry.List()
	sort.Strings(ids)
	return ids
}

// ---- internal ----

func (a *App) getStore(svcID string) (*sqliteq.Store, error) {
	if _, ok := a.Registry.Get(svcID); !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.stores[svcID]; ok {
		return s, nil
	}
	s, err := sqliteq.Open(services.DBPath(svcID))
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
