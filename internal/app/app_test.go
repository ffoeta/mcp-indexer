package app

import (
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/common/services"
	"os"
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
		stores:   make(map[string]*store.Store),
	}
}

// A5: App_AddService_CreatesDir
func TestApp_AddService_CreatesDir(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "mysvc", "", nil)
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

	svcID, err := a.AddService(root, "svc2", "", nil)
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

	_, err := a.AddService(root, "svc3", "", nil)
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

	if _, err := a.AddService(root, "dup", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddService(root, "dup", "", nil); err == nil {
		t.Error("expected error for duplicate service ID")
	}
}

// A9: App_AddService_InvalidRoot_Errors
func TestApp_AddService_InvalidRoot_Errors(t *testing.T) {
	a := setupApp(t)
	_, err := a.AddService("/nonexistent/path/xyz", "svc4", "", nil)
	if err == nil {
		t.Error("expected error for non-existent root")
	}
}

// A10: App_GetServiceInfo_Found
func TestApp_GetServiceInfo_Found(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "info-svc", "", nil)
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
		if _, err := a.AddService(root, id, "", nil); err != nil {
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


// A17: App_GetSymbolFull_UnknownSymbol_Errors
func TestApp_GetSymbolFull_UnknownSymbol_Errors(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()
	svcID, _ := a.AddService(root, "fsvc2", "", nil)

	_, err := a.GetSymbolFull(svcID, "s:py:Ghost:x.py:0", 1)
	if err == nil {
		t.Error("expected error for unknown symbol")
	}
}

// A18: App_UpdateServiceMeta_PersistsFields
func TestApp_UpdateServiceMeta_PersistsFields(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "meta-svc", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	err = a.UpdateServiceMeta(svcID, "updated desc", []string{"order", "supplier"})
	if err != nil {
		t.Fatal(err)
	}

	// Verify persisted to disk
	reg2, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg2.Get(svcID)
	if !ok {
		t.Fatal("service not found after UpdateServiceMeta")
	}
	if entry.Description != "updated desc" {
		t.Errorf("expected description=%q, got %q", "updated desc", entry.Description)
	}
	if len(entry.MainEntities) != 2 || entry.MainEntities[0] != "order" {
		t.Errorf("unexpected mainEntities: %v", entry.MainEntities)
	}
}

// A19: App_UpdateServiceMeta_UnknownService_Errors
func TestApp_UpdateServiceMeta_UnknownService_Errors(t *testing.T) {
	a := setupApp(t)
	err := a.UpdateServiceMeta("ghost", "desc", nil)
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

// A20: App_UpdateServiceMeta_EmptyValues_DoNotOverwrite
func TestApp_UpdateServiceMeta_EmptyValues_DoNotOverwrite(t *testing.T) {
	a := setupApp(t)
	root := t.TempDir()

	svcID, err := a.AddService(root, "keep-svc", "original desc", []string{"entity1"})
	if err != nil {
		t.Fatal(err)
	}

	// Call with empty values — existing data must be preserved
	if err := a.UpdateServiceMeta(svcID, "", nil); err != nil {
		t.Fatal(err)
	}

	entry, ok := a.Registry.Get(svcID)
	if !ok {
		t.Fatal("service not found")
	}
	if entry.Description != "original desc" {
		t.Errorf("description was overwritten: got %q", entry.Description)
	}
	if len(entry.MainEntities) != 1 || entry.MainEntities[0] != "entity1" {
		t.Errorf("mainEntities was overwritten: %v", entry.MainEntities)
	}
}
