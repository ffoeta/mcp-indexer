# mcp-indexer — project knowledge

## Overview

MCP-сервер для индексирования исходного кода. SQLite backend. Используется как инструмент для LLM-агентов (Claude и др.) через Model Context Protocol.

Подробная архитектура: [arch.md](.claude/arch.md)

## Stack

Go 1.23 · modernc.org/sqlite (pure-Go) · tree-sitter (Python, Java) · mcp-go · blake3 · doublestar · cobra

## Architecture layers

```
cmd                    → entrypoint: serve (MCP stdio) + CLI subcommands
internal/mcp           → MCP tool registration (14 tools)
internal/app           → App: central orchestrator, business logic
internal/services      → registry, config, file-map, file-stat, ignore
internal/syncer        → scan → stat-diff → hash-diff → parse → upsert → resolve edges
internal/parse         → Parser interface + tree-sitter (python, java)
internal/tokenize      → Normalizer: split → camelCase → lowercase → stop → stem → dedup
internal/index         → model.go (row structs), ids.go (ID constructors)
internal/index/sqlite  → ddl.sql, store.go, upsert.go, delete.go, query.go
internal/viz           → HTTP server with D3.js force graph
```

## APP_HOME layout

```
$APP_HOME/                        # default: $HOME/.mcp-indexer
  registry.json                   # serviceId → {rootAbs, description, mainEntities}
  services/<serviceId>/
    config.json                   # pathPrefix, includeExt, ignoreFile, search.stopWords
    service.ignore                # gitignore-style паттерны
    file-map.json                 # key → "b3:<hex>"
    file-stat.json                # key → [mtime_ns, size]
    index.db                      # SQLite индекс
```

## ID formats

```
key        = pathPrefix + rel_path          e.g. "src:pkg/collector.py"
fileId     = "f:" + key                     e.g. "f:src:pkg/collector.py"
symbolId   = "s:{lang}:{qualified}:{key}:{startLine}"
unresolved = "x:{name}"
```

Python module naming: `pkg/__init__.py` → `pkg`, `pkg/collector.py` → `pkg.collector`.

## Sync flow

1. `Scan` filesystem → `FileEntry[]`
2. Phase 1: `DiffStat` (mtime+size, no reads) → Added / MaybeModified / Deleted
3. Phase 2: `DiffHashCandidates` (blake3, candidates only) → Added / Modified / Deleted
4. Single TX: delete removed → index added → re-index modified → resolve edges
5. Commit + atomically write `file-map.json` + `file-stat.json`

Modified = delete old + re-index (full replace, не patch).

## Edge types

| Type | From → To | Confidence | Resolution |
|---|---|---|---|
| `defines` | fileId → symbolId | 100 | — |
| `base` | symbolId → x:ClassName | 30 | resolveBaseEdges (unique name → symbolId) |
| `calls` | fileId → x:FQN / symbolId / fileId | 70 | resolveCallEdges (Java: unique simple name → fileId) |
| `imports` | fileId → fileId | 100 | resolveImportEdges (Python: module name → fileId) |

## Search weights

symbol name (100) > qualified (80) > module (60) > file path (40) > import (30)

## Known limitations

- `term_postings` — ручной инвертированный индекс; кандидат на FTS5 + trigram
- `imports` таблица частично дублирует `edges(imports)`
- Import resolution в graph edges (file→file) только для Python; Java импорты записаны в таблицу, но не резолвятся
- `base` edges → `x:unresolved` — наследование не резолвится
- Go parser не реализован (extension зарегистрирован)
- No middleware — no auth, logging, rate limiting

## Adding a new language parser

1. Implement `parse.Parser` interface: `Parse(absPath string) (*ParseResult, error)`
2. Create extractor in `internal/parse/treesitter/<lang>.go`
3. Register extension in `langFromExt()` — `internal/syncer/dosync.go`
4. Add parser in `buildParsers()` — `internal/app/app.go`
5. Add extension to `DefaultConfig().IncludeExt` — `internal/services/config.go`
