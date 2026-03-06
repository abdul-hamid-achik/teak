package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Editor.TabSize != 4 {
		t.Errorf("TabSize = %d, want 4", cfg.Editor.TabSize)
	}
	if cfg.Editor.InsertTabs {
		t.Error("InsertTabs should be false by default")
	}
	if !cfg.Editor.AutoIndent {
		t.Error("AutoIndent should be true by default")
	}
	if cfg.UI.Theme != "nord" {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, "nord")
	}
	if !cfg.UI.ShowTree {
		t.Error("ShowTree should be true by default")
	}
	if len(cfg.LSP) != 0 {
		t.Errorf("LSP should be empty by default, got %d", len(cfg.LSP))
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Should return defaults when no config file exists
	if cfg.Editor.TabSize != 4 {
		t.Errorf("TabSize = %d, want 4", cfg.Editor.TabSize)
	}
}

func TestLoadParsesToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[editor]
tab_size = 2
insert_tabs = true

[ui]
theme = "dracula"
show_tree = false

[[lsp]]
extensions = [".zig"]
command = "zls"
args = []
language_id = "zig"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Test the merge logic directly by parsing the TOML and merging
	cfg := DefaultConfig()
	var user userConfig

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := tomlUnmarshal(data, &user); err != nil {
		t.Fatal(err)
	}

	merge(&cfg, &user)

	if cfg.Editor.TabSize != 2 {
		t.Errorf("TabSize = %d, want 2", cfg.Editor.TabSize)
	}
	if !cfg.Editor.InsertTabs {
		t.Error("InsertTabs should be true")
	}
	if !cfg.Editor.AutoIndent {
		t.Error("AutoIndent should still be true (not overridden)")
	}
	if cfg.UI.Theme != "dracula" {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, "dracula")
	}
	if cfg.UI.ShowTree {
		t.Error("ShowTree should be false")
	}
	if len(cfg.LSP) != 1 {
		t.Fatalf("LSP len = %d, want 1", len(cfg.LSP))
	}
	if cfg.LSP[0].Command != "zls" {
		t.Errorf("LSP command = %q, want %q", cfg.LSP[0].Command, "zls")
	}
}

func TestMergePartialOverride(t *testing.T) {
	cfg := DefaultConfig()
	tabSize := 8
	user := userConfig{
		Editor: &userEditorConfig{
			TabSize: &tabSize,
		},
	}
	merge(&cfg, &user)

	if cfg.Editor.TabSize != 8 {
		t.Errorf("TabSize = %d, want 8", cfg.Editor.TabSize)
	}
	// Other fields should remain at defaults
	if cfg.Editor.InsertTabs {
		t.Error("InsertTabs should remain false")
	}
	if !cfg.Editor.AutoIndent {
		t.Error("AutoIndent should remain true")
	}
	if cfg.UI.Theme != "nord" {
		t.Errorf("Theme should remain %q", "nord")
	}
}

func TestDefaultConfigAgentDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Agent.Enabled {
		t.Error("Agent.Enabled should be true by default")
	}
	if cfg.Agent.Command != "opencode" {
		t.Errorf("Agent.Command = %q, want %q", cfg.Agent.Command, "opencode")
	}
	if len(cfg.Agent.Args) != 1 || cfg.Agent.Args[0] != "acp" {
		t.Errorf("Agent.Args = %v, want [acp]", cfg.Agent.Args)
	}
}

func TestMergeAgentConfig(t *testing.T) {
	cfg := DefaultConfig()
	enabled := false
	cmd := "custom-agent"
	user := userConfig{
		Agent: &userAgentConfig{
			Enabled: &enabled,
			Command: &cmd,
			Args:    []string{"--verbose"},
		},
	}
	merge(&cfg, &user)

	if cfg.Agent.Enabled {
		t.Error("Agent.Enabled should be false after merge")
	}
	if cfg.Agent.Command != "custom-agent" {
		t.Errorf("Agent.Command = %q, want %q", cfg.Agent.Command, "custom-agent")
	}
	if len(cfg.Agent.Args) != 1 || cfg.Agent.Args[0] != "--verbose" {
		t.Errorf("Agent.Args = %v, want [--verbose]", cfg.Agent.Args)
	}
}

func TestMergeEmptyUser(t *testing.T) {
	cfg := DefaultConfig()
	user := userConfig{}
	merge(&cfg, &user)

	// Everything should remain at defaults
	expected := DefaultConfig()
	if cfg.Editor.TabSize != expected.Editor.TabSize {
		t.Errorf("TabSize = %d, want %d", cfg.Editor.TabSize, expected.Editor.TabSize)
	}
	if cfg.Editor.InsertTabs != expected.Editor.InsertTabs {
		t.Errorf("InsertTabs = %v, want %v", cfg.Editor.InsertTabs, expected.Editor.InsertTabs)
	}
	if cfg.Editor.AutoIndent != expected.Editor.AutoIndent {
		t.Errorf("AutoIndent = %v, want %v", cfg.Editor.AutoIndent, expected.Editor.AutoIndent)
	}
	if cfg.UI.Theme != expected.UI.Theme {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, expected.UI.Theme)
	}
	if cfg.UI.ShowTree != expected.UI.ShowTree {
		t.Errorf("ShowTree = %v, want %v", cfg.UI.ShowTree, expected.UI.ShowTree)
	}
	if cfg.Agent.Enabled != expected.Agent.Enabled {
		t.Errorf("Agent.Enabled = %v, want %v", cfg.Agent.Enabled, expected.Agent.Enabled)
	}
}

func TestMergeUIOnly(t *testing.T) {
	cfg := DefaultConfig()
	theme := "catppuccin"
	showTree := false
	user := userConfig{
		UI: &userUIConfig{
			Theme:    &theme,
			ShowTree: &showTree,
		},
	}
	merge(&cfg, &user)

	if cfg.UI.Theme != "catppuccin" {
		t.Errorf("Theme = %q, want %q", cfg.UI.Theme, "catppuccin")
	}
	if cfg.UI.ShowTree {
		t.Error("ShowTree should be false")
	}
	// Editor should be untouched
	if cfg.Editor.TabSize != 4 {
		t.Errorf("TabSize = %d, want 4", cfg.Editor.TabSize)
	}
}

func TestMergeLSPOverride(t *testing.T) {
	cfg := DefaultConfig()
	user := userConfig{
		LSP: []LSPConfig{
			{Extensions: []string{".rs"}, Command: "rust-analyzer", LanguageID: "rust"},
			{Extensions: []string{".py"}, Command: "pyright", LanguageID: "python"},
		},
	}
	merge(&cfg, &user)

	if len(cfg.LSP) != 2 {
		t.Fatalf("LSP len = %d, want 2", len(cfg.LSP))
	}
	if cfg.LSP[0].Command != "rust-analyzer" {
		t.Errorf("LSP[0].Command = %q, want %q", cfg.LSP[0].Command, "rust-analyzer")
	}
	if cfg.LSP[1].LanguageID != "python" {
		t.Errorf("LSP[1].LanguageID = %q, want %q", cfg.LSP[1].LanguageID, "python")
	}
}

func TestMergeLSPEmptyDoesNotOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LSP = []LSPConfig{{Command: "original"}}
	user := userConfig{
		LSP: nil, // empty, should not override
	}
	merge(&cfg, &user)

	if len(cfg.LSP) != 1 || cfg.LSP[0].Command != "original" {
		t.Errorf("LSP should not be overridden by nil, got %v", cfg.LSP)
	}
}

func TestParseAgentTOML(t *testing.T) {
	content := `
[agent]
enabled = false
command = "my-agent"
args = ["serve", "--port", "8080"]
`
	cfg := DefaultConfig()
	var user userConfig
	if err := toml.Unmarshal([]byte(content), &user); err != nil {
		t.Fatal(err)
	}
	merge(&cfg, &user)

	if cfg.Agent.Enabled {
		t.Error("Agent.Enabled should be false")
	}
	if cfg.Agent.Command != "my-agent" {
		t.Errorf("Agent.Command = %q, want %q", cfg.Agent.Command, "my-agent")
	}
	if len(cfg.Agent.Args) != 3 || cfg.Agent.Args[2] != "8080" {
		t.Errorf("Agent.Args = %v, want [serve --port 8080]", cfg.Agent.Args)
	}
}

func TestParseEditorOnlyTOML(t *testing.T) {
	content := `
[editor]
tab_size = 2
`
	cfg := DefaultConfig()
	var user userConfig
	if err := toml.Unmarshal([]byte(content), &user); err != nil {
		t.Fatal(err)
	}
	merge(&cfg, &user)

	if cfg.Editor.TabSize != 2 {
		t.Errorf("TabSize = %d, want 2", cfg.Editor.TabSize)
	}
	// Defaults preserved
	if !cfg.Editor.AutoIndent {
		t.Error("AutoIndent should remain true")
	}
	if cfg.UI.Theme != "nord" {
		t.Errorf("Theme should remain %q, got %q", "nord", cfg.UI.Theme)
	}
}

func TestParseEmptyTOML(t *testing.T) {
	cfg := DefaultConfig()
	var user userConfig
	if err := toml.Unmarshal([]byte(""), &user); err != nil {
		t.Fatal(err)
	}
	merge(&cfg, &user)

	expected := DefaultConfig()
	if cfg.Editor.TabSize != expected.Editor.TabSize {
		t.Error("empty TOML should preserve all defaults")
	}
}

func TestParseMultipleLSP(t *testing.T) {
	content := `
[[lsp]]
extensions = [".go"]
command = "gopls"
language_id = "go"

[[lsp]]
extensions = [".ts", ".tsx"]
command = "typescript-language-server"
args = ["--stdio"]
language_id = "typescript"
`
	cfg := DefaultConfig()
	var user userConfig
	if err := toml.Unmarshal([]byte(content), &user); err != nil {
		t.Fatal(err)
	}
	merge(&cfg, &user)

	if len(cfg.LSP) != 2 {
		t.Fatalf("LSP len = %d, want 2", len(cfg.LSP))
	}
	if cfg.LSP[0].LanguageID != "go" {
		t.Errorf("LSP[0].LanguageID = %q, want %q", cfg.LSP[0].LanguageID, "go")
	}
	if len(cfg.LSP[1].Extensions) != 2 {
		t.Errorf("LSP[1].Extensions len = %d, want 2", len(cfg.LSP[1].Extensions))
	}
	if len(cfg.LSP[1].Args) != 1 || cfg.LSP[1].Args[0] != "--stdio" {
		t.Errorf("LSP[1].Args = %v, want [--stdio]", cfg.LSP[1].Args)
	}
}

// tomlUnmarshal wraps the TOML library for testing.
func tomlUnmarshal(data []byte, v any) error {
	return toml.Unmarshal(data, v)
}
