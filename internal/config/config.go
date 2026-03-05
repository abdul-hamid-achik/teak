package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all application configuration.
type Config struct {
	Editor EditorConfig `toml:"editor"`
	UI     UIConfig     `toml:"ui"`
	LSP    []LSPConfig  `toml:"lsp"`
}

// EditorConfig holds editor-specific settings.
type EditorConfig struct {
	TabSize    int  `toml:"tab_size"`
	InsertTabs bool `toml:"insert_tabs"`
	AutoIndent bool `toml:"auto_indent"`
}

// UIConfig holds UI-related settings.
type UIConfig struct {
	Theme    string `toml:"theme"`
	ShowTree bool   `toml:"show_tree"`
}

// LSPConfig describes how to launch a language server.
type LSPConfig struct {
	Extensions []string `toml:"extensions"`
	Command    string   `toml:"command"`
	Args       []string `toml:"args"`
	LanguageID string   `toml:"language_id"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Editor: EditorConfig{
			TabSize:    4,
			InsertTabs: false,
			AutoIndent: true,
		},
		UI: UIConfig{
			Theme:    "nord",
			ShowTree: true,
		},
	}
}

// configPath returns the path to the config file.
func configPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "teak", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "teak", "config.toml")
}

// ConfigPath returns the path to the config file (exported).
func ConfigPath() string {
	return configPath()
}

// Load reads configuration from ~/.config/teak/config.toml, falling back to defaults.
func Load() (Config, error) {
	cfg := DefaultConfig()

	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	var user userConfig
	if err := toml.Unmarshal(data, &user); err != nil {
		return cfg, err
	}

	merge(&cfg, &user)
	return cfg, nil
}

// userConfig mirrors Config but with pointer fields for merge detection.
type userConfig struct {
	Editor *userEditorConfig `toml:"editor"`
	UI     *userUIConfig     `toml:"ui"`
	LSP    []LSPConfig       `toml:"lsp"`
}

type userEditorConfig struct {
	TabSize    *int  `toml:"tab_size"`
	InsertTabs *bool `toml:"insert_tabs"`
	AutoIndent *bool `toml:"auto_indent"`
}

type userUIConfig struct {
	Theme    *string `toml:"theme"`
	ShowTree *bool   `toml:"show_tree"`
}

// merge applies user overrides onto defaults (only non-nil values).
func merge(cfg *Config, user *userConfig) {
	if user.Editor != nil {
		if user.Editor.TabSize != nil {
			cfg.Editor.TabSize = *user.Editor.TabSize
		}
		if user.Editor.InsertTabs != nil {
			cfg.Editor.InsertTabs = *user.Editor.InsertTabs
		}
		if user.Editor.AutoIndent != nil {
			cfg.Editor.AutoIndent = *user.Editor.AutoIndent
		}
	}
	if user.UI != nil {
		if user.UI.Theme != nil {
			cfg.UI.Theme = *user.UI.Theme
		}
		if user.UI.ShowTree != nil {
			cfg.UI.ShowTree = *user.UI.ShowTree
		}
	}
	if len(user.LSP) > 0 {
		cfg.LSP = user.LSP
	}
}
