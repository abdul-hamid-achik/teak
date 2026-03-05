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

// tomlUnmarshal wraps the TOML library for testing.
func tomlUnmarshal(data []byte, v any) error {
	return toml.Unmarshal(data, v)
}
