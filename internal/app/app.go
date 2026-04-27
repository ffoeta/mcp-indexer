// Package app — оркестратор: связывает services/store/engine/query.
// Возвращает уже compact JSON-готовые структуры для MCP-уровня.
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

func New() (*App, error) {
	reg, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	return &App{Registry: reg, stores: make(map[string]*store.Store)}, nil
}

func NewFromRegistry(reg *services.Registry) *App {
	return &App{Registry: reg, stores: make(map[string]*store.Store)}
}

func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.stores {
		s.Close()
	}
}

// ───────── service management ─────────

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
	if err := a.index(svcID); err != nil {
		return svcID, fmt.Errorf("index %s: %w", svcID, err)
	}
	return svcID, nil
}

func (a *App) Sync(svcID string) error {
	if _, ok := a.Registry.Get(svcID); !ok {
		return fmt.Errorf("service %q not found", svcID)
	}
	return a.index(svcID)
}

func (a *App) UpdateServiceMeta(svcID, description string, mainEntities []string) error {
	if err := a.Registry.UpdateMeta(svcID, description, mainEntities); err != nil {
		return err
	}
	return a.Registry.Save()
}

func (a *App) GetServiceInfo(svcID string) (*ServiceOut, error) {
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	return &ServiceOut{
		ID: svcID, Root: entry.RootAbs,
		Description: entry.Description, MainEntities: entry.MainEntities,
	}, nil
}

func (a *App) ListServices() []ServiceOut {
	full := a.Registry.ListFull()
	out := make([]ServiceOut, 0, len(full))
	for id, e := range full {
		out = append(out, ServiceOut{
			ID: id, Root: e.RootAbs,
			Description: e.Description, MainEntities: e.MainEntities,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (a *App) ListServicesSorted() []string {
	ids := a.Registry.List()
	sort.Strings(ids)
	return ids
}

// index — internal, full reindex.
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

// ───────── search ─────────

// Search ищет по FTS5. kind ∈ {"", "method", "object", "file"}; "" — без фильтра.
func (a *App) Search(svcID, q, kind string, limit int) (*SearchOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cfg, _ := services.LoadConfig(services.ConfigPath(svcID))
	norm := buildNorm(cfg)
	expr := strings.Join(norm.Tokenize(q), " ")
	hits, err := query.FTSearch(st.DB(), expr, kind, limit)
	if err != nil {
		return nil, err
	}
	out := &SearchOut{}
	for _, h := range hits {
		k := "file"
		if h.DocKind == store.DocNode {
			// nodeKind определяется во FTSearch и записан в kind фильтра — но FTSearch
			// возвращает Hit без node.kind. Доберём.
			var nk string
			st.DB().QueryRow(`SELECT kind FROM nodes WHERE node_id=?`, h.DocID).Scan(&nk)
			k = nk
		}
		out.Hits = append(out.Hits, SearchHitOut{
			ID:   shortID(k, h.ShortID),
			K:    k,
			Name: h.Name,
			FQN:  h.FQN,
			File: h.Path,
			L:    h.Line,
		})
	}
	return out, nil
}

// ───────── peek ─────────

// Peek возвращает PeekObjectOut / PeekMethodOut / PeekFileOut в зависимости от ID.
func (a *App) Peek(svcID, idOrShort string) (interface{}, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cid, kind, err := query.ResolveID(st.DB(), idOrShort)
	if err != nil {
		return nil, err
	}
	if kind == store.DocFile {
		return a.peekFile(st, cid)
	}
	n, err := query.GetNode(st.DB(), cid)
	if err != nil {
		return nil, err
	}
	if n.Kind == store.KindObject {
		return a.peekObject(st, n)
	}
	return a.peekMethod(n), nil
}

func (a *App) peekFile(st *store.Store, fileID string) (*PeekFileOut, error) {
	fi, err := query.GetFile(st.DB(), fileID)
	if err != nil {
		return nil, err
	}
	return &PeekFileOut{
		ID:      store.ShortFileID(fi.ShortID),
		K:       "file",
		Path:    fi.Path,
		Lang:    fi.Lang,
		Pkg:     fi.Package,
		Objects: len(fi.Objects),
		Methods: len(fi.Methods),
		Imports: len(fi.Imports),
	}, nil
}

func (a *App) peekObject(st *store.Store, n *query.NodeInfo) (*PeekObjectOut, error) {
	out := &PeekObjectOut{
		ID:         store.ShortNodeID(store.KindObject, n.ShortID),
		K:          "object",
		Subk:       n.Subkind,
		Name:       n.Name,
		FQN:        n.FQN,
		File:       n.File.Path,
		L:          n.StartLine,
		End:        n.EndLine,
		Doc:        n.Doc,
		ExtendedBy: n.ExtendedByCount,
	}
	if n.OwnerID != "" {
		out.Owner = store.ShortNodeID(store.KindObject, n.OwnerShort)
	}

	// methods (short IDs)
	if w, err := query.Walk(st.DB(), n.NodeID, query.EdgeDefines, query.DirOut, 200, 0); err == nil {
		for _, it := range w.Items {
			out.Methods = append(out.Methods, store.ShortNodeID(store.KindMethod, it.OtherShort))
		}
	}
	// inherits out (parents) — разделяем по relation
	if w, err := query.Walk(st.DB(), n.NodeID, query.EdgeInherits, query.DirOut, 50, 0); err == nil {
		for _, it := range w.Items {
			if it.OtherID == "" {
				continue
			}
			short := store.ShortNodeID(store.KindObject, it.OtherShort)
			switch it.Relation {
			case store.RelImplements:
				out.Implements = append(out.Implements, short)
			default:
				out.Extends = append(out.Extends, short)
			}
		}
	}
	return out, nil
}

func (a *App) peekMethod(n *query.NodeInfo) *PeekMethodOut {
	out := &PeekMethodOut{
		ID:              store.ShortNodeID(store.KindMethod, n.ShortID),
		K:               "method",
		Subk:            n.Subkind,
		Name:            n.Name,
		FQN:             n.FQN,
		File:            n.File.Path,
		L:               n.StartLine,
		End:             n.EndLine,
		Sig:             n.Signature,
		Doc:             n.Doc,
		Calls:           n.CallsCount,
		CalledBy:        n.CalledByCount,
		UnresolvedCalls: n.UnresolvedCalls,
	}
	if n.OwnerID != "" {
		out.Owner = store.ShortNodeID(store.KindObject, n.OwnerShort)
	}
	return out
}

// ───────── walk ─────────

// Walk возвращает рёбра вокруг ноды/файла в формате compact.
func (a *App) Walk(svcID, idOrShort, edge, dir string, limit, offset int) (*WalkOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cid, _, err := query.ResolveID(st.DB(), idOrShort)
	if err != nil {
		return nil, err
	}
	d := query.Direction(dir)
	if d == "" {
		d = query.DirBoth
	}
	res, err := query.Walk(st.DB(), cid, query.EdgeKind(edge), d, limit, offset)
	if err != nil {
		return nil, err
	}
	out := &WalkOut{Total: res.Total}
	for _, w := range res.Items {
		item := WalkItem{
			Hint: w.Hint,
			Line: w.Line,
			Conf: w.Confidence,
			Rel:  w.Relation,
			Name: w.OtherName,
			FQN:  w.OtherFQN,
			File: w.OtherFile,
			L:    w.OtherLine,
		}
		if w.OtherID != "" {
			short := compactShort(w.OtherKind, w.OtherShort, w.OtherID)
			// направление: для DirIn other — это "from"; иначе — "to".
			if d == query.DirIn {
				item.From = short
			} else {
				item.To = short
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// compactShort выбирает префикс по docKind. Для node нужен kind ("method"/"object"),
// который мы вытягиваем из canonical id-prefix или (если не получилось) — из БД.
// Для simplicity берём из canonical: "n:lang:m:..." vs "n:lang:o:...".
func compactShort(docKind string, shortID int64, canonical string) string {
	if docKind == store.DocFile {
		return store.ShortFileID(shortID)
	}
	// canonical = "n:{lang}:{m|o}:..."
	if len(canonical) > 5 {
		switch canonical[len("n:python:"):][0] {
		case 'm':
			return store.ShortNodeID(store.KindMethod, shortID)
		case 'o':
			return store.ShortNodeID(store.KindObject, shortID)
		}
	}
	// fallback
	return store.ShortNodeID(store.KindMethod, shortID)
}

// ───────── code ─────────

// Code возвращает текст диапазона строк ноды. ctx — лишние строки до/после.
func (a *App) Code(svcID, idOrShort string, ctx int) (*CodeOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	cid, kind, err := query.ResolveID(st.DB(), idOrShort)
	if err != nil {
		return nil, err
	}
	if kind != store.DocNode {
		return nil, fmt.Errorf("code() works only for nodes (method/object), got file")
	}
	cr, err := query.GetCodeRange(st.DB(), cid)
	if err != nil {
		return nil, err
	}
	entry, ok := a.Registry.Get(svcID)
	if !ok {
		return nil, fmt.Errorf("service %q not found", svcID)
	}
	start := cr.StartLine - ctx
	if start < 1 {
		start = 1
	}
	end := cr.EndLine + ctx
	src, err := readLines(filepath.Join(entry.RootAbs, cr.FilePath), start, end)
	if err != nil {
		return nil, err
	}
	return &CodeOut{
		ID:    idOrShort,
		File:  cr.FilePath,
		Start: start,
		End:   end,
		Src:   src,
	}, nil
}

// ───────── file ─────────

// File возвращает overview файла без исходника.
func (a *App) File(svcID, pathOrID string) (*FileOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	fi, err := query.GetFile(st.DB(), pathOrID)
	if err != nil {
		return nil, err
	}
	out := &FileOut{
		ID:   store.ShortFileID(fi.ShortID),
		Path: fi.Path,
		Lang: fi.Lang,
		Pkg:  fi.Package,
	}
	for _, ir := range fi.Imports {
		row := FileImportOut{Raw: ir.Raw}
		if ir.TargetFileID != "" {
			row.Tgt = store.ShortFileID(ir.TargetShortID)
		}
		out.Imports = append(out.Imports, row)
	}
	for _, o := range fi.Objects {
		out.Objects = append(out.Objects, NodeBriefOut{
			ID:      store.ShortNodeID(store.KindObject, o.ShortID),
			Subk:    o.Subkind,
			Name:    o.Name,
			L:       o.StartLine,
			End:     o.EndLine,
			Methods: o.MethodCount,
		})
	}
	for _, m := range fi.Methods {
		out.Methods = append(out.Methods, NodeBriefOut{
			ID:   store.ShortNodeID(store.KindMethod, m.ShortID),
			Subk: m.Subkind,
			Name: m.Name,
			L:    m.StartLine,
			End:  m.EndLine,
		})
	}
	return out, nil
}

// ───────── tree ─────────

// Tree возвращает плоский список всех файлов сервиса с агрегатами.
// JS-сторона UI группирует их в дерево по каталогам.
func (a *App) Tree(svcID string) (*TreeOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	rows, err := query.ListFiles(st.DB())
	if err != nil {
		return nil, err
	}
	out := &TreeOut{Files: make([]TreeFileOut, 0, len(rows))}
	for _, r := range rows {
		out.Files = append(out.Files, TreeFileOut{
			ID:      store.ShortFileID(r.ShortID),
			Path:    r.Path,
			Lang:    r.Lang,
			Pkg:     r.Package,
			Objects: r.Objects,
			Methods: r.Methods,
		})
	}
	return out, nil
}

// ───────── stats ─────────

func (a *App) Stats(svcID string) (*StatsOut, error) {
	st, err := a.getStore(svcID)
	if err != nil {
		return nil, err
	}
	s, err := query.GetStats(st.DB())
	if err != nil {
		return nil, err
	}
	return &StatsOut{
		Files:           s.Files,
		Objects:         s.Objects,
		Methods:         s.Methods,
		CallsResolved:   s.CallsResolved,
		CallsUnresolved: s.CallsUnresolved,
		Inherits:        s.Inherits,
		ImportsResolved: s.ImportsResolved,
		ImportsExternal: s.ImportsExternal,
		SearchDocs:      s.FTSDocCount,
	}, nil
}

// ───────── internal helpers ─────────

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

// shortID формирует строковую форму short-ID по kind/num.
func shortID(kind string, num int64) string {
	switch kind {
	case "file":
		return store.ShortFileID(num)
	case store.KindObject, store.KindMethod:
		return store.ShortNodeID(kind, num)
	}
	return ""
}

// readLines читает строки [startLine..endLine] из файла (1-based, включительно).
func readLines(absPath string, startLine, endLine int) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
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