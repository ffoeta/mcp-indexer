# mcp-indexer — project knowledge

## Overview

MCP-сервер для индексирования исходного кода. SQLite backend. Используется как инструмент для LLM-агентов (Claude и др.) через Model Context Protocol.

## APP_HOME layout

```
$APP_HOME/                        # default: $HOME/.mcp-indexer
  registry.json                   # список сервисов: id → {rootAbs, ...}
  services/<serviceId>/
    config.json                   # extensions, pathPrefix, maxFileSizeKB
    service.ignore                # gitignore-style паттерны
    file-map.json                 # hash truth: key → "b3:<hex>"
    file-stat.json                # stat cache: key → [mtime_ns, size]
    index.db                      # SQLite индекс
```

## Architecture layers

```
cmd                    → entrypoint: serve (MCP stdio) + CLI subcommands
internal/services      → registry, config, file-map, file-stat I/O
internal/syncer        → scan.go, diff.go, dosync.go (orchestration)
internal/parse         → Parser interface + tree-sitter wrappers (python, java)
internal/tokenize      → Normalizer: split delimiters, camelCase, stem, stop-words
internal/index         → model.go (row structs), ids.go (ID constructors)
internal/index/sqlite  → ddl.sql, store.go, upsert.go, delete.go, query.go
```

## Database schema

SQLite, WAL mode, foreign_keys ON.

| Table | Key columns | FK behaviour |
|---|---|---|
| `modules` | module_id PK, module_name | — |
| `files` | file_id PK, key UNIQUE, rel_path, lang, hash, module_id | → modules SET NULL |
| `imports` | file_id, imp | → files CASCADE DELETE |
| `symbols` | symbol_id PK, file_id, kind, name, qualified, start_line, end_line | → files CASCADE DELETE |
| `edges` | type, from_id, to_id, confidence, aux | **NO FK** — ручная очистка |
| `term_postings` | term, doc_id, weight | **NO FK** — ручная очистка |

`edges` и `term_postings` не имеют FK. При удалении файла нужна явная очистка — см. `delete.go:DeleteFileByKey`.

## ID formats

```
key        = pathPrefix + rel_path          e.g. "src:pkg/collector.py"
fileId     = "f:" + key                     e.g. "f:src:pkg/collector.py"
moduleId   = "m:{lang}:{moduleName}"        e.g. "m:py:pkg.collector"
symbolId   = "s:{lang}:{qualified}:{key}:{startLine}"
unresolved = "x:{name}"
```

Python module naming: `pkg/__init__.py` → `pkg`, `pkg/collector.py` → `pkg.collector`.

## Edge types

| Type | From | To | Confidence |
|---|---|---|---|
| `contains` | moduleId | fileId | 100 |
| `imports` | fileId | moduleId | 100 |
| `defines` | fileId | symbolId | 100 |
| `base` | symbolId | x:BaseName | 30 |
| `calls` | fileId | moduleId / symbolId / x:name | 70 |

`calls` edges дедуплицированы по target в пределах одного файла.

## What is extracted from files

- **imports** `[]string` — имена импортируемых модулей
- **symbols** — kind (`class`/`function`), name, qualified, start_line, end_line, bases `[]string`
- **calls** — вызовы функций/методов; резолвятся в moduleId, symbolId или x:unresolved

Языки: Python (tree-sitter), Java (tree-sitter). Go — extension зарегистрирован, парсер не реализован.

## Search weights (term_postings)

| Source | doc_id | Weight |
|---|---|---|
| symbol name | symbolId | 100 |
| qualified name | symbolId | 80 |
| module name | moduleId | 60 |
| file path | fileId | 40 |
| import string | fileId | 30 |

## Sync flow

1. `Scan` filesystem → `FileEntry[]`
2. `DiffHash` vs saved `file-map.json` → Added / Modified / Deleted
3. Per file in TX: hash → lang → parse → upsert module / file / imports / symbols / edges / postings
4. `tx.Commit()`
5. Atomically write `file-map.json` + `file-stat.json`

Modified = delete old + re-index (full replace, не patch).

## Known limitations

- `term_postings` — ручной инвертированный индекс; кандидат на замену SQLite FTS5 (trigram tokenizer для prefix/substring search)
- `imports` таблица частично дублирует `edges(imports)`; используется для graph query и токенизации
- Module resolution только для Python; Java/Go модули в `modules` не создаются
- `base` edges ведут на `x:unresolved` — разрешение наследования не реализовано

## Adding a new language parser

1. Реализовать `parse.Parser` interface: `Parse(absPath string) (*ParseResult, error)`
2. Зарегистрировать расширение в `langFromExt()` — `internal/syncer/dosync.go`
3. Добавить parser в map при инициализации в `cmd/main.go`
