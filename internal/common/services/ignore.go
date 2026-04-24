package services

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Matcher проверяет rel_path по doublestar-паттернам из service.ignore.
type Matcher struct {
	patterns []string
}

func LoadMatcher(path string) (*Matcher, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return &Matcher{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open ignore %s: %w", path, err)
	}
	defer f.Close()

	var patterns []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return &Matcher{patterns: patterns}, sc.Err()
}

// Match возвращает true если relPath (unix слеши) совпадает с любым паттерном.
// Семантика gitignore: паттерн без '/' матчится по basename на любом уровне,
// но только для чистых unix-путей (без ':' — т.е. без pathPrefix).
func (m *Matcher) Match(relPath string) bool {
	basename := relPath
	if idx := strings.LastIndex(relPath, "/"); idx >= 0 {
		basename = relPath[idx+1:]
	}
	// Basename-matching применяем только к чистым unix-путям (rel_path, без prefix вида "src:")
	isUnixPath := !strings.Contains(relPath, ":")

	for _, p := range m.patterns {
		if ok, _ := doublestar.Match(p, relPath); ok {
			return true
		}
		// dir-prefix: pattern "vendor/" → совпадает с путями внутри vendor/
		if strings.HasSuffix(p, "/") {
			dir := strings.TrimSuffix(p, "/")
			if strings.HasPrefix(relPath, dir+"/") {
				return true
			}
			continue
		}
		// Паттерн без '/' → gitignore-семантика: матчим по basename
		if isUnixPath && !strings.Contains(p, "/") {
			if ok, _ := doublestar.Match(p, basename); ok {
				return true
			}
		}
	}
	return false
}
