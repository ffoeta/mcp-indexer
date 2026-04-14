package app

import (
	sqliteq "mcp-indexer/internal/index/sqlite"
	"mcp-indexer/internal/services"
	"os"
	"path/filepath"
	"testing"
)

func setupApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MCP_INDEXER_HOME", home)

	reg, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	return &App{
		Registry: reg,
		stores:   make(map[string]*sqliteq.Store),
	}
}

// A5: App_AddService_CreatesDir
func TestApp_AddService_CreatesDir(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "mysvc", "My Service")
	if err != nil {
		t.Fatal(err)
	}
	if svcID != "mysvc" {
		t.Errorf("expected svcID=mysvc, got %q", svcID)
	}

	dir := services.ServiceDir(svcID)
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("service dir not created: %v", err)
	}
}

// A6: App_AddService_WritesConfig
func TestApp_AddService_WritesConfig(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "svc2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(services.ConfigPath(svcID)); err != nil {
		t.Error("config.json not created")
	}
	if _, err := os.Stat(services.IgnoreFilePath(svcID)); err != nil {
		t.Error("service.ignore not created")
	}
}

// A7: App_AddService_SavesRegistry
func TestApp_AddService_SavesRegistry(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	_, err := a.AddService(root, "svc3", "")
	if err != nil {
		t.Fatal(err)
	}

	// Re-load registry from disk
	reg2, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg2.Get("svc3"); !ok {
		t.Error("svc3 not found in saved registry")
	}
}

// A8: App_AddService_DuplicateID_Errors
func TestApp_AddService_DuplicateID_Errors(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	if _, err := a.AddService(root, "dup", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddService(root, "dup", ""); err == nil {
		t.Error("expected error for duplicate service ID")
	}
}

// A9: App_AddService_InvalidRoot_Errors
func TestApp_AddService_InvalidRoot_Errors(t *testing.T) {
	a := setupApp(t)
	_, err := a.AddService("/nonexistent/path/xyz", "svc4", "")
	if err == nil {
		t.Error("expected error for non-existent root")
	}
}

// A10: App_GetServiceInfo_Found
func TestApp_GetServiceInfo_Found(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "info-svc", "InfoService")
	if err != nil {
		t.Fatal(err)
	}

	info, err := a.GetServiceInfo(svcID)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := info.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", info)
	}
	if m["serviceId"] != svcID {
		t.Errorf("expected serviceId=%q, got %v", svcID, m["serviceId"])
	}
}

// A11: App_GetServiceInfo_NotFound_Errors
func TestApp_GetServiceInfo_NotFound_Errors(t *testing.T) {
	a := setupApp(t)
	_, err := a.GetServiceInfo("ghost")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

// A12: App_ListServicesSorted
func TestApp_ListServicesSorted(t *testing.T) {
	a := setupApp(t)

	for _, id := range []string{"z-svc", "a-svc", "m-svc"} {
		root := t.TempDir()
		if _, err := a.AddService(root, id, ""); err != nil {
			t.Fatal(err)
		}
	}

	list := a.ListServicesSorted()
	if len(list) < 3 {
		t.Fatalf("expected 3 services, got %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i] < list[i-1] {
			t.Errorf("not sorted: %v", list)
		}
	}
}

// A13: App_PrepareSync_UnknownService_Errors
func TestApp_PrepareSync_UnknownService_Errors(t *testing.T) {
	a := setupApp(t)
	_, err := a.PrepareSync("ghost")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

// A14: App_PrepareSync_Works
func TestApp_PrepareSync_Works(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	svcID, err := a.AddService(root, "sync-svc", "")
	if err != nil {
		t.Fatal(err)
	}

	// Write a python file
	os.WriteFile(filepath.Join(root, "a.py"), []byte("x=1"), 0o644)

	// Config needs to have .py extension (AddService writes DefaultConfig which includes .py)

	res, err := a.PrepareSync(svcID)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}
