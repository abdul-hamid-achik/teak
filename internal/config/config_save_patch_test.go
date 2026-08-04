package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveToPreservesCommentsAndUnknownSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# My annotated Teak config.
[editor]
tab_size = 2 # two spaces, on purpose
insert_tabs = false

# Custom tooling section the settings UI does not know about.
[mystery]
answer = 42

[ui]
theme = "nord"
show_tree = true
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Editor.TabSize = 8
	cfg.Editor.WordWrap = true
	cfg.UI.Theme = "dracula"

	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# My annotated Teak config.",
		"# two spaces, on purpose",
		"# Custom tooling section the settings UI does not know about.",
		"[mystery]",
		"answer = 42",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("saved config lost %q:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "tab_size = 8") {
		t.Errorf("tab_size not updated:\n%s", content)
	}
	if !strings.Contains(content, `theme = "dracula"`) {
		t.Errorf("theme not updated:\n%s", content)
	}
	if !strings.Contains(content, "word_wrap = true") {
		t.Errorf("word_wrap not added:\n%s", content)
	}

	// The result must still be valid TOML with the requested values.
	var loaded Config
	if err := tomlUnmarshal([]byte(content), &loaded); err != nil {
		t.Fatalf("saved config does not parse: %v", err)
	}
	if loaded.Editor.TabSize != 8 || !loaded.Editor.WordWrap || loaded.UI.Theme != "dracula" {
		t.Fatalf("loaded config = %+v, want the saved values", loaded)
	}
}

func TestSaveToCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := DefaultConfig()
	cfg.Editor.TabSize = 2

	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	var loaded Config
	if err := tomlUnmarshal(mustReadFile(t, path), &loaded); err != nil {
		t.Fatalf("saved config does not parse: %v", err)
	}
	if loaded.Editor.TabSize != 2 {
		t.Fatalf("tab_size = %d, want 2", loaded.Editor.TabSize)
	}
}

func TestSaveToBacksUpUnpatchableAnnotatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// A quoted key defeats the conservative patcher; the save must fall back
	// to a full rewrite but keep the annotated original in the backup.
	original := `# keep me
[editor]
"tab_size" = 2
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Editor.TabSize = 6
	outcome, err := SaveToWithOutcome(path, cfg)
	if err != nil {
		t.Fatalf("SaveToWithOutcome: %v", err)
	}
	if outcome != SavedWithBackup {
		t.Fatalf("outcome = %v, want SavedWithBackup", outcome)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want the untouched original", backup)
	}
	var loaded Config
	if err := tomlUnmarshal(mustReadFile(t, path), &loaded); err != nil {
		t.Fatalf("rewritten config does not parse: %v", err)
	}
	if loaded.Editor.TabSize != 6 {
		t.Fatalf("tab_size = %d, want 6", loaded.Editor.TabSize)
	}
}

func TestPatchConfigFilePreservesTrailingCommentOnManagedKey(t *testing.T) {
	existing := "[editor]\ntab_size = 2 # favorite number\n"
	cfg := DefaultConfig()
	cfg.Editor.TabSize = 4

	patched, ok := patchConfigFile(existing, cfg)
	if !ok {
		t.Fatal("patchConfigFile failed")
	}
	if !strings.Contains(patched, "tab_size = 4 # favorite number") {
		t.Fatalf("patched = %q, want the value replaced and the comment kept", patched)
	}
}

func TestPatchConfigFileRejectsAmbiguousInput(t *testing.T) {
	cfg := DefaultConfig()
	// A section header the parser cannot identify must abort the patch rather
	// than risk writing into the wrong scope.
	if _, ok := patchConfigFile("[editor\ntab_size = 2\n", cfg); ok {
		t.Fatal("patchConfigFile accepted a malformed section header")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return data
}
