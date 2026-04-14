package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// D1: LoadFileMap_Missing_ReturnsEmpty
func TestLoadFileMap_Missing_ReturnsEmpty(t *testing.T) {
	m, err := LoadFileMap(filepath.Join(t.TempDir(), "no.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// D2: LoadFileStat_Missing_ReturnsEmpty
func TestLoadFileStat_Missing_ReturnsEmpty(t *testing.T) {
	m, err := LoadFileStat(filepath.Join(t.TempDir(), "no.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// D3: LoadFileMap_CorruptJSON_Errors
func TestLoadFileMap_CorruptJSON_Errors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "corrupt.json")
	os.WriteFile(p, []byte("{broken"), 0o644)
	if _, err := LoadFileMap(p); err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

// D4: WriteJSONAtomic_CreatesFile
func TestWriteJSONAtomic_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.json")
	if err := SaveFileMap(p, FileMap{"k": "b3:aabb"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("file not created")
	}
}

// D5: WriteJSONAtomic_OverwritesExisting
func TestWriteJSONAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.json")
	SaveFileMap(p, FileMap{"a": "b3:1111"})
	SaveFileMap(p, FileMap{"b": "b3:2222"})
	m, _ := LoadFileMap(p)
	if _, ok := m["a"]; ok {
		t.Error("old key should be gone after overwrite")
	}
	if _, ok := m["b"]; !ok {
		t.Error("new key should be present")
	}
}

// D6: WriteJSONAtomic_NoTmpLeft
func TestWriteJSONAtomic_NoTmpLeft(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.json")
	SaveFileMap(p, FileMap{"k": "v"})
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Error(".tmp file should not exist after write")
	}
}

// D8: WriteJSONAtomic_FailsOnReadOnlyDir
func TestWriteJSONAtomic_FailsOnReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod dir:", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	p := filepath.Join(dir, "map.json")
	if err := SaveFileMap(p, FileMap{"k": "v"}); err == nil {
		t.Error("expected error writing to read-only dir")
	}
}

// D9: FileMap_Format_IsMapKeyToHashOnly
func TestFileMap_Format_IsMapKeyToHashOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "map.json")
	SaveFileMap(p, FileMap{"src:pkg/a.py": "b3:aabbcc"})
	data, _ := os.ReadFile(p)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	v, ok := raw["src:pkg/a.py"]
	if !ok {
		t.Error("key not found")
	}
	if _, isStr := v.(string); !isStr {
		t.Errorf("value should be string, got %T", v)
	}
}

// D10: FileStat_Format_IsMapKeyToTupleOnly
func TestFileStat_Format_IsMapKeyToTupleOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stat.json")
	SaveFileStat(p, FileStat{"k": {12345, 100}})
	data, _ := os.ReadFile(p)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	v, ok := raw["k"]
	if !ok {
		t.Error("key not found")
	}
	arr, isArr := v.([]interface{})
	if !isArr || len(arr) != 2 {
		t.Errorf("expected [mtime,size] tuple, got %T %v", v, v)
	}
}
