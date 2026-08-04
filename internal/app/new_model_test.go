package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/config"
	"teak/internal/session"
	"teak/internal/text"
)

func TestNewModelRejectsInvalidConfiguration(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.TabSize = 0

	model, err := NewModel("", t.TempDir(), cfg)
	if err == nil {
		model.cleanup()
		t.Fatal("NewModel() accepted an invalid configuration")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("NewModel() error = %v, want invalid config context", err)
	}
}

func TestNewModelPropagatesLSPEnvironment(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.LSP = []config.LSPConfig{{
		Extensions: []string{".fixture"},
		Command:    "fixture-lsp",
		LanguageID: "fixture",
		Env:        map[string]string{"TEAK_FIXTURE_MODE": "1"},
	}}
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)
	server := model.lspMgr.ConfigForFile("main.fixture")
	if server == nil || server.Env["TEAK_FIXTURE_MODE"] != "1" {
		t.Fatalf("interactive LSP config = %#v, want environment preserved", server)
	}
}

func TestNewModelWithFilesCreatesTabsAndStartupCursors(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModelWithFiles([]StartupFile{
		{Path: a, Line: 2, Col: 1},
		{Path: b, Line: -1},
	}, root, cfg)
	if err != nil {
		t.Fatalf("NewModelWithFiles: %v", err)
	}
	t.Cleanup(model.cleanup)

	if len(model.editors) != 2 || len(model.tabBar.Tabs) != 2 {
		t.Fatalf("tabs/editors = %d/%d, want 2", len(model.tabBar.Tabs), len(model.editors))
	}
	if model.editors[0].Buffer.FilePath != a || model.editors[1].Buffer.FilePath != b {
		t.Fatalf("paths = %q, %q", model.editors[0].Buffer.FilePath, model.editors[1].Buffer.FilePath)
	}
	if model.welcome != nil {
		t.Fatal("welcome should be nil when opening files")
	}
	if model.sessionRestoreEligible {
		t.Fatal("session restore should be disabled when Session.Enabled is false")
	}
	if len(model.startupFiles) != 2 {
		t.Fatalf("startupFiles = %d, want 2", len(model.startupFiles))
	}
	if _, ok := model.startupCursors[filepath.Clean(a)]; !ok {
		t.Fatal("expected startup cursor for first file")
	}

	// Simulate async load completion for the first file.
	updated, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:     a,
		EditorID: model.editors[0].ID(),
		Snapshot: text.New([]byte("package main\n\nfunc main() {}\n")),
	})
	m := updated.(Model)
	ed := m.editors[0]
	if ed.Buffer.Cursor.Line != 2 {
		t.Fatalf("cursor line = %d, want 2", ed.Buffer.Cursor.Line)
	}
	if _, ok := m.startupCursors[filepath.Clean(a)]; ok {
		t.Fatal("startup cursor should be consumed after load")
	}
}

func TestNewModelWithFilesEnablesSessionRestoreWhenConfigured(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = true
	model, err := NewModelWithFiles([]StartupFile{{Path: path, Line: -1}}, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.cleanup)
	if !model.sessionRestoreEligible {
		t.Fatal("expected session restore to stay eligible when opening CLI files")
	}
}

func TestSessionRestoreMergesCLIStartupFile(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "session.go")
	cliPath := filepath.Join(root, "cli.go")
	for _, p := range []string{sessionPath, cliPath} {
		if err := os.WriteFile(p, []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = true
	model, err := NewModelWithFiles([]StartupFile{{Path: cliPath, Line: 0, Col: 0}}, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.cleanup)

	// Simulate a successful restore that only knew about session.go.
	cmd := model.restoreSessionFromPinnedRead(
		session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{
			{FilePath: sessionPath, CursorLine: 0, Pinned: true},
		}},
		[]restoredSessionFile{
			{Tab: session.TabState{FilePath: sessionPath, CursorLine: 0, Pinned: true}, Snapshot: text.New([]byte("package p\n"))},
		},
		nil,
	)
	if cmd == nil {
		t.Fatal("expected restore commands")
	}
	if len(model.editors) != 2 {
		t.Fatalf("editors = %d, want 2 (session + CLI)", len(model.editors))
	}
	// CLI file should be active.
	if model.activeEditor() == nil || model.activeEditor().Buffer.FilePath != cliPath {
		t.Fatalf("active = %q, want CLI file %q", model.activeEditor().Buffer.FilePath, cliPath)
	}
	paths := map[string]bool{}
	for _, ed := range model.editors {
		paths[ed.Buffer.FilePath] = true
	}
	if !paths[sessionPath] || !paths[cliPath] {
		t.Fatalf("paths = %v, want both session and CLI files", paths)
	}
}
