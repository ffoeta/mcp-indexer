package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FileID возвращает fileId = "f:" + key.
func FileID(key string) string { return "f:" + key }

// SymbolID возвращает symbolId.
// qualified может совпадать с name если квалифицированное имя неизвестно.
func SymbolID(lang, qualified, key string, startLine int) string {
	return fmt.Sprintf("s:%s:%s:%s:%d", lang, qualified, key, startLine)
}

// UnresolvedID возвращает id для неразрешённой ссылки.
func UnresolvedID(name string) string { return "x:" + name }

// PythonModuleName вычисляет имя Python-модуля из rel_path (без pathPrefix).
// pkg/__init__.py → pkg
// pkg/collector.py → pkg.collector
func PythonModuleName(relPath string) string {
	// нормализуем слеши
	relPath = filepath.ToSlash(relPath)
	// убираем расширение
	if ext := filepath.Ext(relPath); ext != "" {
		relPath = relPath[:len(relPath)-len(ext)]
	}
	// __init__ → пакет
	parts := strings.Split(relPath, "/")
	if len(parts) > 0 && parts[len(parts)-1] == "__init__" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}
