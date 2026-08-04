package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNamedSessionRoundTripAndWorkspaceIsolation(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := State{Version: 1, RootDir: root, ActiveTab: 0, Tabs: []TabState{{FilePath: "main.go"}}}

	if err := SaveNamed(state, "review"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadNamed(context.Background(), root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootDir != root || loaded.ActiveTab != 0 {
		t.Fatalf("loaded named state = %#v", loaded)
	}
	if _, err := LoadNamed(context.Background(), other, "review"); !os.IsNotExist(err) {
		t.Fatalf("LoadNamed(other) error = %v, want not-exist", err)
	}
	names, err := ListNamed(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "review" {
		t.Fatalf("ListNamed() = %#v, want [review]", names)
	}
}

func TestNamedSessionFallsBackToLegacyWorkspaceKey(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "project")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-link")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	state := State{Version: 1, RootDir: alias, ActiveTab: 0, Tabs: []TabState{{FilePath: "main.go"}}}
	legacyPath := filepath.Join(StateHome(), "sessions", legacyRootKey(alias), "named", "review.json")
	if err := saveToPath(state, legacyPath); err != nil {
		t.Fatal(err)
	}

	names, err := ListNamed(alias)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "review" {
		t.Fatalf("ListNamed(real root) = %#v, want legacy review session", names)
	}
	loaded, err := LoadNamed(context.Background(), alias, "review")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootDir != alias || len(loaded.Tabs) != 1 {
		t.Fatalf("LoadNamed(real root) = %#v, want legacy session", loaded)
	}
}

func TestNamedSessionRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escape", `nested\\escape`, "review/next"} {
		if _, err := NamedPath(root, name); err == nil {
			t.Errorf("NamedPath(%q) accepted unsafe name", name)
		}
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "escape.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected fixture escape file: %v", err)
	}
}

func TestCheckNamedReportsStaleTabsAndWorkspaceEscapes(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "main.go")
	if err := os.WriteFile(existing, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs: []TabState{
			{FilePath: existing},
			{FilePath: filepath.Join(root, "deleted.go")},
			{FilePath: filepath.Join(filepath.Dir(root), "outside.go")},
		},
	}
	if err := SaveNamed(state, "review"); err != nil {
		t.Fatal(err)
	}

	health, err := CheckNamed(context.Background(), root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if health.State != "stale" || health.Tabs != 3 {
		t.Fatalf("health = %#v, want stale session with three tabs", health)
	}
	if len(health.Issues) != 2 {
		t.Fatalf("health issues = %#v, want missing and outside entries", health.Issues)
	}
	if health.Issues[0].State != "missing" || health.Issues[1].State != "outside" {
		t.Fatalf("health issues = %#v, want missing then outside", health.Issues)
	}
}

func TestRemoveNamedRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	path, err := NamedPath(root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte("not a session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveNamed(root, "review"); err == nil {
		t.Fatal("RemoveNamed accepted a symlink")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was affected: %v", err)
	}
}
