package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A1: AppHome_DefaultPath_Resolves
func TestAppHome_DefaultPath_Resolves(t *testing.T) {
	t.Setenv("MCP_INDEXER_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".mcp-indexer")
	if got := AppHome(); got != want {
		t.Errorf("AppHome() = %q, want %q", got, want)
	}
}

// A2: AppHome_EnvOverride_Wins
func TestAppHome_EnvOverride_Wins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MCP_INDEXER_HOME", dir)
	if got := AppHome(); got != dir {
		t.Errorf("AppHome() = %q, want %q", got, dir)
	}
}

func TestRegistryPath_ContainsRegistryJSON(t *testing.T) {
	t.Setenv("MCP_INDEXER_HOME", "/tmp/test-home")
	if !strings.HasSuffix(RegistryPath(), "registry.json") {
		t.Error("RegistryPath should end with registry.json")
	}
}

func TestServiceDir_ContainsID(t *testing.T) {
	t.Setenv("MCP_INDEXER_HOME", "/tmp/test-home")
	got := ServiceDir("mysvc")
	if !strings.Contains(got, "mysvc") {
		t.Errorf("ServiceDir should contain serviceId, got %q", got)
	}
}
