package services

import (
	"path/filepath"
	"testing"
)

// A3: Registry_LoadMissing_ReturnsEmpty
func TestRegistry_LoadMissing_ReturnsEmpty(t *testing.T) {
	r, err := LoadRegistry(filepath.Join(t.TempDir(), "no-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ids := r.List(); len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

// A4: Registry_SaveAndReload_RoundTrip
func TestRegistry_SaveAndReload_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r, _ := LoadRegistry(path)

	entry := ServiceEntry{RootAbs: "/some/root", Description: "test"}
	if err := r.Add("svc1", entry); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	r2, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r2.Get("svc1")
	if !ok {
		t.Fatal("svc1 not found after reload")
	}
	if got.RootAbs != entry.RootAbs || got.Description != entry.Description {
		t.Errorf("got %+v, want %+v", got, entry)
	}
}

func TestRegistry_Add_Collision(t *testing.T) {
	r, _ := LoadRegistry(filepath.Join(t.TempDir(), "r.json"))
	_ = r.Add("dup", ServiceEntry{RootAbs: "/a"})
	if err := r.Add("dup", ServiceEntry{RootAbs: "/b"}); err == nil {
		t.Error("expected collision error")
	}
}

func TestRegistry_Remove_NotFound(t *testing.T) {
	r, _ := LoadRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err := r.Remove("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent")
	}
}
