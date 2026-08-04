package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "teak"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "teak", "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadWarnsOnUnknownKeys(t *testing.T) {
	writeTestConfig(t, `
[editor]
tab_size = 4
# a typo: the real key is tab_size
tab_width = 2

[mystery]
answer = 42
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Editor.TabSize != 4 {
		t.Fatalf("tab_size = %d, want 4", cfg.Editor.TabSize)
	}

	joined := strings.Join(cfg.LoadWarnings, " | ")
	if !strings.Contains(joined, "tab_width") {
		t.Errorf("warnings = %q, want the typo'd editor.tab_width key flagged", joined)
	}
	if !strings.Contains(joined, "mystery") {
		t.Errorf("warnings = %q, want the unknown section flagged", joined)
	}
}

func TestLoadNoWarningsForKnownKeys(t *testing.T) {
	writeTestConfig(t, `
[editor]
tab_size = 2
insert_tabs = true
auto_indent = true
format_on_save = false
word_wrap = false
scroll_margin = 3

[ui]
theme = "nord"
show_tree = true
tree_width = 30

[session]
enabled = true
auto_save_interval = 30
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.LoadWarnings) != 0 {
		t.Fatalf("LoadWarnings = %v, want none for a fully known config", cfg.LoadWarnings)
	}
}
