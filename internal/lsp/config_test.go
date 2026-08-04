package lsp

import (
	"testing"

	"teak/internal/toolpath"
)

func TestConfigForFileBlueprint(t *testing.T) {
	cfg := ConfigForFile("/tmp/service.bp")
	if cfg == nil {
		t.Fatal("ConfigForFile() returned nil for .bp file")
	}
	if cfg.Command != "bp" {
		t.Fatalf("Command = %q, want %q", cfg.Command, "bp")
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "lsp" {
		t.Fatalf("Args = %v, want [lsp]", cfg.Args)
	}
	if cfg.LanguageID != "blueprint" {
		t.Fatalf("LanguageID = %q, want %q", cfg.LanguageID, "blueprint")
	}
}

func TestDefaultLanguageServersHaveInstallationHints(t *testing.T) {
	seen := make(map[string]struct{})
	for _, cfg := range DefaultConfigs() {
		if _, ok := seen[cfg.Command]; ok {
			continue
		}
		seen[cfg.Command] = struct{}{}
		if hint := toolpath.Hint(cfg.Command); hint == "" {
			t.Errorf("default language server %q has no installation hint", cfg.Command)
		}
	}
}
