package services

import (
	"os"
	"path/filepath"
	"testing"
)

// B1: Config_Load_MinimalValid
func TestConfig_Load_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"pathPrefix":"src:","includeExt":[".py"],"ignoreFile":"service.ignore"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.PathPrefix != "src:" {
		t.Errorf("pathPrefix = %q", cfg.PathPrefix)
	}
	if len(cfg.IncludeExt) != 1 || cfg.IncludeExt[0] != ".py" {
		t.Errorf("includeExt = %v", cfg.IncludeExt)
	}
}

// B2: Config_PrefixMustEndWithColon
func TestConfig_PrefixMustEndWithColon(t *testing.T) {
	cfg := Config{PathPrefix: "src", IncludeExt: []string{".py"}}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for prefix without colon")
	}

	cfg2 := Config{PathPrefix: "src:", IncludeExt: []string{".py"}}
	if err := cfg2.Validate(); err != nil {
		t.Errorf("valid prefix rejected: %v", err)
	}

	cfg3 := Config{PathPrefix: "", IncludeExt: []string{".py"}}
	if err := cfg3.Validate(); err != nil {
		t.Errorf("empty prefix should be allowed: %v", err)
	}
}

// B3: Config_IncludeExt_Empty_Errors
func TestConfig_IncludeExt_Empty_Errors(t *testing.T) {
	cfg := Config{IncludeExt: nil}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty includeExt")
	}
	cfg2 := Config{IncludeExt: []string{}}
	if err := cfg2.Validate(); err == nil {
		t.Error("expected error for empty slice includeExt")
	}
}

func TestConfig_DefaultConfig_IsValid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig invalid: %v", err)
	}
}
