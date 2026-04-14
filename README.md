# mcp-indexer

Локальный индексатор исходного кода с интерфейсом MCP (Model Context Protocol). Индексирует Python/Java-код в SQLite; LLM-агенты запрашивают его через MCP-инструменты.

---

## Требования

- Go 1.22+
- Внешних зависимостей нет — SQLite встроен через `modernc.org/sqlite`

---

## Сборка

```bash
git clone <repo>
cd mcp-indexer
go build -o mcp-indexer ./cmd
```

Или без сборки, напрямую через `go run`:

```bash
go run ./cmd <команда>
```

---

## Быстрый старт: проиндексировать проект и найти что-нибудь

```bash
# 1. Зарегистрировать проект
go run ./cmd add /path/to/your/project --id myproject

# 2. Проиндексировать
go run ./cmd do-sync myproject

# 3. Поискать
go run ./cmd search myproject "название класса или функции"
```

---

## Подключить к Claude (или другому MCP-клиенту)

Сервер общается по MCP через stdio. Добавить в `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "mcp-indexer": {
      "command": "/абсолютный/путь/до/mcp-indexer",
      "args": ["serve"]
    }
  }
}
```

Или через `go run` без предварительной сборки:

```json
{
  "mcpServers": {
    "mcp-indexer": {
      "command": "go",
      "args": ["run", "/абсолютный/путь/до/mcp-indexer/cmd", "serve"]
    }
  }
}
```

Проверить вручную:

```bash
go run ./cmd serve
# ждёт MCP JSON-RPC на stdin — Ctrl-C для остановки
```

---

## CLI

### Список зарегистрированных проектов

```bash
go run ./cmd list
```

### Зарегистрировать проект

```bash
go run ./cmd add <rootAbs> [--id <serviceId>] [--name <название>]

# Примеры:
go run ./cmd add /home/user/myapp
go run ./cmd add /home/user/myapp --id myapp --name "My App"
```

`--id` по умолчанию берётся из имени директории. Должен быть уникальным.

### Предпросмотр изменений (без записи)

```bash
go run ./cmd prepare-sync <serviceId>
```

Показывает сколько файлов будет добавлено/изменено/удалено на основе stat-кэша. Содержимое файлов не читает.

### Запустить полную синхронизацию

```bash
go run ./cmd do-sync <serviceId>
```

Читает файлы, считает Blake3-хэши, сравнивает с предыдущим запуском, обновляет SQLite-индекс.

### Поиск

```bash
go run ./cmd search <serviceId> <запрос> [--sym N] [--file N] [--mod N]

# Примеры:
go run ./cmd search myapp "UserService"
go run ./cmd search myapp "parse request" --sym 5 --file 0
```

`--sym` / `--file` / `--mod` — максимум результатов по типу (по умолчанию 20/10/5). `0` — пропустить тип.

### Контекст файла

```bash
go run ./cmd file-context <serviceId> <fileKey>

# fileKey = pathPrefix + relPath, например:
go run ./cmd file-context myapp "pkg/collector.py"
go run ./cmd file-context myapp "src:pkg/collector.py"   # если pathPrefix = "src:"
```

Возвращает модуль, импорты и список символов файла.

### Соседи в графе зависимостей (BFS)

```bash
go run ./cmd neighbors <serviceId> <nodeId> [--depth N] [--edge-types <типы>]

# Примеры:
go run ./cmd neighbors myapp "m:py:pkg.collector" --depth 2
go run ./cmd neighbors myapp "f:pkg/collector.py" --depth 1 --edge-types defines,imports
```

`nodeId` — fileId (`f:...`), moduleId (`m:py:...`) или symbolId (`s:py:...`).

---

## Конфигурация

Каждый сервис хранит `config.json` в `$APP_HOME/services/<serviceId>/`:

```json
{
  "pathPrefix": "src:",
  "includeExt": [".py", ".java"],
  "ignoreFile": "service.ignore"
}
```

- **`pathPrefix`** — префикс, добавляемый ко всем путям при построении ключей (по умолчанию `""`)
- **`includeExt`** — расширения файлов для индексации (по умолчанию `[".py", ".java"]`)
- **`ignoreFile`** — файл с gitignore-паттернами исключений (по умолчанию `service.ignore`)

`service.ignore` использует doublestar glob против `rel_path`:

```
__pycache__/
**/__pycache__/**
*.pyc
.venv/**
venv/**
target/**
build/**
dist/**
.git/**
node_modules/**
```

---

## Где хранятся данные

По умолчанию: `$HOME/.mcp-indexer`  
Переопределить: `MCP_INDEXER_HOME=/path/to/dir`

```
$APP_HOME/
  registry.json                  # зарегистрированные сервисы
  services/<serviceId>/
    config.json
    service.ignore
    file-map.json                # хэш-истина: key → "b3:<hex>"
    file-stat.json               # stat-кэш для быстрого предварительного диффа
    index.db                     # SQLite-индекс
```

---

## MCP-инструменты (для LLM-агентов)

| Инструмент | Обязательные параметры | Описание |
|---|---|---|
| `getInfo` | — | Путь к конфигу и версия |
| `getServiceList` | — | Список зарегистрированных сервисов |
| `addService` | `rootAbs` | Зарегистрировать новый сервис |
| `getServiceInfo` | `serviceId` | Детали и конфиг сервиса |
| `prepareSync` | `serviceId` | Предпросмотр изменений (без записи) |
| `doSync` | `serviceId` | Хэш-дифф + применить к индексу |
| `getProjectOverview` | `serviceId` | Счётчики файлов/модулей/символов/рёбер |
| `search` | `serviceId`, `query` | Поиск с опциональным JSON `limits` |
| `getFileContext` | `serviceId`, `path` | Модуль, импорты и символы файла |
| `getSymbolContext` | `serviceId`, `symbolId` | Детали символа |
| `getSymbolFull` | `serviceId`, `symbolId` | Символ + код + вызывающие + рёбра |
| `getNeighbors` | `serviceId`, `nodeId` | BFS в графе зависимостей |

---

## Форматы ключей

```
key        = pathPrefix + rel_path          "src:pkg/collector.py"
fileId     = "f:" + key                     "f:src:pkg/collector.py"
moduleId   = "m:py:" + moduleName           "m:py:pkg.collector"
symbolId   = "s:py:{qualified}:{key}:{line}"
unresolved = "x:" + name
```

---

## Тесты

```bash
go test ./...
```
