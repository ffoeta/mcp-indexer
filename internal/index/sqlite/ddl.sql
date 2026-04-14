PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS modules (
    module_id   TEXT PRIMARY KEY,
    module_name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    file_id   TEXT PRIMARY KEY,
    key       TEXT NOT NULL UNIQUE,
    rel_path  TEXT NOT NULL,
    lang      TEXT NOT NULL,
    hash      TEXT NOT NULL,
    module_id TEXT REFERENCES modules(module_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_files_key ON files(key);

CREATE TABLE IF NOT EXISTS imports (
    file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    imp     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_id);
CREATE INDEX IF NOT EXISTS idx_imports_imp  ON imports(imp);

CREATE TABLE IF NOT EXISTS symbols (
    symbol_id  TEXT PRIMARY KEY,
    file_id    TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    qualified  TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_symbols_file      ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_symbols_name      ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_qualified ON symbols(qualified);

CREATE TABLE IF NOT EXISTS edges (
    type       TEXT NOT NULL,
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    confidence INTEGER NOT NULL DEFAULT 100,
    aux        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_id);

CREATE TABLE IF NOT EXISTS term_postings (
    term   TEXT NOT NULL,
    doc_id TEXT NOT NULL,
    weight REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_postings_term ON term_postings(term);
