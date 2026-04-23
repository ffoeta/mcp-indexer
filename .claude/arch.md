# mcp-indexer — Architecture Reference

## Stack

| Component | Library | Version | Purpose |
|---|---|---|---|
| Language | Go | 1.23 | — |
| SQLite | modernc.org/sqlite | v1.36.1 | Pure-Go driver, no CGO |
| Tree-sitter | smacker/go-tree-sitter | — | Python, Java parsing |
| MCP | mark3labs/mcp-go | v0.32.0 | MCP stdio server framework |
| CLI | spf13/cobra | v1.8.1 | Subcommand framework |
| Hash | zeebo/blake3 | v0.2.4 | Fast file hashing |
| Glob | bmatcuk/doublestar/v4 | v4.7.1 | gitignore-style pattern matching |

Module: `mcp-indexer` (go.mod). Binary built via `run.sh build` → `~/bin/mcp-indexer`.

---

## Directory Structure

```
cmd/
  main.go              → rootCmd + 9 subcommands (serve, list, add, prepare-sync, do-sync, search, file-context, neighbors, viz)
  viz.go               → viz subcommand (HTTP graph visualization)
internal/
  app/
    app.go             → App struct: central orchestrator, all business logic
    types.go           → SearchLimits, SearchResponse, SymbolFullResponse
  mcp/
    server.go          → Register(srv, app): 14 MCP tools
  services/
    registry.go        → Registry: thread-safe map[serviceId]ServiceEntry, JSON persistence
    service.go         → ServiceEntry struct
    config.go          → Config, SearchConfig, DefaultConfig, DefaultStopWords
    filemap.go         → FileMap, FileStat, atomicWriteFile
    ignore.go          → Matcher: doublestar glob with gitignore semantics
    paths.go           → AppHome, RegistryPath, ServiceDir, ConfigPath, etc.
  syncer/
    scan.go            → Scan(rootAbs, Config, Matcher) → []FileEntry
    diff.go            → DiffStat, DiffHash, DiffHashCandidates
    dosync.go          → DoSync: 2-phase pipeline orchestration
    prepare.go         → PrepareSync: stat-only preview
    hash.go            → HashFile(path) → "b3:<hex>"
    errors.go          → SyncError, Pos
  parse/
    parser.go          → Parser interface, ParseResult, SymbolDef, CallRef, ParseError
    treesitter/
      parser.go        → treesitter.Parser struct, extractor interface
      python.go        → pyExtractor: 2-pass (declarations + calls)
      java.go          → javaExtractor: imports, classes, methods, calls
    python/
      runner.go        → python.New() → parse.Parser (wrapper)
    java/
      treesitter.go    → java.New() → parse.Parser (wrapper)
  tokenize/
    normalize.go       → Normalizer.Tokenize pipeline
    split.go           → SplitDelimiters, SplitCamel
    stem.go            → Stem: conservative suffix stripping
    stopwords.go       → DefaultStopSet, BuildStopSet
  index/
    model.go           → FileRow, ImportRow, SymbolRow, EdgeRow, TermPosting
    ids.go             → FileID, SymbolID, UnresolvedID, PythonModuleName
  index/sqlite/
    ddl.sql            → Schema DDL + PRAGMAs (embedded via go:embed)
    store.go           → Open, Close, Begin, DB, legacy migrations
    upsert.go          → UpsertFile, InsertImports, InsertSymbol, InsertEdge, InsertTermPostings
    delete.go          → DeleteFileByKey: manual edge/posting cleanup + CASCADE
    query.go           → Search, GetFileContext, GetSymbolContext, GetCallers, GetNeighbors, GetAllEdges, GetOverview, BuildModuleFileMap
  viz/
    server.go          → Serve(): HTTP server with D3.js force graph
test/
  integration/         → integration_general_test.go, integration_java_test.go, integration_python_test.go
  testdata/            → python/, java/ sample source files
```

---

## Architecture Layers

```
┌─────────────────────────────────────────┐
│  cmd/main.go                            │  Entrypoint: MCP stdio + CLI
├─────────────────────────────────────────┤
│  internal/mcp/server.go                 │  Thin MCP tool layer (param extraction)
├─────────────────────────────────────────┤
│  internal/app/app.go                    │  Orchestrator: business logic
├──────────┬──────────┬───────────────────┤
│ services │ syncer   │ parse / tokenize  │  Domain packages
├──────────┴──────────┴───────────────────┤
│  internal/index/sqlite                  │  Data layer (raw SQL)
└─────────────────────────────────────────┘
```

**Data flow**: MCP request → `mcp/server.go` (param extraction) → `app.go` (orchestration) → `services` / `syncer` / `parse` / `tokenize` / `sqlite` → response.

---

## App Struct

`internal/app/app.go` (393 lines)

```go
type App struct {
    Registry *services.Registry
    stores   map[string]*sqlite.Store  // lazy-opened, mutex-protected
    mu       sync.Mutex
}
```

### Lifecycle

- `New()` — loads registry from `~/.mcp-indexer/registry.json`
- `Close()` — closes all SQLite stores

### Key Methods

| Method | Purpose |
|---|---|
| `AddService(rootAbs, svcID, desc, mainEntities)` | Register service, create dir + config + ignore |
| `UpdateServiceMeta(svcID, desc, mainEntities)` | Update metadata |
| `GetServiceInfo(svcID)` | Full service info |
| `GetServiceConfig(svcID)` | Config details |
| `PrepareSync(svcID)` | Stat-only diff preview |
| `DoSync(svcID)` | Full sync pipeline |
| `GetProjectOverview(svcID)` | File/symbol/edge counts |
| `Search(svcID, query, limits)` | Full-text search |
| `GetFileContext(svcID, path)` | File + imports + symbols |
| `GetSymbolContext(svcID, symbolID)` | Symbol metadata + source code |
| `GetSymbolFull(svcID, symbolID, edgeDepth)` | Symbol + code + callers + edges |
| `GetNeighbors(svcID, nodeID, depth, edgeTypes)` | BFS graph traversal |

### Internal Helpers

- `getStore(svcID)` — lazy-opens SQLite, mutex-protected
- `buildParsers()` — returns `map[string]parse.Parser{".py": python.New(""), ".java": java.New()}`
- `buildNorm(svcID)` — returns `*tokenize.Normalizer` with service-specific stop words

---

## MCP Tools

Registered in `internal/mcp/server.go` via `Register(srv *server.MCPServer, a *app.App)`. 14 tools:

| # | Tool | Required Params | Optional Params | Description |
|---|---|---|---|---|
| 1 | `help` | — | — | Server description + tool docs |
| 2 | `debug_get_config` | — | — | Returns MCP_INDEXER_HOME |
| 3 | `get_service_list` | — | — | id → rootAbs map |
| 4 | `add_service` | `rootAbs` | `serviceId`, `description`, `mainEntities` | Register codebase root |
| 5 | `get_service_meta` | `serviceId` | — | Full service metadata |
| 6 | `update_service_meta` | `serviceId` | `description`, `mainEntities` | Update metadata |
| 7 | `prepare_sync` | `serviceId` | — | Dry-run stat diff |
| 8 | `sync` | `serviceId` | — | Hash diff + apply |
| 9 | `debug_get_project_stats` | `serviceId` | — | File/symbol/edge counts |
| 10 | `debug_get_project_config` | `serviceId` | — | Config.json content |
| 11 | `search` | `serviceId`, `query` | `limits` (JSON `{"sym":20,"file":10}`) | Full-text search |
| 12 | `get_file_context` | `serviceId`, `path` | — | File + imports + symbols |
| 13 | `get_symbol_context` | `serviceId`, `symbolId` | — | Symbol metadata + source |
| 14 | `get_symbol_full` | `serviceId`, `symbolId` | `edgeDepth` (default 1) | Symbol + code + callers + edges |
| 15 | `get_neighbors` | `serviceId`, `nodeId` | `depth` (default 2), `edgeTypes` (CSV) | BFS graph traversal |

All responses use `jsonResult(v)` or `errResult(err)` helpers.

---

## Sync Pipeline

`internal/syncer/dosync.go` — `DoSync()` (~506 lines)

### 2-Phase Diff

```
Phase 1: Stat-diff (no file reads)
  Scan(rootAbs, Config, Matcher) → []FileEntry
  DiffStat(current, savedFileStat) → StatDiffResult{Added, MaybeModified, Deleted}
  If nothing changed → early exit

Phase 2: Hash-diff (candidates only)
  DiffHashCandidates(current, savedFileMap, candidates) → HashDiffResult{Added, Modified, Deleted}
  Only reads/hashes files in MaybeModified set
```

### Index Transaction

Single SQLite transaction:

1. **Delete removed files** — `DeleteFileByKey` for each deleted key
2. **Index added files** — `indexEntry()` for each added file
3. **Re-index modified files** — delete old entry + `indexEntry()` (full replace, not patch)
4. **Resolve edges** (3 passes):
   - `resolveImportEdges()` — Python: module name → fileID, create `imports` edges
   - `resolveBaseEdges()` — `x:ClassName` → real `symbolId` if unique
   - `resolveCallEdges()` — `x:FQN` → Java `fileId` if simple name is unique
5. **Commit transaction**
6. **Atomically write** `file-map.json` + `file-stat.json`

### indexEntry()

Per file in the transaction:

1. Hash file (blake3)
2. `langFromExt(relPath)` → language string
3. `UpsertFile(tx, FileRow{...})`
4. Tokenize file path → `InsertTermPostings(tx, postings)` with weight 40
5. `parse.Parser.Parse(absPath)` → `ParseResult`
6. For each symbol:
   - `InsertSymbol(tx, SymbolRow{...})`
   - `InsertEdge(tx, {type:"defines", from:fileID, to:symbolID, confidence:100})`
   - Tokenize symbol name → postings with weight 100
   - Tokenize qualified name → postings with weight 80
   - For each base class: `InsertEdge(tx, {type:"base", from:symbolID, to:x:BaseName, confidence:30})`
7. `InsertImports(tx, imports)`
   - Tokenize import strings → postings with weight 30
   - For Python: module name → postings with weight 60
8. For each call:
   - `InsertEdge(tx, {type:"calls", from:fileID, to:resolved|x:unresolved, confidence:70})`
   - Calls are deduplicated by target within a single file

---

## Database Schema

`internal/index/sqlite/ddl.sql`

### Tables

```sql
CREATE TABLE files (
    file_id  TEXT PRIMARY KEY,
    key      TEXT UNIQUE NOT NULL,
    rel_path TEXT NOT NULL,
    lang     TEXT NOT NULL DEFAULT '',
    hash     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE imports (
    file_id  TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    imp      TEXT NOT NULL
);

CREATE TABLE symbols (
    symbol_id   TEXT PRIMARY KEY,
    file_id     TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL,
    qualified   TEXT NOT NULL,
    start_line  INTEGER NOT NULL,
    end_line    INTEGER NOT NULL
);

CREATE TABLE edges (
    type       TEXT NOT NULL,
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    confidence INTEGER NOT NULL DEFAULT 100,
    aux        TEXT NOT NULL DEFAULT ''
);

CREATE TABLE term_postings (
    term   TEXT NOT NULL,
    doc_id TEXT NOT NULL,
    weight REAL NOT NULL
);
```

### Indexes

| Index | Table | Column(s) |
|---|---|---|
| `idx_files_key` | files | key |
| `idx_imports_file` | imports | file_id |
| `idx_imports_imp` | imports | imp |
| `idx_symbols_file` | symbols | file_id |
| `idx_symbols_name` | symbols | name |
| `idx_symbols_qualified` | symbols | qualified |
| `idx_edges_from` | edges | from_id |
| `idx_edges_to` | edges | to_id |
| `idx_postings_term` | term_postings | term |

### PRAGMAs

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA cache_size = -64000;   -- 64 MB
PRAGMA temp_store = MEMORY;
```

### No-FK Tables

`edges` and `term_postings` have no foreign keys. Manual cleanup required on file deletion — see `DeleteFileByKey`.

### DeleteFileByKey Algorithm

1. Collect all `symbol_id`s for the file
2. Build `allIDs` = `[fileID] + symbolIDs`
3. Delete from `edges` where `from_id IN (...) OR to_id IN (...)` — batched at 400 IDs
4. Delete from `term_postings` where `doc_id IN (...)` — batched at 800 IDs
5. Delete from `files` where `key = ?` — CASCADE handles `symbols` + `imports`

---

## ID System

`internal/index/ids.go`

| Format | Pattern | Example |
|---|---|---|
| File key | `{pathPrefix}{relPath}` | `src:pkg/collector.py` |
| File ID | `f:{key}` | `f:src:pkg/collector.py` |
| Symbol ID | `s:{lang}:{qualified}:{key}:{startLine}` | `s:py:pkg.Collector:src:pkg/collector.py:15` |
| Unresolved | `x:{name}` | `x:BaseClass` |
| Module ID (legacy) | `m:{lang}:{moduleName}` | `m:py:pkg.collector` — **table dropped**, only used in edge resolution maps |

### Python Module Naming

`PythonModuleName(relPath)` converts relative paths to dotted module names:

- Strip extension → replace `/` with `.` → strip `.__init` tail
- `pkg/__init__.py` → `pkg`
- `pkg/collector.py` → `pkg.collector`
- `pkg/sub/mod.py` → `pkg.sub.mod`

---

## Edge Types

| Type | From | To | Confidence | When Created |
|---|---|---|---|---|
| `defines` | `f:fileKey` | `s:lang:qual:key:line` | 100 | Every symbol in a file |
| `base` | `s:symbolId` | `x:ClassName` or `s:symbolId` | 30 (unresolved) | Class inheritance |
| `calls` | `f:fileKey` | `x:FQN`, `s:symbolId`, or `f:fileKey` | 70 | Function/method calls |
| `imports` | `f:fileKey` | `f:fileKey` | 100 | Python module imports (resolved file-to-file) |

### Edge Resolution (3 passes in DoSync)

**Pass 1: resolveImportEdges()**
- Builds `map[moduleName]fileID` for all Python files
- For each changed file's imports, looks up the import string
- Creates `imports` edge: source fileID → target fileID

**Pass 2: resolveBaseEdges()**
- Finds `base` edges with `to_id LIKE 'x:%'`
- Builds `map[className][]symbolId` from all indexed symbols
- If exactly one class matches → update edge's `to_id` to real symbolId
- Ambiguous or missing → left as `x:` unresolved

**Pass 3: resolveCallEdges()**
- Finds `calls` edges with `to_id LIKE 'x:%'`
- For Java: builds `map[simpleClassName][]fileId`
- If FQN's simple name maps to exactly one Java file → update `to_id` to `fileId`

### Calls Deduplication

`calls` edges are deduplicated by target within a single source file. Same function called multiple times from the same file produces one edge.

---

## Search System

### Tokenization Pipeline

`internal/tokenize/normalize.go` — `Normalizer.Tokenize(s string) []string`

```
Input string
  → SplitDelimiters (split by / . _ - : whitespace)
  → SplitCamel (camelCase → ["camel", "Case"])
  → lowercase
  → filter len < 2
  → stopword removal
  → Stem (conservative suffix stripping)
  → filter len < 2 again
  → deduplicate
```

### Stemmer Rules

`internal/tokenize/stem.go` — applies first matching rule if result > 2 chars:

| Suffix | Replacement | Example |
|---|---|---|
| `iers` | `ier` | suppliers → supplier |
| `ies` | `y` | factories → factory |
| `ing` | (remove) | running → runn |
| `ation(s)` | `ate` | migration → migrate |
| `ness` | (remove) | darkness → dark |
| `ment` | (remove) | management → manage |
| `ers` | `er` | callers → caller |
| `ed` | (remove) | indexed → index |
| `ly` | (remove) | simply → simp |
| `s` | (remove) | files → file |

### Stopwords

`internal/tokenize/stopwords.go` — 40+ built-in words: English articles, pronouns, common programming keywords (`self`, `this`, `return`, `import`, `def`, `class`, `func`, `var`, `int`, `str`, etc.). Per-service configurable via `config.json` `search.stopWords`.

### Term Postings Weights

| Source | doc_id | Weight | Constant |
|---|---|---|---|
| Symbol `name` | `symbolId` | 100.0 | `weightName` |
| Symbol `qualified` | `symbolId` | 80.0 | `weightQualified` |
| Module name (Python imports) | `fileId` | 60.0 | `weightModule` |
| File `rel_path` | `fileId` | 40.0 | `weightPath` |
| Import string | `fileId` | 30.0 | `weightImport` |

### Search Query Execution

`internal/index/sqlite/query.go` — `Search(db, terms)`

1. Query tokenized using same `Normalizer.Tokenize()` pipeline
2. For each term: `SELECT doc_id, weight FROM term_postings WHERE term = ?`
3. Sum weights per `doc_id` across all matching terms (OR semantics, additive scoring)
4. Sort by descending score
5. `App.Search` partitions results:
   - `s:` prefix → symbol results (limited by `limits.Sym`, default 20)
   - `f:` prefix → file results (limited by `limits.File`, default 10)
   - Set limit to 0 for a type to skip it entirely

### Search Response

```go
type SearchResponse struct {
    Sym  [][]interface{} // [symbolId, kind, name, fileKey, startLine, endLine]
    File [][]interface{} // [fileKey]
}
```

---

## Graph Traversal

### GetNeighbors — BFS

`internal/index/sqlite/query.go` — `GetNeighbors(db, nodeID, depth, edgeTypes)`

```
Input: nodeID, depth, edgeTypes[]
1. Initialize: visited={nodeID}, frontier=[nodeID], result=[], edgeSeen={}
2. For d = 0..depth-1:
   a. For each node in frontier:
      - Query outgoing: SELECT * FROM edges WHERE from_id = ? [+ type IN filter]
      - Query incoming: SELECT * FROM edges WHERE to_id = ? [+ type IN filter]
   b. For each edge:
      - Deduplicate by "type|from|to" key
      - Add to result
      - Determine neighbor node (other end)
      - If not visited → add to next frontier
   c. frontier = nextFrontier
3. Return []NeighborEdge{Type, From, To}
```

### GetCallers

`query.go` — `GetCallers(db, symbolID)`: finds all `calls` edges where `to_id = symbolID`, joins with `files` table, returns `[]CallerRef{FileKey, Via}`.

### GetSymbolFull

`app.go` — combines 3 data sources:
1. `GetSymbolContext` — symbol metadata + source code (read from disk by start/end line)
2. `GetCallers` — who calls this symbol
3. `GetNeighbors` — graph edges at configurable depth

---

## Parser Interface

`internal/parse/parser.go`

```go
type Parser interface {
    Parse(absPath string) (*ParseResult, error)
}

type ParseResult struct {
    Imports []string
    Symbols []SymbolDef
    Calls   []CallRef
}

type SymbolDef struct {
    Kind       string     // "class" or "function"
    Name       string
    Qualified  string     // e.g. "pkg.Collector" or "Collector.run"
    StartLine  int
    EndLine    int
    Bases      []string   // superclass names
}

type CallRef struct {
    Name   string     // function/method name
    Line   int
    Module string     // resolved module (mutually exclusive with Local)
    Local  string     // resolved local definition (mutually exclusive with Module)
}
```

### Tree-Sitter Implementation

`internal/parse/treesitter/parser.go`

```go
type Parser struct {
    lang *sitter.Language
    ext  extractor
}

type extractor interface {
    extract(root *sitter.Node, src []byte) *parse.ParseResult
}
```

`Parse(absPath)`: read file → run tree-sitter → check for ERROR nodes → delegate to `extractor.extract()`.

### Python Extractor (`treesitter/python.go`)

Two-pass approach:
- **Pass 1**: Walk top-level nodes — `import_statement`, `import_from_statement`, `class_definition`, `function_definition`, `decorated_definition`. Build `importMap[alias]=module` and `localDefs` set.
- **Pass 2**: Walk entire tree for `call` nodes. `resolveCall()` traces root of attribute chains (e.g. `os.path.join` → root `os`), resolves via `importMap` (Module), `localDefs` (Local), or marks unresolved.

Classes: extracts name, superclasses (`Bases`), nested methods. Functions: kind="function", qualified = `ParentName.method_name` if nested.

### Java Extractor (`treesitter/java.go`)

Single-pass walk of top-level nodes:
- `import_declaration`: tracks `importMap[simpleName]=fullClass`. Handles `import static`.
- `class_declaration`, `interface_declaration`, `enum_declaration`: extracts name, superclass, methods/constructors (kind="function").
- `walkCalls`: finds `method_invocation` and `object_creation_expression`. Resolves via `importMap` — if object/constructor type matches an import, the full class name is used as `CallRef.Module`.

---

## Service Management

### Registry

`internal/services/registry.go`

`Registry` — thread-safe `map[string]ServiceEntry` with `sync.RWMutex`. Persisted as JSON at `~/.mcp-indexer/registry.json`.

Methods: `LoadRegistry`, `Save` (atomic write), `Add`, `Remove`, `Get`, `List`, `ListFull`, `UpdateMeta`.

### ServiceEntry

```go
type ServiceEntry struct {
    RootAbs      string
    Description  string
    MainEntities []string
}
```

### Config

`internal/services/config.go`

```go
type Config struct {
    PathPrefix string       // prepended to relPaths as file keys, e.g. "src:"
    IncludeExt []string     // default: [".py", ".java"]
    IgnoreFile string       // default: "service.ignore"
    Search     SearchConfig // { StopWords []string }
}
```

`DefaultConfig()` — PathPrefix="", IncludeExt=[".py", ".java"], 47 built-in stop words.

### Ignore File

`internal/services/ignore.go`

`Matcher` — gitignore-style pattern matching using `doublestar`:
- Patterns without `/` → basename matching
- Patterns ending with `/` → directory prefix matching
- Supports `**` for recursive matching

### File-Map and File-Stat

`internal/services/filemap.go`

- `FileMap` = `map[string]string` — key → `"b3:<hex>"` (blake3 hashes)
- `FileStat` = `map[string][2]int64` — key → `[mtime_ns, size]`
- Both saved via `atomicWriteFile` (write `.tmp` → rename)

### APP_HOME Layout

```
$APP_HOME/                        # default: $HOME/.mcp-indexer
  registry.json                   # map[serviceId]ServiceEntry
  services/<serviceId>/
    config.json                   # PathPrefix, IncludeExt, IgnoreFile, Search.StopWords
    service.ignore                # doublestar glob patterns
    file-map.json                 # key → "b3:<hex>"
    file-stat.json                # key → [mtime_ns, size]
    index.db                      # SQLite index (WAL mode)
```

---

## Viz Server

`internal/viz/server.go` (196 lines)

HTTP server with embedded static HTML (D3.js force graph):

| Endpoint | Description |
|---|---|
| `GET /` | Embedded `static/index.html` (17KB D3.js force graph) |
| `GET /api/graph` | All edges as `{nodes, links}` for D3.js |
| `GET /api/neighbors?node=&depth=` | BFS neighbors as graph |

Auto-opens browser via `open` (macOS) / `xdg-open` (Linux).

Started via `mcp-indexer viz <serviceId> [--port 8080]`.

---

## CLI Subcommands

`cmd/main.go` — 9 subcommands via cobra:

| Subcommand | Args | Flags | Description |
|---|---|---|---|
| `serve` | — | — | Start MCP stdio server |
| `list` | — | — | List registered services |
| `add` | `<rootAbs>` | `--id`, `--description`, `--entities` | Register new service |
| `prepare-sync` | `<serviceId>` | — | Dry-run stat diff |
| `do-sync` | `<serviceId>` | — | Hash diff + apply |
| `search` | `<serviceId> <query>` | `--sym` (20), `--file` (10) | Full-text search |
| `file-context` | `<serviceId> <key>` | — | File context |
| `neighbors` | `<serviceId> <nodeId>` | `--depth` (2), `--edge-types` | BFS neighbors |
| `viz` | `<serviceId>` | `--port` (8080) | Graph visualization server |

All subcommands except `serve` use `withApp(fn)` wrapper that creates `App`, defers `Close()`.

---

## Known Limitations

- **term_postings** — manual inverted index; candidate for SQLite FTS5 with trigram tokenizer for prefix/substring search
- **imports table** partially duplicates `edges(imports)`; used for graph queries and tokenization
- **Import resolution** into graph edges (file→file) only for Python; Java imports are stored in `imports` table and tokenized, but not resolved to `imports` edges
- **Module ID** table (`modules`) dropped as legacy; only used in edge resolution maps
- **base edges** lead to `x:unresolved` — inheritance resolution not implemented
- **Go parser** registered in `langFromExt` but not implemented — will fail if `.go` files are in `IncludeExt`
- **No middleware** — no authentication, logging, or rate limiting on MCP server
- **MaxOpenConns(1)** — serialized SQLite access; limits concurrent read throughput
- **Call resolution** for Java only resolves to file level (not symbol level)
- **Single-transaction sync** — large codebases may hold transaction open for extended time

---

## Adding a New Language Parser

1. Implement `parse.Parser` interface: `Parse(absPath string) (*ParseResult, error)`
2. Create extractor in `internal/parse/treesitter/<lang>.go` implementing the `extractor` interface
3. Register extension in `langFromExt()` — `internal/syncer/dosync.go`
4. Add parser to map in `buildParsers()` — `internal/app/app.go`
5. Add extension to `DefaultConfig().IncludeExt` — `internal/services/config.go`