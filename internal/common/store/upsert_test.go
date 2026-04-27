package store

import (
	"testing"
)

func TestInsertFile_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	row := FileRow{
		FileID:  "f:src:a.py",
		ShortID: 7,
		Key:     "src:a.py",
		RelPath: "a.py",
		Lang:    "python",
		Package: "pkg",
		Hash:    "b3:cafe",
	}
	if err := InsertFile(tx, row); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var got FileRow
	err := s.DB().QueryRow(
		`SELECT file_id, short_id, key, rel_path, lang, package, hash FROM files WHERE key=?`,
		"src:a.py",
	).Scan(&got.FileID, &got.ShortID, &got.Key, &got.RelPath, &got.Lang, &got.Package, &got.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if got != row {
		t.Errorf("round-trip mismatch:\n want %+v\n  got %+v", row, got)
	}
}

func TestInsertNode_OwnerNullVsSet(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()

	fid := mkFile(t, tx, 1, "src:a.py", "python")
	objID := mkNode(t, tx, 10, fid, KindObject, SubClass, "pkg.A", 5)
	mkNode(t, tx, 11, fid, KindMethod, SubMethod, "pkg.A.foo", 6)

	// owner для метода устанавливаем явно через update, имитируя правильный flow
	if _, err := tx.Exec(`UPDATE nodes SET owner_id=? WHERE fqn=?`, objID, "pkg.A.foo"); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var owner *string
	s.DB().QueryRow(`SELECT owner_id FROM nodes WHERE fqn='pkg.A'`).Scan(&owner)
	if owner != nil {
		t.Errorf("object owner should be NULL, got %v", *owner)
	}

	s.DB().QueryRow(`SELECT owner_id FROM nodes WHERE fqn='pkg.A.foo'`).Scan(&owner)
	if owner == nil || *owner != objID {
		t.Errorf("method owner mismatch: %v vs %s", owner, objID)
	}
}

func TestInsertCallEdge_ResolvedAndUnresolved(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()

	fid := mkFile(t, tx, 1, "src:a.py", "python")
	caller := mkNode(t, tx, 10, fid, KindMethod, SubFn, "pkg.foo", 1)
	callee := mkNode(t, tx, 11, fid, KindMethod, SubFn, "pkg.bar", 5)

	// resolved
	if err := InsertCallEdge(tx, CallEdge{
		CallerID: caller, CalleeID: callee,
		CalleeName: "bar", Line: 2, Confidence: ConfSameFile,
	}); err != nil {
		t.Fatal(err)
	}
	// unresolved
	if err := InsertCallEdge(tx, CallEdge{
		CallerID: caller, CalleeID: "",
		CalleeName: "mystery", CalleeOwner: "ext.Mod", Line: 3, Confidence: ConfNone,
	}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	rows, _ := s.DB().Query(`SELECT callee_id, callee_name, confidence FROM edges_calls ORDER BY line`)
	defer rows.Close()
	var resolvedSeen, unresolvedSeen bool
	for rows.Next() {
		var ce *string
		var name string
		var conf int
		rows.Scan(&ce, &name, &conf)
		if ce != nil && *ce == callee && name == "bar" && conf == ConfSameFile {
			resolvedSeen = true
		}
		if ce == nil && name == "mystery" && conf == ConfNone {
			unresolvedSeen = true
		}
	}
	if !resolvedSeen {
		t.Error("resolved call not found")
	}
	if !unresolvedSeen {
		t.Error("unresolved call not found")
	}
}

func TestInsertInheritEdge_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fid := mkFile(t, tx, 1, "src:a.py", "python")
	child := mkNode(t, tx, 10, fid, KindObject, SubClass, "pkg.Child", 1)
	parent := mkNode(t, tx, 11, fid, KindObject, SubClass, "pkg.Parent", 5)

	if err := InsertInheritEdge(tx, InheritEdge{
		ChildID: child, ParentID: parent,
		ParentHint: "Parent", Relation: RelExtends,
	}); err != nil {
		t.Fatal(err)
	}
	if err := InsertInheritEdge(tx, InheritEdge{
		ChildID: child, ParentID: "",
		ParentHint: "Unknown", Relation: RelImplements,
	}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE child_id=?`, child).Scan(&n)
	if n != 2 {
		t.Errorf("expected 2 inherit edges, got %d", n)
	}
	var nullParents int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges_inherits WHERE parent_id IS NULL`).Scan(&nullParents)
	if nullParents != 1 {
		t.Errorf("expected 1 NULL parent, got %d", nullParents)
	}
}

func TestInsertImportEdge_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	src := mkFile(t, tx, 1, "src:a.py", "python")
	tgt := mkFile(t, tx, 2, "src:b.py", "python")

	if err := InsertImportEdge(tx, ImportEdge{
		FileID: src, TargetFileID: tgt, Raw: "pkg.b",
	}); err != nil {
		t.Fatal(err)
	}
	if err := InsertImportEdge(tx, ImportEdge{
		FileID: src, TargetFileID: "", Raw: "os",
	}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var external int
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges_imports WHERE target_file_id IS NULL`).Scan(&external)
	if external != 1 {
		t.Errorf("expected 1 external import, got %d", external)
	}
}

func TestCascade_FileDelete_RemovesNodesAndEdges(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fid := mkFile(t, tx, 1, "src:a.py", "python")
	caller := mkNode(t, tx, 10, fid, KindMethod, SubFn, "pkg.foo", 1)
	callee := mkNode(t, tx, 11, fid, KindMethod, SubFn, "pkg.bar", 5)
	InsertCallEdge(tx, CallEdge{CallerID: caller, CalleeID: callee, CalleeName: "bar", Line: 2, Confidence: ConfSameFile})
	InsertInheritEdge(tx, InheritEdge{ChildID: caller, ParentID: callee, ParentHint: "bar", Relation: RelExtends})
	tx.Commit()

	// Удалим файл — должно каскадно вычистить nodes и edges_*
	if _, err := s.DB().Exec(`DELETE FROM files WHERE file_id=?`, fid); err != nil {
		t.Fatal(err)
	}

	var n int
	s.DB().QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&n)
	if n != 0 {
		t.Errorf("nodes not cascaded: %d", n)
	}
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges_calls`).Scan(&n)
	if n != 0 {
		t.Errorf("edges_calls not cascaded: %d", n)
	}
	s.DB().QueryRow(`SELECT COUNT(*) FROM edges_inherits`).Scan(&n)
	if n != 0 {
		t.Errorf("edges_inherits not cascaded: %d", n)
	}
}

func TestWipeAll_ClearsEverythingIncludingFTS(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	fid := mkFile(t, tx, 1, "src:a.py", "python")
	mkNode(t, tx, 10, fid, KindMethod, SubFn, "pkg.foo", 1)
	InsertSearchDoc(tx, SearchDoc{
		DocID: "n1", DocKind: DocNode, Name: "foo", FQN: "pkg foo", Path: "src a py",
	})
	tx.Commit()

	if err := WipeAll(s.DB()); err != nil {
		t.Fatal(err)
	}

	var n int
	for _, q := range []string{
		`SELECT COUNT(*) FROM files`,
		`SELECT COUNT(*) FROM nodes`,
		`SELECT COUNT(*) FROM edges_calls`,
		`SELECT COUNT(*) FROM edges_inherits`,
		`SELECT COUNT(*) FROM edges_imports`,
		`SELECT COUNT(*) FROM search_idx`,
	} {
		s.DB().QueryRow(q).Scan(&n)
		if n != 0 {
			t.Errorf("%q expected 0, got %d", q, n)
		}
	}
}

func TestFTS5_BasicMatch(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()

	docs := []SearchDoc{
		{DocID: "n1", DocKind: DocNode, Name: "order service", FQN: "com x order service", Path: "src order java"},
		{DocID: "n2", DocKind: DocNode, Name: "payment processor", FQN: "com x payment processor", Path: "src payment java"},
		{DocID: "f1", DocKind: DocFile, Path: "src order java"},
	}
	for _, d := range docs {
		if err := InsertSearchDoc(tx, d); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()

	rows, err := s.DB().Query(`
		SELECT doc_id FROM search_idx
		WHERE search_idx MATCH ?
		ORDER BY bm25(search_idx)
	`, "order")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		got = append(got, id)
	}
	if len(got) < 2 {
		t.Fatalf("expected ≥2 hits for 'order', got %v", got)
	}
}

func TestFTS5_PrefixMatch(t *testing.T) {
	s := openTestStore(t)
	tx, _ := s.Begin()
	InsertSearchDoc(tx, SearchDoc{
		DocID: "n1", DocKind: DocNode, Name: "supplier", FQN: "x supplier", Path: "",
	})
	tx.Commit()

	var n int
	s.DB().QueryRow(
		`SELECT COUNT(*) FROM search_idx WHERE search_idx MATCH ?`, "supp*",
	).Scan(&n)
	if n != 1 {
		t.Errorf("prefix match failed: got %d, want 1", n)
	}
}