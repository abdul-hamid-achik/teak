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
		{
			Extensions: []string{".c", ".h"},
			Command:    "clangd",
			Args:       []string{},
			LanguageID: "c",
		},
		{
			Extensions: []string{".cpp", ".hpp", ".cc", ".cxx"},
			Command:    "clangd",
			Args:       []string{},
			LanguageID: "cpp",
		},
		{
			Extensions: []string{".java"},
			Command:    "jdtls",
			Args:       []string{},
			LanguageID: "java",
		},
		{
			Extensions: []string{".lua"},
			Command:    "lua-language-server",
			Args:       []string{},
			LanguageID: "lua",
		},
		{
			Extensions: []string{".zig"},
			Command:    "zls",
			Args:       []string{},
			LanguageID: "zig",
		},
		{
			Extensions: []string{".rb"},
			Command:    "solargraph",
			Args:       []string{"stdio"},
			LanguageID: "ruby",
		},
		{
			Extensions: []string{".ex", ".exs"},
			Command:    "elixir-ls",
			Args:       []string{},
			LanguageID: "elixir",
		},
		{
			Extensions: []string{".cs"},
			Command:    "OmniSharp",
			Args:       []string{"--languageserver"},
			LanguageID: "csharp",
		},
		{
			Extensions: []string{".yaml", ".yml"},
			Command:    "yaml-language-server",
			Args:       []string{"--stdio"},
			LanguageID: "yaml",
		},
		{
			Extensions: []string{".sh", ".bash"},
			Command:    "bash-language-server",
			Args:       []string{"start"},
			LanguageID: "shellscript",
		},
		{
			Extensions: []string{".css", ".scss", ".less"},
			Command:    "vscode-css-language-server",
			Args:       []string{"--stdio"},
			LanguageID: "css",
		},
		{
			Extensions: []string{".html", ".htm"},
			Command:    "vscode-html-language-server",
			Args:       []string{"--stdio"},
			LanguageID: "html",
		},
		{
			Extensions: []string{".json"},
			Command:    "vscode-json-language-server",
			Args:       []string{"--stdio"},
			LanguageID: "json",
		},
	}
}

// ConfigForFile returns the server config matching a file path, or nil if none match.
// Uses the built-in default configs.
func ConfigForFile(path string) *ServerConfig {
	return configForFile(DefaultConfigs(), path)
}

// configForFile returns the server config matching a file path from the given list.
func configForFile(configs []ServerConfig, path string) *ServerConfig {
	ext := strings.ToLower(filepath.Ext(path))
	for _, cfg := range configs {
		for _, e := range cfg.Extensions {
			if e == ext {
				c := cfg
				return &c
			}
		}
	}
	return nil
}

// MergeConfigs merges user configs over defaults. User entries that match any
// extension of a default entry replace that default; otherwise they are appended.
func MergeConfigs(defaults, user []ServerConfig) []ServerConfig {
	if len(user) == 0 {
		return defaults
	}

	// Build a set of extensions overridden by user configs
	overridden := make(map[string]bool)
	for _, u := range user {
		for _, ext := range u.Extensions {
			overridden[ext] = true
		}
	}

	// Keep default entries whose extensions are not overridden
	var result []ServerConfig
	for _, d := range defaults {
		keep := true
		for _, ext := range d.Extensions {
			if overridden[ext] {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, d)
		}
	}

	return append(result, user...)
}
