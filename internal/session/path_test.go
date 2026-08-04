package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateHomeUsesAbsoluteXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if got, want := StateHome(), filepath.Join(dir, "teak"); got != want {
		t.Fatalf("StateHome() = %q, want %q", got, want)
	}
}

func TestPathUsesAbsoluteXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if got, want := Path(), filepath.Join(dir, "teak", "session.json"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
	if got, want := LegacyPath(), Path(); got != want {
		t.Fatalf("LegacyPath() = %q, want %q", got, want)
	}
}

func TestPathIgnoresRelativeXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative")
	if got := Path(); !filepath.IsAbs(got) {
		t.Fatalf("Path() = %q, want an absolute path", got)
	}
}

func TestPathForRootIsPerWorkspaceUnderSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	rootA := filepath.Join(dir, "project-a")
	rootB := filepath.Join(dir, "project-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatal(err)
	}

	pathA := PathForRoot(rootA)
	pathB := PathForRoot(rootB)
	if pathA == pathB {
		t.Fatalf("expected distinct session paths, both %q", pathA)
	}
	if !strings.Contains(pathA, filepath.Join("teak", "sessions")) {
		t.Fatalf("PathForRoot = %q, want .../teak/sessions/...", pathA)
	}
	if filepath.Base(pathA) != "session.json" {
		t.Fatalf("base = %q, want session.json", filepath.Base(pathA))
	}
	// Same root spelling variants must map to the same key.
	if PathForRoot(rootA) != PathForRoot(rootA+string(filepath.Separator)) {
		t.Fatal("trailing separator changed session key")
	}
}

func TestPathForRootCanonicalizesSymlinkAliases(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	realRoot := filepath.Join(stateHome, "project")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(stateHome, "project-link")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if realPath, aliasPath := PathForRoot(realRoot), PathForRoot(alias); realPath != aliasPath {
		t.Fatalf("PathForRoot(real) = %q, alias = %q; want one durable workspace identity", realPath, aliasPath)
	}
}

func TestLoadContextForRootFallsBackToLegacyWorkspaceKey(t *testing.T) {
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
	legacyPath := filepath.Join(StateHome(), "sessions", legacyRootKey(alias), "session.json")
	if err := saveToPath(state, legacyPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadContextForRoot(context.Background(), alias)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootDir != alias || len(loaded.Tabs) != 1 {
		t.Fatalf("LoadContextForRoot(real root) = %#v, want legacy session", loaded)
	}
}

func TestSaveAndLoadArePerWorkspace(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	rootA := filepath.Join(xdg, "a")
	rootB := filepath.Join(xdg, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fileA := filepath.Join(rootA, "a.go")
	fileB := filepath.Join(rootB, "b.go")

	if err := Save(State{
		Version:   1,
		RootDir:   rootA,
		ActiveTab: 0,
		Tabs:      []TabState{{FilePath: fileA, CursorLine: 1}},
	}); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	if err := Save(State{
		Version:   1,
		RootDir:   rootB,
		ActiveTab: 0,
		Tabs:      []TabState{{FilePath: fileB, CursorLine: 2}},
	}); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	loadedA, err := LoadContextForRoot(context.Background(), rootA)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	loadedB, err := LoadContextForRoot(context.Background(), rootB)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	if len(loadedA.Tabs) != 1 || loadedA.Tabs[0].FilePath != fileA {
		t.Fatalf("A tabs = %+v", loadedA.Tabs)
	}
	if len(loadedB.Tabs) != 1 || loadedB.Tabs[0].FilePath != fileB {
		t.Fatalf("B tabs = %+v", loadedB.Tabs)
	}
}

func TestLoadContextForRootMigratesLegacySession(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	root := filepath.Join(xdg, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.go")
	// Write only the legacy global session file.
	if err := saveToPath(State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs:      []TabState{{FilePath: file}},
	}, LegacyPath()); err != nil {
		t.Fatalf("legacy save: %v", err)
	}

	loaded, err := LoadContextForRoot(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadContextForRoot: %v", err)
	}
	if len(loaded.Tabs) != 1 || loaded.Tabs[0].FilePath != file {
		t.Fatalf("migrated tabs = %+v", loaded.Tabs)
	}

	// A different root must not receive the legacy session.
	other := filepath.Join(xdg, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContextForRoot(context.Background(), other); !os.IsNotExist(err) {
		t.Fatalf("other root error = %v, want ErrNotExist", err)
	}
}
