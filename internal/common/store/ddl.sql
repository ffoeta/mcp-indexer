PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA cache_size = -65536;  -- 64 MB page cache
PRAGMA temp_store = MEMORY;

-- ───────── core tables ─────────

CREATE TABLE IF NOT EXISTS files (
    file_id  TEXT PRIMARY KEY,
    short_id INTEGER NOT NULL UNIQUE,
    key      TEXT NOT NULL UNIQUE,
    rel_path TEXT NOT NULL,
    lang     TEXT NOT NULL,
    package  TEXT NOT NULL DEFAULT '',
    hash     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_files_key  ON files(key);
CREATE INDEX IF NOT EXISTS idx_files_lang ON files(lang);

CREATE TABLE IF NOT EXISTS nodes (
    node_id    TEXT PRIMARY KEY,
    short_id   INTEGER NOT NULL UNIQUE,
    file_id    TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,        -- 'object' | 'method'
    subkind    TEXT NOT NULL,        -- class|interface|enum|struct|trait | fn|method|ctor|module
    name       TEXT NOT NULL,
    fqn        TEXT NOT NULL,
    owner_id   TEXT REFERENCES nodes(node_id) ON DELETE CASCADE,
    scope      TEXT NOT NULL DEFAULT 'global', -- global|local|member
    signature  TEXT NOT NULL DEFAULT '',
    doc        TEXT NOT NULL DEFAULT '',
    start_line INTEGER NOT NULL,
    end_line   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes(file_id);
CREATE INDEX IF NOT EXISTS idx_nodes_owner ON nodes(owner_id);
CREATE INDEX IF NOT EXISTS idx_nodes_name  ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_nodes_fqn   ON nodes(fqn);
CREATE INDEX IF NOT EXISTS idx_nodes_kind  ON nodes(kind);

-- ───────── edges (typed, separate tables) ─────────

CREATE TABLE IF NOT EXISTS edges_calls (
    caller_id    TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    callee_id    TEXT REFERENCES nodes(node_id) ON DELETE SET NULL,
    callee_name  TEXT NOT NULL,
    callee_owner TEXT NOT NULL DEFAULT '',
    line         INTEGER NOT NULL,
    confidence   INTEGER NOT NULL  -- 100|70|40|0
);
CREATE INDEX IF NOT EXISTS idx_calls_caller ON edges_calls(caller_id);
CREATE INDEX IF NOT EXISTS idx_calls_callee ON edges_calls(callee_id);
CREATE INDEX IF NOT EXISTS idx_calls_name   ON edges_calls(callee_name);

CREATE TABLE IF NOT EXISTS edges_inherits (
    child_id    TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    parent_id   TEXT REFERENCES nodes(node_id) ON DELETE SET NULL,
    parent_hint TEXT NOT NULL,
    relation    TEXT NOT NULL          -- 'extends' | 'implements'
);
CREATE INDEX IF NOT EXISTS idx_inh_child  ON edges_inherits(child_id);
CREATE INDEX IF NOT EXISTS idx_inh_parent ON edges_inherits(parent_id);

CREATE TABLE IF NOT EXISTS edges_imports (
    file_id        TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    target_file_id TEXT REFERENCES files(file_id) ON DELETE SET NULL,
    raw            TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_imp_src ON edges_imports(file_id);
CREATE INDEX IF NOT EXISTS idx_imp_tgt ON edges_imports(target_file_id);

-- ───────── full-text search ─────────

CREATE VIRTUAL TABLE IF NOT EXISTS search_idx USING fts5(
    doc_id   UNINDEXED,
    doc_kind UNINDEXED,         -- 'node' | 'file'
    name,
    fqn,
    path,
    tokenize='unicode61 remove_diacritics 2',
    prefix='2 3 4'
);