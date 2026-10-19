package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"teak/internal/toolpath"
)

func TestConfigReloadAppliesToolOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { toolpath.Configure(nil) })

	toolName := "teak-reload-tool"
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	toolPath := filepath.Join(t.TempDir(), toolName)
	if err := os.WriteFile(toolPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newViewTestModel(t, true)
	cfg := m.appCfg
	cfg.Tools = map[string]string{"teak-reload-tool": toolPath}
	updated, _ := m.handleConfigReloaded(configReloadedMsg{Generation: m.configReloadGeneration, Config: cfg})
	m = updated.(Model)

	got, err := toolpath.Resolve("teak-reload-tool")
	if err != nil {
		t.Fatalf("reloaded tool override did not resolve: %v", err)
	}
	if got != filepath.Clean(toolPath) {
		t.Fatalf("Resolve() = %q after reload, want reloaded override %q", got, toolPath)
	}
}
