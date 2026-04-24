package engine

import (
	"encoding/json"
	"fmt"
	"mcp-indexer/internal/common/services"
	"mcp-indexer/internal/common/store"
	"mcp-indexer/internal/indexer/parse"
	"mcp-indexer/internal/indexer/scan"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Collect — Phase 1:
// 1. Сканирует файлы проекта.
// 2. Парсит каждый файл.
// 3. Строит defined_map (fullQualified → entry) и fileModuleMap (importStr → fileKey).
// 4. Определяет внешние зависимости (imports, не разрешённые во внутренние файлы).
// 5. Сохраняет symbols_defined.json и symbols_used.json в svcDir.
func Collect(
	rootAbs string,
	cfg *services.Config,
	matcher *services.Matcher,
	parsers map[string]parse.Parser,
	svcDir string,
) (*CollectResult, error) {
	entries, err := scan.Scan(rootAbs, cfg, matcher)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	cr := &CollectResult{
		DefinedMap:    make(map[string]DefinedEntry),
		FileModuleMap: make(map[string]string),
	}

	// --- Парсинг файлов ---
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.AbsPath))
		p, ok := parsers[ext]
		if !ok {
			continue
		}

		lang := langFromExt(ext)
		res, err := p.Parse(e.AbsPath)
		if err != nil {
			continue // best-effort: пропускаем файлы с ошибками парсинга
		}

		rf := &RawFile{
			Key:     e.Key,
			AbsPath: e.AbsPath,
			RelPath: e.RelPath,
			Lang:    lang,
			Symbols: res.Symbols,
			Imports: res.Imports,
			Calls:   res.Calls,
		}
		cr.Files = append(cr.Files, rf)

		// Строим defined_map: fullQualified → DefinedEntry
		modPrefix := modulePrefix(rf.RelPath, lang)
		for _, sym := range res.Symbols {
			fq := fullQualified(modPrefix, sym.Qualified)
			cr.DefinedMap[fq] = DefinedEntry{
				FileKey: e.Key,
				Line:    sym.StartLine,
				Kind:    sym.Kind,
			}
		}
	}

	// --- Строим fileModuleMap: importString → fileKey ---
	for _, rf := range cr.Files {
		switch rf.Lang {
		case "python":
			mod := store.PythonModuleName(rf.RelPath)
			cr.FileModuleMap[mod] = rf.Key
		case "java":
			// Полный FQN: com/example/OrderService.java → com.example.OrderService
			fqn := javaFQN(rf.RelPath)
			cr.FileModuleMap[fqn] = rf.Key
			// Простое имя: OrderService (только если нет коллизий)
			parts := strings.Split(fqn, ".")
			if simpleName := parts[len(parts)-1]; simpleName != "" {
				if _, exists := cr.FileModuleMap[simpleName]; !exists {
					cr.FileModuleMap[simpleName] = rf.Key
				}
			}
		}
	}

	// --- Внешние импорты (не разрешились во внутренние файлы) ---
	seen := map[string]bool{}
	for _, rf := range cr.Files {
		for _, imp := range rf.Imports {
			if _, internal := cr.FileModuleMap[imp]; !internal && !seen[imp] {
				seen[imp] = true
				cr.External = append(cr.External, imp)
			}
		}
	}
	sort.Strings(cr.External)

	// --- Сохраняем JSON-артефакты ---
	if err := saveJSON(filepath.Join(svcDir, "symbols_defined.json"), cr.DefinedMap); err != nil {
		return nil, fmt.Errorf("save symbols_defined.json: %w", err)
	}
	if err := saveJSON(filepath.Join(svcDir, "symbols_used.json"), cr.External); err != nil {
		return nil, fmt.Errorf("save symbols_used.json: %w", err)
	}

	return cr, nil
}

// modulePrefix возвращает префикс модуля для файла (для построения fullQualified).
// Python: "pkg/collector.py" → "pkg.collector"
// Java: нет модульного префикса, используем квалифицированное имя из парсера.
func modulePrefix(relPath, lang string) string {
	switch lang {
	case "python":
		return store.PythonModuleName(relPath)
	default:
		return ""
	}
}

// fullQualified объединяет модульный префикс и qualified из парсера.
func fullQualified(prefix, qualified string) string {
	if prefix == "" {
		return qualified
	}
	return prefix + "." + qualified
}

// javaFQN конвертирует rel_path Java-файла в FQN.
// "com/example/OrderService.java" → "com.example.OrderService"
func javaFQN(relPath string) string {
	s := strings.TrimSuffix(relPath, ".java")
	return strings.ReplaceAll(s, "/", ".")
}

// langFromExt возвращает название языка по расширению файла.
func langFromExt(ext string) string {
	switch ext {
	case ".py":
		return "python"
	case ".java":
		return "java"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}

func saveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
