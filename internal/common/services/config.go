package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// SearchConfig — параметры поиска из config.json.
type SearchConfig struct {
	StopWords []string `json:"stopWords"`
}

// Config — конфигурация сервиса (config.json).
type Config struct {
	PathPrefix string       `json:"pathPrefix"` // e.g. "src:" — должен заканчиваться на ":"
	IncludeExt []string     `json:"includeExt"` // e.g. [".py", ".java"] — не пустой
	IgnoreFile string       `json:"ignoreFile"`
	Search     SearchConfig `json:"search"`
}

// Validate проверяет инвариант конфига.
func (c *Config) Validate() error {
	if len(c.IncludeExt) == 0 {
		return errors.New("includeExt must not be empty")
	}
	if c.PathPrefix != "" && !strings.HasSuffix(c.PathPrefix, ":") {
		return fmt.Errorf("pathPrefix %q must end with ':'", c.PathPrefix)
	}
	return nil
}

func DefaultConfig() Config {
	return Config{
		PathPrefix: "",
		IncludeExt: []string{".py", ".java"},
		IgnoreFile: "service.ignore",
		Search:     SearchConfig{StopWords: DefaultStopWords()},
	}
}

func DefaultStopWords() []string {
	return []string{
		"a", "an", "the", "and", "or", "not", "in", "is", "it", "of", "to",
		"as", "at", "be", "by", "do", "for", "if", "on", "up", "we",
		"self", "this", "super", "true", "false", "null", "nil", "none",
		"new", "return", "import", "from", "def", "class", "func", "var",
		"let", "const", "type", "struct", "interface", "public", "private",
		"protected", "static", "final", "void", "int", "str", "bool",
		"list", "map", "set", "get", "err", "error",
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
