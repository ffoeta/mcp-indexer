package syncer

import (
	"encoding/json"
	"mcp-indexer/internal/services"
	"mcp-indexer/internal/testutil"
	"mcp-indexer/internal/tokenize"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupDoSync(t *testing.T) (svcID string, svc services.ServiceEntry, root string) {
	t.Helper()
	return setupService(t) // reuse prepare_test.go helper
}

func makeNorm() *tokenize.Normalizer {
	return tokenize.New(map[string]struct{}{})
}

// G1: DoSync_ComputesBlake3_HashPrefixB3
func TestDoSync_ComputesBlake3_HashPrefixB3(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	store := testutil.NewTestStore(t)

	res, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("expected 1 added, got %d", res.Added)
	}

	// check file-map.json has b3: prefix
	fm, err := services.LoadFileMap(services.FileMapPath(svcID))
	if err != nil {
		t.Fatal(err)
	}
	hash, ok := fm["a.py"]
	if !ok {
		t.Fatal("a.py not in file-map")
	}
	if !strings.HasPrefix(hash, "b3:") {
		t.Errorf("hash %q must have b3: prefix", hash)
	}
	if len(hash) < 10 {
		t.Errorf("hash %q too short", hash)
	}
}

// G2: DoSync_DetectsModified_ByHash
func TestDoSync_DetectsModified_ByHash(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	path := filepath.Join(root, "a.py")
	writeFile(t, path, "x=1")
	store := testutil.NewTestStore(t)

	// first sync — adds file
	_, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}

	// modify content
	writeFile(t, path, "x=2")

	// second sync — should detect as modified
	res2, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if res2.Modified != 1 {
		t.Errorf("expected 1 modified, got %d", res2.Modified)
	}
}

// G3: DoSync_HashError_AddsSyncError
func TestDoSync_HashError_AddsSyncError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, chmod won't deny read")
	}
	svcID, svc, root := setupDoSync(t)
	path := filepath.Join(root, "secret.py")
	writeFile(t, path, "x=1")
	os.Chmod(path, 0o000)
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	store := testutil.NewTestStore(t)
	res, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "HASH_ERROR" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected HASH_ERROR in errors, got %+v", res.Errors)
	}
}

// G4: DoSync_RewritesFileMap_Atomically (no .tmp left)
func TestDoSync_RewritesFileMap_Atomically(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	store := testutil.NewTestStore(t)

	_, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(services.FileMapPath(svcID) + ".tmp"); err == nil {
		t.Error(".tmp file must not remain after DoSync")
	}
}

// G5: DoSync_OnMapWriteFailure_ReturnsMAP_WRITE_ERROR
func TestDoSync_OnMapWriteFailure_ReturnsMAP_WRITE_ERROR(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	store := testutil.NewTestStore(t)

	// Make service dir read-only so map write fails
	serviceDir := services.ServiceDir(svcID)
	if err := os.Chmod(serviceDir, 0o555); err != nil {
		t.Skip("cannot chmod service dir")
	}
	t.Cleanup(func() { os.Chmod(serviceDir, 0o755) })

	res, err := DoSync(svc, svcID, store, nil, makeNorm())
	// must not return error — MAP_WRITE_ERROR goes into res.Errors
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "MAP_WRITE_ERROR" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected MAP_WRITE_ERROR in errors, got %+v", res.Errors)
	}
}

// G6: DoSync_ErrorsAlwaysNonNil
func TestDoSync_ErrorsAlwaysNonNil(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	store := testutil.NewTestStore(t)

	res, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if res.Errors == nil {
		t.Error("Errors must be non-nil slice, not nil")
	}
}

// G7: DoSync_FileMap_HasCorrectContent
func TestDoSync_FileMap_HasCorrectContent(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "hello")
	writeFile(t, filepath.Join(root, "b.py"), "world")
	store := testutil.NewTestStore(t)

	_, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(services.FileMapPath(svcID))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("file-map not valid JSON: %v", err)
	}
	if len(raw) != 2 {
		t.Errorf("expected 2 entries in file-map, got %d", len(raw))
	}
}

// G8: DoSync_DeletedFile_RemovedFromMap
func TestDoSync_DeletedFile_RemovedFromMap(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	path := filepath.Join(root, "a.py")
	writeFile(t, path, "x=1")
	store := testutil.NewTestStore(t)

	// First sync — adds file
	_, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}

	// Remove file from FS
	os.Remove(path)

	// Second sync — should delete
	res2, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}

	if res2.Deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", res2.Deleted)
	}

	fm, _ := services.LoadFileMap(services.FileMapPath(svcID))
	if _, ok := fm["a.py"]; ok {
		t.Error("a.py should be removed from file-map after delete")
	}
}

// TestDoSync_NoChanges_EarlyExit проверяет, что повторный sync без изменений
// возвращает Added=0, Modified=0, Deleted=0.
func TestDoSync_NoChanges_EarlyExit(t *testing.T) {
	svcID, svc, root := setupDoSync(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	writeFile(t, filepath.Join(root, "b.py"), "y=2")
	store := testutil.NewTestStore(t)

	res1, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if res1.Added != 2 {
		t.Fatalf("first sync: expected 2 added, got %d", res1.Added)
	}

	// Второй sync без изменений → ранний выход
	res2, err := DoSync(svc, svcID, store, nil, makeNorm())
	if err != nil {
		t.Fatal(err)
	}
	if res2.Added != 0 || res2.Modified != 0 || res2.Deleted != 0 {
		t.Errorf("no-op sync: expected 0/0/0, got added=%d modified=%d deleted=%d",
			res2.Added, res2.Modified, res2.Deleted)
	}
}
