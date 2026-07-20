package config

import (
	"path/filepath"
	"testing"
)

func TestConfigPathUsesAbsoluteXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got, want := configPath(), filepath.Join(dir, "teak", "config.toml"); got != want {
		t.Fatalf("configPath() = %q, want %q", got, want)
	}
}

func TestConfigPathIgnoresRelativeXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if got := configPath(); !filepath.IsAbs(got) {
		t.Fatalf("configPath() = %q, want an absolute path", got)
	}
}
