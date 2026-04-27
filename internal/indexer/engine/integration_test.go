package engine

import (
	"database/sql"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/tokenize"
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/parse/treesitter"
	"os"
	"path/filepath"
	"testing"
)

// ───────── helpers ─────────

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runIndex(t *testing.T, root string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfgVal := services.DefaultConfig()
	cfg := &cfgVal
	matcher := &services.Matcher{}
	parsers := map[string]parse.Parser{
		".py":   treesitter.NewPython(),
		".java": treesitter.NewJava(),
	}
	norm := tokenize.New(tokenize.DefaultStopSet())

	if err := Index(s.DB(), root, cfg, matcher, parsers, norm, t.TempDir()); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return s.DB()
}

func countRows(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

// ───────── tests ─────────

func TestIndex_PythonCrossFileCalls(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "pkg/__init__.py", "")
	writeProjectFile(t, root, "pkg/repo.py", `
class OrderRepo:
    def save(self, order):
        pass
`)
	writeProjectFile(t, root, "pkg/svc.py", `
from pkg.repo import OrderRepo

class OrderService:
    def run(self):
        repo: OrderRepo = OrderRepo()
        repo.save(None)
`)
	db := runIndex(t, root)

	if n := countRows(t, db, `SELECT COUNT(*) FROM files`); n != 3 {
		t.Errorf("files=%d, want 3", n)
	}
	// объекты: OrderRepo, OrderService
	if n := countRows(t, db, `SELECT COUNT(*) FROM nodes WHERE kind=?`, store.KindObject); n != 2 {
		t.Errorf("objects=%d, want 2", n)
	}
	// methods: <module>×3 + save + run = 5
	if n := countRows(t, db, `SELECT COUNT(*) FROM nodes WHERE kind=?`, store.KindMethod); n < 5 {
		t.Errorf("methods=%d, want ≥5", n)
	}

	// cross-file call: OrderService.run → OrderRepo.save (resolved)
	var conf int
	err := db.QueryRow(`
		SELECT confidence FROM edges_calls
		WHERE callee_name='save' AND callee_id IS NOT NULL
		LIMIT 1
	`).Scan(&conf)
	if err != nil {
		t.Fatalf("expected resolved save() call: %v", err)
	}
	if conf < store.ConfImport {
		t.Errorf("save() resolved with conf=%d, want ≥%d", conf, store.ConfImport)
	}

	// imports edge resolved
	var tgt sql.NullString
	db.QueryRow(`SELECT target_file_id FROM edges_imports WHERE raw='pkg.repo'`).Scan(&tgt)
	if !tgt.Valid {
		t.Errorf("import 'pkg.repo' should resolve to fileID")
	}
}

func TestIndex_PythonModuleLevelCallsAttributedToSyntheticModule(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "main.py", `
import logging

def helper():
    pass

logging.info("starting")
helper()
`)
	db := runIndex(t, root)

	// Должен быть synthetic <module>-method
	var moduleNodeID string
	err := db.QueryRow(`
		SELECT node_id FROM nodes WHERE name='<module>' AND kind='method'
	`).Scan(&moduleNodeID)
	if err != nil {
		t.Fatalf("synthetic <module> not found: %v", err)
	}

	// helper() resolved (pass 1 same-file)
	var conf int
	err = db.QueryRow(`
		SELECT confidence FROM edges_calls
		WHERE caller_id=? AND callee_name='helper' AND callee_id IS NOT NULL
	`, moduleNodeID).Scan(&conf)
	if err != nil {
		t.Errorf("helper() not resolved at module-level: %v", err)
	} else if conf != store.ConfSameFile {
		t.Errorf("helper conf=%d, want %d", conf, store.ConfSameFile)
	}

	// logging.info() — unresolved (external module)
	var unresolvedConf int
	db.QueryRow(`
		SELECT confidence FROM edges_calls
		WHERE caller_id=? AND callee_name='info' AND callee_id IS NULL
	`, moduleNodeID).Scan(&unresolvedConf)
	if unresolvedConf != store.ConfNone {
		t.Errorf("logging.info should be unresolved with conf=0, got %d", unresolvedConf)
	}
}

func TestIndex_PythonInheritsResolved(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "base.py", `
class Animal:
    pass
`)
	writeProjectFile(t, root, "cat.py", `
from base import Animal

class Cat(Animal):
    pass
`)
	db := runIndex(t, root)

	var parentID sql.NullString
	err := db.QueryRow(`
		SELECT parent_id FROM edges_inherits WHERE parent_hint='Animal'
	`).Scan(&parentID)
	if err != nil {
		t.Fatalf("inherits row missing: %v", err)
	}
	if !parentID.Valid {
		t.Errorf("Animal parent_id should be resolved")
	}
}

func TestIndex_JavaCrossFileCalls(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "com/x/OrderRepo.java", `
package com.x;
public class OrderRepo {
    public void save(Object o) {}
}
`)
	writeProjectFile(t, root, "com/x/OrderService.java", `
package com.x;
public class OrderService {
    public void run() {
        OrderRepo r = new OrderRepo();
        r.save(null);
    }
}
`)
	db := runIndex(t, root)

	// both classes
	if n := countRows(t, db, `SELECT COUNT(*) FROM nodes WHERE kind=?`, store.KindObject); n != 2 {
		t.Errorf("objects=%d, want 2", n)
	}

	// run() → save() resolved
	var calleeFQN string
	err := db.QueryRow(`
		SELECT n.fqn FROM edges_calls e
		JOIN nodes n ON n.node_id = e.callee_id
		WHERE e.callee_name='save'
	`).Scan(&calleeFQN)
	if err != nil {
		t.Fatalf("save() not resolved: %v", err)
	}
	if calleeFQN != "com.x.OrderRepo.save" {
		t.Errorf("callee FQN=%q, want com.x.OrderRepo.save", calleeFQN)
	}

	// new OrderRepo() — резолвится в ctor (subkind=ctor) или в class?
	// Сейчас ctor не объявлен явно — Java extractor не создаёт implicit ctor,
	// поэтому ожидаем unresolved или Pass 3 fallback. Проверяем что ребро есть.
	var ctorRowCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM edges_calls WHERE callee_name='OrderRepo'
	`).Scan(&ctorRowCount)
	if ctorRowCount == 0 {
		t.Errorf("new OrderRepo() call edge missing")
	}
}

func TestIndex_FTS5_FindsByName(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "svc.py", `
class OrderService:
    def process_order(self):
        pass
`)
	db := runIndex(t, root)

	rows, err := db.Query(`
		SELECT doc_id FROM search_idx
		WHERE search_idx MATCH ?
		ORDER BY bm25(search_idx)
	`, "order")
	if err != nil {
		t.Fatalf("fts query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if count == 0 {
		t.Errorf("fts MATCH 'order' returned no hits")
	}
}

func TestIndex_FullReindex_ClearsOldData(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "a.py", "class Foo: pass\n")
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, _ := store.Open(dbPath)
	defer s.Close()

	cfgVal := services.DefaultConfig()
	cfg := &cfgVal
	matcher := &services.Matcher{}
	parsers := map[string]parse.Parser{".py": treesitter.NewPython()}
	norm := tokenize.New(tokenize.DefaultStopSet())

	if err := Index(s.DB(), root, cfg, matcher, parsers, norm, ""); err != nil {
		t.Fatal(err)
	}
	// Удаляем класс, добавляем другой
	os.WriteFile(filepath.Join(root, "a.py"), []byte("class Bar: pass\n"), 0o644)
	if err := Index(s.DB(), root, cfg, matcher, parsers, norm, ""); err != nil {
		t.Fatal(err)
	}
	var fooCount, barCount int
	s.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE name='Foo'`).Scan(&fooCount)
	s.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE name='Bar'`).Scan(&barCount)
	if fooCount != 0 {
		t.Errorf("Foo should be wiped, got %d", fooCount)
	}
	if barCount != 1 {
		t.Errorf("Bar should be present, got %d", barCount)
	}
}