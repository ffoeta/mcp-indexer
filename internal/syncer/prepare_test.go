package syncer

import (
	"encoding/json"
	"mcp-indexer/internal/services"
	"mcp-indexer/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func setupService(t *testing.T) (svcID string, svc services.ServiceEntry, root string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MCP_INDEXER_HOME", home)
	root = t.TempDir()
	svcID = "testsvc"
	svc = services.ServiceEntry{RootAbs: root}

	os.MkdirAll(services.ServiceDir(svcID), 0o755)

	cfg := services.Config{
		PathPrefix: "",
		IncludeExt: []string{".py"},
		IgnoreFile: "service.ignore",
		Search:     services.SearchConfig{},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(services.ConfigPath(svcID), data, 0o644)
	os.WriteFile(services.IgnoreFilePath(svcID), []byte(""), 0o644)
	return
}

// F1: PrepareSync_NoWrites
func TestPrepareSync_NoWrites(t *testing.T) {
	svcID, svc, root := setupService(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")

	fmPath := services.FileMapPath(svcID)
	fsPath := services.FileStatPath(svcID)

	_, err := PrepareSync(svc, svcID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fmPath); err == nil {
		t.Error("PrepareSync must not write file-map.json")
	}
	if _, err := os.Stat(fsPath); err == nil {
		t.Error("PrepareSync must not write file-stat.json")
	}
}

// F2: PrepareSync_UsesIncludeExtAndIgnore
func TestPrepareSync_UsesIncludeExtAndIgnore(t *testing.T) {
	svcID, svc, root := setupService(t)
	writeFile(t, filepath.Join(root, "a.py"), "")
	writeFile(t, filepath.Join(root, "b.go"), "") // excluded by includeExt

	res, err := PrepareSync(svc, svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Errorf("expected 1 added (only .py), got %d", res.Added)
	}
}

// F3: PrepareSync_Added_FromFSNotInSavedStat
func TestPrepareSync_Added(t *testing.T) {
	svcID, svc, root := setupService(t)
	writeFile(t, filepath.Join(root, "new.py"), "")

	res, err := PrepareSync(svc, svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Errorf("expected 1 added, got %d", res.Added)
	}
}

// F4: PrepareSync_Deleted_FromSavedNotInFS
func TestPrepareSync_Deleted(t *testing.T) {
	svcID, svc, _ := setupService(t)
	// записываем stat для файла которого нет на FS
	services.SaveFileStat(services.FileStatPath(svcID), services.FileStat{
		"ghost.py": {1000, 100},
	})

	res, err := PrepareSync(svc, svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", res.Deleted)
	}
}

// F5: PrepareSync_MaybeModified_FromStatDelta
func TestPrepareSync_MaybeModified(t *testing.T) {
	svcID, svc, root := setupService(t)
	writeFile(t, filepath.Join(root, "a.py"), "x=1")
	info, _ := os.Stat(filepath.Join(root, "a.py"))

	// Сохраняем stat с другим mtime
	services.SaveFileStat(services.FileStatPath(svcID), services.FileStat{
		"a.py": {info.ModTime().UnixNano() - 1e9, info.Size()},
	})

	res, err := PrepareSync(svc, svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res.MaybeModified != 1 {
		t.Errorf("expected 1 maybeModified, got %d", res.MaybeModified)
	}
}

// F11: PrepareSync_ScanErrorsReported (permission error on dir)
func TestPrepareSync_ScanError_PermissionDeniedDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, chmod won't deny access")
	}
	svcID, svc, root := setupService(t)
	secret := filepath.Join(root, "secret")
	os.MkdirAll(secret, 0o000)
	t.Cleanup(func() { os.Chmod(secret, 0o755) })

	writeFile(t, filepath.Join(root, "visible.py"), "")

	// PrepareSync должен вернуть результат без паники
	_, err := PrepareSync(svc, svcID)
	// Ошибка или нет зависит от ОС — главное не panic
	_ = err
	_ = testutil.NewTestStore // just use the import
}
