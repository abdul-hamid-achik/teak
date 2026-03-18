package lsp

import "testing"

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
