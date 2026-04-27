package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FileID возвращает канонический fileId = "f:" + key.
func FileID(key string) string { return "f:" + key }

// NodeID — канонический ID ноды.
// Формат: "n:{lang}:{kind[0]}:{fqn}:{key}:{startLine}"
//
//	kind[0] = 'o' (object) или 'm' (method) — компактнее, чем полное слово.
func NodeID(lang, kind, fqn, key string, startLine int) string {
	return fmt.Sprintf("n:%s:%s:%s:%s:%d", lang, kindShort(kind), fqn, key, startLine)
}

func kindShort(kind string) string {
	switch kind {
	case KindObject:
		return "o"
	case KindMethod:
		return "m"
	default:
		return kind
	}
}

// ShortFileID возвращает короткий ID для MCP-payload: "f12".
func ShortFileID(shortID int64) string { return fmt.Sprintf("f%d", shortID) }

// ShortNodeID возвращает короткий ID для MCP-payload: "m412" / "o7".
func ShortNodeID(kind string, shortID int64) string {
	return fmt.Sprintf("%s%d", kindShort(kind), shortID)
}

// PythonModuleName вычисляет имя Python-модуля из rel_path (без pathPrefix).
// pkg/__init__.py → pkg
// pkg/collector.py → pkg.collector
func PythonModuleName(relPath string) string {
	relPath = filepath.ToSlash(relPath)
	if ext := filepath.Ext(relPath); ext != "" {
		relPath = relPath[:len(relPath)-len(ext)]
	}
	parts := strings.Split(relPath, "/")
	if len(parts) > 0 && parts[len(parts)-1] == "__init__" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}