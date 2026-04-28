## Deferred

- **TypeScript / JavaScript extractor** — единый парсер на `tree-sitter-typescript`
  (`.ts/.tsx/.js/.jsx`). Классы, interfaces, enums, top-level functions →
  method без owner, methods классов с owner, `new Foo()`, `obj.method()`,
  var-types из type annotations + `as`, JSDoc → Doc.

- **Go extractor** — funcs (свободные → method без owner), methods на receiver,
  types (struct/interface). Calls: `pkg.Func` через importMap, `var.Method`
  через var-types. Учесть особенности embedded fields и pointer receivers.

