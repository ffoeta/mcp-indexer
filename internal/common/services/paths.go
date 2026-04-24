package services

import (
	"os"
	"path/filepath"
)

func AppHome() string {
	if h := os.Getenv("MCP_INDEXER_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("cannot determine home directory: " + err.Error())
	}
	return filepath.Join(home, ".mcp-indexer")
}

func RegistryPath() string              { return filepath.Join(AppHome(), "registry.json") }
func ServiceDir(id string) string       { return filepath.Join(AppHome(), "services", id) }
func ConfigPath(id string) string       { return filepath.Join(ServiceDir(id), "config.json") }
func IgnoreFilePath(id string) string   { return filepath.Join(ServiceDir(id), "service.ignore") }
func DBPath(id string) string           { return filepath.Join(ServiceDir(id), "index.db") }
