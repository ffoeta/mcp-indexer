package services

import (
	"encoding/json"
	"fmt"
	"os"
)

// FileMap — map key -> "b3:<hex>".
type FileMap map[string]string

// FileStat — map key -> [mtime_ns, size].
type FileStat map[string][2]int64

func LoadFileMap(path string) (FileMap, error) {
	m := make(FileMap)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read file-map %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse file-map: %w", err)
	}
	return m, nil
}

func SaveFileMap(path string, m FileMap) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

func LoadFileStat(path string) (FileStat, error) {
	m := make(FileStat)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read file-stat %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse file-stat: %w", err)
	}
	return m, nil
}

func SaveFileStat(path string, m FileStat) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// atomicWriteFile записывает данные во временный файл, затем переименовывает.
func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
