package syncer

import (
	"fmt"
	"io/fs"
	"mcp-indexer/internal/services"
	"path/filepath"
	"strings"
	"time"
)

// FileEntry описывает файл после сканирования.
type FileEntry struct {
	Key     string // pathPrefix + rel_path (unix slashes)
	AbsPath string
	RelPath string // rel_path без prefix
	Size    int64
	ModTime time.Time
}

// Scan обходит rootAbs, применяет фильтры, возвращает файлы.
func Scan(rootAbs string, cfg *services.Config, matcher *services.Matcher) ([]FileEntry, error) {
	extSet := buildExtSet(cfg.IncludeExt)
	var entries []FileEntry

	err := filepath.WalkDir(rootAbs, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", absPath, walkErr)
		}
		if absPath == rootAbs {
			return nil
		}

		rel, err := filepath.Rel(rootAbs, absPath)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)

		if matcher.Match(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		if len(extSet) > 0 {
			ext := strings.ToLower(filepath.Ext(absPath))
			if _, ok := extSet[ext]; !ok {
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", absPath, err)
		}

		entries = append(entries, FileEntry{
			Key:     cfg.PathPrefix + relSlash,
			AbsPath: absPath,
			RelPath: relSlash,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	return entries, err
}

func buildExtSet(exts []string) map[string]struct{} {
	if len(exts) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		m[strings.ToLower(e)] = struct{}{}
	}
	return m
}
