package session

import (
	"path/filepath"
	"testing"
)

func TestPathUsesAbsoluteXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if got, want := Path(), filepath.Join(dir, "teak", "session.json"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestPathIgnoresRelativeXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative")
	if got := Path(); !filepath.IsAbs(got) {
		t.Fatalf("Path() = %q, want an absolute path", got)
	}
}
