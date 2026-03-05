package lsp

import (
	"path/filepath"
	"strings"
)

// ServerConfig describes how to launch a language server.
type ServerConfig struct {
	Extensions []string
	Command    string
	Args       []string
	LanguageID string
}

// DefaultConfigs returns built-in configurations for common language servers.
func DefaultConfigs() []ServerConfig {
	return []ServerConfig{
		{
			Extensions: []string{".go"},
			Command:    "gopls",
			Args:       []string{},
			LanguageID: "go",
		},
		{
			Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
			Command:    "typescript-language-server",
			Args:       []string{"--stdio"},
			LanguageID: "typescript",
		},
		{
			Extensions: []string{".py"},
			Command:    "pylsp",
			Args:       []string{},
			LanguageID: "python",
		},
		{
			Extensions: []string{".rs"},
			Command:    "rust-analyzer",
			Args:       []string{},
			LanguageID: "rust",
		},
	}
}

// ConfigForFile returns the server config matching a file path, or nil if none match.
func ConfigForFile(path string) *ServerConfig {
	ext := strings.ToLower(filepath.Ext(path))
	for _, cfg := range DefaultConfigs() {
		for _, e := range cfg.Extensions {
			if e == ext {
				c := cfg
				return &c
			}
		}
	}
	return nil
}
