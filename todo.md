## Deferred

- **viz / UI graph visualization** — старая реализация D3.js была завязана на legacy
  `NeighborEdge` формат. Перевод под новые рёбра (3 типа: calls/inherits/imports
  + псевдо-defines через `owner_id`) откладывается; `internal/ui/server.go` сейчас
  возвращает `ErrNotImplemented`, `cmd viz` в CLI отдаёт ту же ошибку.

- **TypeScript / JavaScript extractor** — единый парсер на `tree-sitter-typescript`
  (`.ts/.tsx/.js/.jsx`). Классы, interfaces, enums, top-level functions →
  method без owner, methods классов с owner, `new Foo()`, `obj.method()`,
  var-types из type annotations + `as`, JSDoc → Doc.

- **Go extractor** — funcs (свободные → method без owner), methods на receiver,
  types (struct/interface). Calls: `pkg.Func` через importMap, `var.Method`
  через var-types. Учесть особенности embedded fields и pointer receivers.

---

## Оценка MCP indexer (исходная)

Что работало хорошо:
- Поиск по ключевому слову сразу дал весь срез: классы, методы, тесты —
  без лишнего шума
- serviceId чётко разделяет search-impl-daemon и search-api-hh-supplier —
  не смешивает контексты из разных репозиториев
- get_symbol_full с includeCode: true вернул полный исходник класса
  за один запрос — не надо было читать файл отдельно
- edgeDepth в edges показал структуру defines-связей (методы класса) —
  полезно для навигации

Проблемы:
- callers: [] — граф вызовов не заполнен ни для одного класса. Это
  критичный gap: чтобы понять, кто вызывает update() / loadFromDisk()
  (планировщик?), пришлось бы идти grep'ом
- Нет edges типа calls или implements — только defines. Нельзя понять
  зависимости между классами через граф, только через чтение исходника
  вручную
- get_neighbors не дал бы ничего полезного без calls-рёбер —
  вырожденный граф
- Поиск не возвращает SupplierFeatureProvider, SupplierUpdateService,
  KardinalJobsClient — внешние зависимости невидимы, нужно
  переключаться на search-api-hh-supplier serviceId

Вывод: инструмент хорош как быстрый grep-on-steroids с исходником.
Для понимания потока вызовов и межсервисных зависимостей —
неполноценен из-за отсутствия callers и calls-рёбер. Это
компенсируется только чтением файлов напрямую или переключением
контекста на соседний сервис.
