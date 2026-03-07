package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all application configuration.
type Config struct {
	Editor  EditorConfig  `toml:"editor"`
	UI      UIConfig      `toml:"ui"`
	LSP     []LSPConfig   `toml:"lsp"`
	Agent   AgentConfig   `toml:"agent"`
	Session SessionConfig `toml:"session"`
}

// SessionConfig configures session restore.
type SessionConfig struct {
	Enabled          bool `toml:"enabled"`
	AutoSaveInterval int  `toml:"auto_save_interval"` // seconds
}

// AgentConfig configures the ACP agent.
type AgentConfig struct {
	Enabled bool     `toml:"enabled"`
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// EditorConfig holds editor-specific settings.
type EditorConfig struct {
	TabSize      int  `toml:"tab_size"`
	InsertTabs   bool `toml:"insert_tabs"`
	AutoIndent   bool `toml:"auto_indent"`
	FormatOnSave bool `toml:"format_on_save"`
	WordWrap     bool `toml:"word_wrap"`
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
		Agent: AgentConfig{
			Enabled: true,
			Command: "opencode",
			Args:    []string{"acp"},
		},
		Session: SessionConfig{
			Enabled:          true,
			AutoSaveInterval: 30,
		},
	}
}

// configPath returns the path to the config file.
func configPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "teak", "config.toml")
	}
	// Fallback to temp directory for CI environments
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "teak", "config.toml")
	}
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
	Editor  *userEditorConfig  `toml:"editor"`
	UI      *userUIConfig      `toml:"ui"`
	LSP     []LSPConfig        `toml:"lsp"`
	Agent   *userAgentConfig   `toml:"agent"`
	Session *userSessionConfig `toml:"session"`
}

type userSessionConfig struct {
	Enabled          *bool `toml:"enabled"`
	AutoSaveInterval *int  `toml:"auto_save_interval"`
}

type userAgentConfig struct {
	Enabled *bool    `toml:"enabled"`
	Command *string  `toml:"command"`
	Args    []string `toml:"args"`
}

type userEditorConfig struct {
	TabSize      *int  `toml:"tab_size"`
	InsertTabs   *bool `toml:"insert_tabs"`
	AutoIndent   *bool `toml:"auto_indent"`
	FormatOnSave *bool `toml:"format_on_save"`
	WordWrap     *bool `toml:"word_wrap"`
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
		if user.Editor.FormatOnSave != nil {
			cfg.Editor.FormatOnSave = *user.Editor.FormatOnSave
		}
		if user.Editor.WordWrap != nil {
			cfg.Editor.WordWrap = *user.Editor.WordWrap
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
	if user.Session != nil {
		if user.Session.Enabled != nil {
			cfg.Session.Enabled = *user.Session.Enabled
		}
		if user.Session.AutoSaveInterval != nil {
			cfg.Session.AutoSaveInterval = *user.Session.AutoSaveInterval
		}
	}
	if user.Agent != nil {
		if user.Agent.Enabled != nil {
			cfg.Agent.Enabled = *user.Agent.Enabled
		}
		if user.Agent.Command != nil {
			cfg.Agent.Command = *user.Agent.Command
		}
		if user.Agent.Args != nil {
			cfg.Agent.Args = user.Agent.Args
		}
	}
}

// Validate validates the configuration and returns an error if invalid.
func (c Config) Validate() error {
	// Validate editor config
	if c.Editor.TabSize < 1 || c.Editor.TabSize > 8 {
		return fmt.Errorf("tab_size must be between 1 and 8, got %d", c.Editor.TabSize)
	}

	// Validate theme - check against known valid themes
	validThemes := map[string]bool{
		"nord":         true,
		"dracula":      true,
		"catppuccin":   true,
		"solarized-dark": true,
		"one-dark":     true,
	}
	if c.UI.Theme != "" {
		if !validThemes[c.UI.Theme] {
			return fmt.Errorf("unknown theme: %q", c.UI.Theme)
		}
	}

	// Validate agent config
	if c.Agent.Enabled && c.Agent.Command == "" {
		return fmt.Errorf("agent.enabled is true but agent.command is empty")
	}

	// Validate session config
	if c.Session.Enabled && c.Session.AutoSaveInterval <= 0 {
		return fmt.Errorf("session.auto_save_interval must be positive when session is enabled")
	}

	// Validate LSP configs
	for i, lsp := range c.LSP {
		if lsp.Command == "" {
			return fmt.Errorf("lsp[%d].command is empty", i)
		}
		if len(lsp.Extensions) == 0 {
			return fmt.Errorf("lsp[%d].extensions is empty", i)
		}
	}

	return nil
}
