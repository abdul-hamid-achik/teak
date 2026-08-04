package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	maxConfigBytes        = 4 << 20
	maxLSPConfigs         = 128
	maxConfigStringSize   = 4 << 10
	maxConfigStringData   = 256 << 10
	maxConfigArgs         = 128
	maxToolOverrides      = 64
	maxLSPEnvEntries      = 64
	maxTotalLSPEnvEntries = 256
	maxLSPExtensions      = 64
	maxTotalLSPExtensions = 512
	maxTotalLSPArgs       = 1024
	// Keep the integer-to-duration conversion in the app bounded and useful:
	// longer intervals are indistinguishable from an on-demand session save.
	maxAutoSaveIntervalSeconds = 24 * 60 * 60
)

// Config holds all application configuration.
type Config struct {
	Editor  EditorConfig      `toml:"editor"`
	UI      UIConfig          `toml:"ui"`
	Tools   map[string]string `toml:"tools"`
	LSP     []LSPConfig       `toml:"lsp"`
	Agent   AgentConfig       `toml:"agent"`
	Session SessionConfig     `toml:"session"`

	// LoadWarnings lists issues detected while loading (unknown keys, etc.).
	// Never persisted.
	LoadWarnings []string `toml:"-"`
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
	Sandbox string   `toml:"sandbox"`
}

// EditorConfig holds editor-specific settings.
type EditorConfig struct {
	TabSize      int  `toml:"tab_size"`
	InsertTabs   bool `toml:"insert_tabs"`
	AutoIndent   bool `toml:"auto_indent"`
	FormatOnSave bool `toml:"format_on_save"`
	WordWrap     bool `toml:"word_wrap"`
	ScrollMargin int  `toml:"scroll_margin"`
}

// UIConfig holds UI-related settings.
type UIConfig struct {
	Theme     string `toml:"theme"`
	ShowTree  bool   `toml:"show_tree"`
	TreeWidth int    `toml:"tree_width"` // 0 keeps the default width
}

// LSPConfig describes how to launch a language server.
type LSPConfig struct {
	Extensions []string          `toml:"extensions"`
	Command    string            `toml:"command"`
	Args       []string          `toml:"args"`
	LanguageID string            `toml:"language_id"`
	Env        map[string]string `toml:"env"`
}

var knownThemes = []string{"nord", "dracula", "catppuccin", "solarized-dark", "one-dark"}

// KnownThemes returns the supported UI theme names in display order.
func KnownThemes() []string {
	return append([]string(nil), knownThemes...)
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Editor: EditorConfig{
			TabSize:      4,
			InsertTabs:   false,
			AutoIndent:   true,
			ScrollMargin: 2,
		},
		UI: UIConfig{
			Theme:    "nord",
			ShowTree: true,
		},
		Agent: AgentConfig{
			Enabled: true,
			Command: "opencode",
			Args:    []string{"acp"},
			Sandbox: "auto",
		},
		Session: SessionConfig{
			Enabled:          true,
			AutoSaveInterval: 30,
		},
	}
}

// configPath returns the path to the config file.
func configPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "teak", "config.toml")
	}
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
	data, err := readConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	var user userConfig
	meta, err := toml.Decode(string(data), &user)
	if err != nil {
		return cfg, err
	}
	// A typo'd key silently keeps the default value, which users read as "my
	// config isn't working". Surface every key the decoder did not consume.
	for _, key := range meta.Undecoded() {
		cfg.LoadWarnings = append(cfg.LoadWarnings, fmt.Sprintf("unknown config key %q", strings.Join(key, ".")))
	}

	merge(&cfg, &user)
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// Save writes cfg to the standard Teak configuration path. The file is
// validated before any filesystem mutation and is replaced atomically.
func Save(cfg Config) error {
	return SaveTo(configPath(), cfg)
}

func readConfig(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config path is not a regular file")
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("config file exceeds %d-byte limit", maxConfigBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("config file exceeds %d-byte limit", maxConfigBytes)
	}
	return data, nil
}

// userConfig mirrors Config but with pointer fields for merge detection.
type userConfig struct {
	Editor  *userEditorConfig  `toml:"editor"`
	UI      *userUIConfig      `toml:"ui"`
	Tools   map[string]string  `toml:"tools"`
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
	Sandbox *string  `toml:"sandbox"`
}

type userEditorConfig struct {
	TabSize      *int  `toml:"tab_size"`
	InsertTabs   *bool `toml:"insert_tabs"`
	AutoIndent   *bool `toml:"auto_indent"`
	FormatOnSave *bool `toml:"format_on_save"`
	WordWrap     *bool `toml:"word_wrap"`
	ScrollMargin *int  `toml:"scroll_margin"`
}

type userUIConfig struct {
	Theme     *string `toml:"theme"`
	ShowTree  *bool   `toml:"show_tree"`
	TreeWidth *int    `toml:"tree_width"`
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
		if user.Editor.ScrollMargin != nil {
			cfg.Editor.ScrollMargin = *user.Editor.ScrollMargin
		}
	}
	if user.UI != nil {
		if user.UI.Theme != nil {
			cfg.UI.Theme = *user.UI.Theme
		}
		if user.UI.ShowTree != nil {
			cfg.UI.ShowTree = *user.UI.ShowTree
		}
		if user.UI.TreeWidth != nil {
			cfg.UI.TreeWidth = *user.UI.TreeWidth
		}
	}
	if user.Tools != nil {
		cfg.Tools = make(map[string]string, len(user.Tools))
		for name, path := range user.Tools {
			cfg.Tools[name] = path
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
		if user.Agent.Sandbox != nil {
			cfg.Agent.Sandbox = *user.Agent.Sandbox
		}
	}
}

// Validate validates the configuration and returns an error if invalid.
func (c Config) Validate() error {
	// Validate editor config
	if c.Editor.TabSize < 1 || c.Editor.TabSize > 8 {
		return fmt.Errorf("tab_size must be between 1 and 8, got %d", c.Editor.TabSize)
	}
	if c.Editor.ScrollMargin < 0 || c.Editor.ScrollMargin > 50 {
		return fmt.Errorf("scroll_margin must be between 0 and 50, got %d", c.Editor.ScrollMargin)
	}

	// Validate theme - check against known valid themes
	if c.UI.Theme != "" {
		validTheme := false
		for _, theme := range knownThemes {
			if c.UI.Theme == theme {
				validTheme = true
				break
			}
		}
		if !validTheme {
			return fmt.Errorf("unknown theme: %q", c.UI.Theme)
		}
	}
	if c.UI.TreeWidth < 0 || c.UI.TreeWidth > 120 {
		return fmt.Errorf("tree_width must be between 0 and 120 (0 keeps the default), got %d", c.UI.TreeWidth)
	}

	// Validate agent config
	if err := validateCommand("agent.command", c.Agent.Command, c.Agent.Enabled); err != nil {
		return err
	}
	if c.Agent.Sandbox != "" && c.Agent.Sandbox != "off" && c.Agent.Sandbox != "auto" && c.Agent.Sandbox != "required" {
		return fmt.Errorf("agent.sandbox must be one of off, auto, or required")
	}
	if len(c.Agent.Args) > maxConfigArgs {
		return fmt.Errorf("agent.args exceeds %d entries", maxConfigArgs)
	}
	for i, arg := range c.Agent.Args {
		if err := validateConfigString(fmt.Sprintf("agent.args[%d]", i), arg); err != nil {
			return err
		}
	}

	// Validate session config
	if c.Session.Enabled {
		if c.Session.AutoSaveInterval <= 0 {
			return fmt.Errorf("session.auto_save_interval must be positive when session is enabled")
		}
		if c.Session.AutoSaveInterval > maxAutoSaveIntervalSeconds {
			return fmt.Errorf("session.auto_save_interval must not exceed %d seconds", maxAutoSaveIntervalSeconds)
		}
	}

	// Validate explicit external tool paths. The path itself may not exist yet:
	// doctor should be able to report a broken override and its remediation
	// instead of turning an install-in-progress into an unreadable config.
	if len(c.Tools) > maxToolOverrides {
		return fmt.Errorf("tools exceeds %d entries", maxToolOverrides)
	}
	for name, path := range c.Tools {
		field := fmt.Sprintf("tools[%q]", name)
		if name == "" || strings.TrimSpace(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
			return fmt.Errorf("%s name must be a non-empty tool name without path separators or surrounding whitespace", field)
		}
		if err := validateConfigString(field+" name", name); err != nil {
			return err
		}
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("%s path must not be empty or whitespace only", field)
		}
		if err := validateConfigString(field+" path", path); err != nil {
			return err
		}
	}

	// Validate LSP configs
	if len(c.LSP) > maxLSPConfigs {
		return fmt.Errorf("lsp exceeds %d entries", maxLSPConfigs)
	}
	seenExtensions := make(map[string]struct{})
	totalExtensions := 0
	totalArgs := 0
	totalEnvEntries := 0
	for i, lsp := range c.LSP {
		if len(lsp.Extensions) == 0 {
			return fmt.Errorf("lsp[%d].extensions is empty", i)
		}
		if err := validateCommand(fmt.Sprintf("lsp[%d].command", i), lsp.Command, true); err != nil {
			return err
		}
		if strings.TrimSpace(lsp.LanguageID) == "" {
			return fmt.Errorf("lsp[%d].language_id must not be empty", i)
		}
		if err := validateConfigString(fmt.Sprintf("lsp[%d].language_id", i), lsp.LanguageID); err != nil {
			return err
		}
		if len(lsp.Extensions) > maxLSPExtensions {
			return fmt.Errorf("lsp[%d].extensions exceeds %d entries", i, maxLSPExtensions)
		}
		totalExtensions += len(lsp.Extensions)
		if totalExtensions > maxTotalLSPExtensions {
			return fmt.Errorf("total lsp extensions exceeds %d entries", maxTotalLSPExtensions)
		}
		if len(lsp.Args) > maxConfigArgs {
			return fmt.Errorf("lsp[%d].args exceeds %d entries", i, maxConfigArgs)
		}
		totalArgs += len(lsp.Args)
		if totalArgs > maxTotalLSPArgs {
			return fmt.Errorf("total lsp args exceeds %d entries", maxTotalLSPArgs)
		}
		if len(lsp.Env) > maxLSPEnvEntries {
			return fmt.Errorf("lsp[%d].env exceeds %d entries", i, maxLSPEnvEntries)
		}
		totalEnvEntries += len(lsp.Env)
		if totalEnvEntries > maxTotalLSPEnvEntries {
			return fmt.Errorf("total lsp env exceeds %d entries", maxTotalLSPEnvEntries)
		}
		for j, extension := range lsp.Extensions {
			name := fmt.Sprintf("lsp[%d].extensions[%d]", i, j)
			if err := validateConfigString(name, extension); err != nil {
				return err
			}
			if !strings.HasPrefix(extension, ".") || len(extension) == 1 {
				return fmt.Errorf("%s must start with a non-empty dot extension", name)
			}
			if strings.TrimSpace(extension) != extension {
				return fmt.Errorf("%s must not contain leading or trailing whitespace", name)
			}
			if strings.ToLower(extension) != extension {
				return fmt.Errorf("%s must be lowercase", name)
			}
			if _, duplicate := seenExtensions[extension]; duplicate {
				return fmt.Errorf("duplicate extension %q in lsp[%d]", extension, i)
			}
			seenExtensions[extension] = struct{}{}
		}
		for j, arg := range lsp.Args {
			if err := validateConfigString(fmt.Sprintf("lsp[%d].args[%d]", i, j), arg); err != nil {
				return err
			}
		}
		for name, value := range lsp.Env {
			field := fmt.Sprintf("lsp[%d].env[%q]", i, name)
			if name == "" || strings.TrimSpace(name) != name || strings.ContainsRune(name, '=') {
				return fmt.Errorf("%s must be a non-empty environment name without '=' or surrounding whitespace", field)
			}
			if err := validateConfigString(field+" name", name); err != nil {
				return err
			}
			if err := validateConfigString(field, value); err != nil {
				return err
			}
		}
	}
	if totalConfigStringBytes(c) > maxConfigStringData {
		return fmt.Errorf("configuration string data exceeds %d bytes", maxConfigStringData)
	}

	return nil
}

func validateCommand(name, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is empty", name)
		}
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be whitespace only", name)
	}
	return validateConfigString(name, value)
}

func totalConfigStringBytes(c Config) int {
	total := len(c.UI.Theme) + len(c.Agent.Command)
	for name, path := range c.Tools {
		total += len(name) + len(path)
	}
	for _, arg := range c.Agent.Args {
		total += len(arg)
	}
	for _, lsp := range c.LSP {
		total += len(lsp.Command) + len(lsp.LanguageID)
		for _, extension := range lsp.Extensions {
			total += len(extension)
		}
		for _, arg := range lsp.Args {
			total += len(arg)
		}
		for name, value := range lsp.Env {
			total += len(name) + len(value)
		}
	}
	return total
}

func validateConfigString(name, value string) error {
	if len(value) > maxConfigStringSize {
		return fmt.Errorf("%s exceeds %d bytes", name, maxConfigStringSize)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	return nil
}
