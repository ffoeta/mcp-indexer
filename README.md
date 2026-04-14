# mcp-indexer

Local multi-service code indexer exposing an MCP (Model Context Protocol) interface.

## APP_HOME

Default: `$HOME/.mcp-indexer`  
Override: `MCP_INDEXER_HOME=/path/to/dir`

Layout:
```
$APP_HOME/
  registry.json
  services/<serviceId>/
    config.json
    service.ignore
    file-map.json   # hash truth: key -> "b3:<hex>"
    file-stat.json  # stat cache: key -> [mtime_ns, size]
    index.db        # SQLite index
```

## Run

```bash
# MCP stdio server (for Claude / LLM agents)
go run ./cmd serve

# CLI commands
go run ./cmd list
go run ./cmd add /path/to/project --id myservice
go run ./cmd prepare-sync myservice
go run ./cmd do-sync myservice
go run ./cmd search myservice "collector factory"
go run ./cmd file-context myservice "src:pkg/collector.py"
go run ./cmd neighbors myservice "m:py:pkg.collector" --depth 2
```

## MCP Tools

| Tool | Description |
|---|---|
| `getServiceList` | List registered service IDs |
| `addService` | Register a new service |
| `getServiceInfo` | Service details + config |
| `prepareSync` | Stat-diff preview (no writes) |
| `doSync` | Hash diff + apply to index |
| `getProjectOverview` | File/module/symbol/edge counts |
| `search` | Search by query with limits per type |
| `getFileContext` | Module, imports, symbols for a file |
| `getSymbolContext` | Symbol details by symbolId |
| `getNeighbors` | BFS in dependency graph |

## Configuration

`config.json` per service:

```json
{
  "pathPrefix": "src:",
  "includeExt": [".py", ".java"],
  "ignoreFile": "service.ignore",
  "search": {
    "stopWords": [
      "a", "an", "the", "and", "or", "not", "in", "is", "it", "of", "to",
      "as", "at", "be", "by", "do", "for", "if", "on", "up", "we",
      "self", "this", "super", "true", "false", "null", "nil", "none",
      "new", "return", "import", "from", "def", "class", "func", "var",
      "let", "const", "type", "struct", "interface", "public", "private",
      "protected", "static", "final", "void", "int", "str", "bool",
      "list", "map", "set", "get", "err", "error"
    ]
  }
}
```

`service.ignore` — doublestar glob patterns against `rel_path`:

```
__pycache__/
**/__pycache__/**
*.pyc
*.pyo
.venv/**
venv/**
.env/**
target/**
build/**
dist/**
*.egg-info/**
.git/**
node_modules/**
```

## Key Formats

- `key` = `pathPrefix + rel_path` (e.g. `src:pkg/collector.py`)
- `fileId` = `f:` + key
- `moduleId` = `m:py:` + moduleName (e.g. `m:py:pkg.collector`)
- `symbolId` = `s:python:ClassName.method:src:pkg/collector.py:42`
- unresolved = `x:` + name

