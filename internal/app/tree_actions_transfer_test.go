package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/lsp"
	"teak/internal/plugin"
	"teak/internal/text"
)

func TestTreeRenameIsAsyncAndConfined(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "before.go")
	if err := os.WriteFile(source, []byte("package before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	cmd := m.startTreeRename(source, "after.go")
	if cmd == nil {
		t.Fatal("rename returned no command")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("rename mutated disk before command execution: %v", err)
	}
	result := cmd().(treeActionResultMsg)
	if result.Err != nil || !result.Committed {
		t.Fatalf("rename result = %#v", result)
	}
	updatedAny, _ := m.Update(result)
	updated := updatedAny.(Model)
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after rename: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "after.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package before\n" {
		t.Fatalf("renamed content = %q", data)
	}
	if updated.status != "Renamed: after.go" {
		t.Fatalf("status = %q, want rename success", updated.status)
	}
}

func TestTreeCopyDirectoryPreservesContentsWithoutFollowingLinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "main.go"), []byte("package main\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	cmd := m.startTreeCopy(source, "src-copy")
	if cmd == nil {
		t.Fatal("copy returned no command")
	}
	result := cmd().(treeActionResultMsg)
	if result.Err != nil || !result.Committed {
		t.Fatalf("copy result = %#v", result)
	}
	updatedAny, _ := m.Update(result)
	_ = updatedAny.(Model)
	data, err := os.ReadFile(filepath.Join(root, "src-copy", "nested", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("copied content = %q", data)
	}
	mode, err := os.Stat(filepath.Join(root, "src-copy", "nested", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if mode.Mode().Perm() != 0o640 {
		t.Fatalf("copied mode = %v; want 0640", mode.Mode().Perm())
	}
}

func TestTreeMoveRelocatesOpenDirtyTab(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n// unsaved\n")
	source := filepath.Join(root, "active.go")
	destination := filepath.Join(root, "dest", "active.go")

	cmd := m.startTreeMove(source, filepath.Join(root, "dest"))
	msg := cmd().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("destination missing after move: %v", err)
	}
	if got := updated.activeEditor().Buffer.FilePath; got != destination {
		t.Fatalf("open buffer path = %q, want %q", got, destination)
	}
	if !updated.activeEditor().Buffer.Dirty() {
		t.Fatal("move cleared unsaved buffer state")
	}
	if updated.tabBar.FindTab(destination) < 0 {
		t.Fatalf("tab bar does not contain moved path: %#v", updated.tabBar.Tabs)
	}
}

func TestTreeMoveRelocatesDiagnosticsWithOpenTab(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n// local\n")
	source := filepath.Join(root, "active.go")
	destination := filepath.Join(root, "dest", "active.go")
	m = completeDiagnosticsForTest(t, m, lsp.DiagnosticsMsg{URI: lsp.FileURI(source), Diagnostics: []lsp.Diagnostic{{
		Severity: lsp.SeverityError,
		Message:  "before move",
	}}})

	msg := m.startTreeMove(source, filepath.Join(root, "dest"))().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	if generation := updated.diagnosticProjections.currentSnapshotGeneration(); generation != 0 {
		snapshotAny, _ := updated.Update(updated.diagnosticProjections.snapshotCmd(generation)())
		updated = snapshotAny.(Model)
	}

	if got := updated.fileDiagnostics[destination]; got != int(lsp.SeverityError) {
		t.Fatalf("destination file diagnostic severity = %d, want %d", got, lsp.SeverityError)
	}
	if _, ok := updated.fileDiagnostics[source]; ok {
		t.Fatalf("old file diagnostic still present: %#v", updated.fileDiagnostics)
	}
	if got := updated.tabBar.Tabs[updated.activeTab].DiagSeverity; got != int(lsp.SeverityError) {
		t.Fatalf("moved tab diagnostic severity = %d, want %d", got, lsp.SeverityError)
	}
	if problem := updated.problemsPanel.SelectedProblem(); problem == nil || problem.FilePath != destination {
		t.Fatalf("relocated problem = %#v, want destination path", problem)
	}
}

func TestTreeRenameAcrossExtensionsPreservesPreparedDiagnostics(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n")
	source := filepath.Join(root, "active.go")
	destination := filepath.Join(root, "active.txt")
	m = completeDiagnosticsForTest(t, m, lsp.DiagnosticsMsg{URI: lsp.FileURI(source), Diagnostics: []lsp.Diagnostic{{
		Range: lsp.DiagRange{
			Start: lsp.DiagPosition{Line: 0},
			End:   lsp.DiagPosition{Line: 0, Character: 7},
		},
		Severity: lsp.SeverityError,
		Message:  "survives lexer rebuild",
	}}})

	msg := m.startTreeRename(source, "active.txt")().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	if got := updated.activeEditor().Buffer.FilePath; got != destination {
		t.Fatalf("renamed editor path = %q, want %q", got, destination)
	}
	diagnostics := updated.activeEditor().DiagnosticsIntersecting(0, 0)
	if len(diagnostics) != 1 || diagnostics[0].Message != "survives lexer rebuild" {
		t.Fatalf("prepared diagnostics after lexer rebuild = %#v", diagnostics)
	}
}

func TestTreeRenameAcrossExtensionsPreservesPreparedPluginHighlights(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	index := addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n")
	source := filepath.Join(root, "active.go")
	if err := m.setPluginHighlightsForEditor(index, plugin.UIHighlightRequest{
		Namespace: 7,
		Highlights: []plugin.UIHighlight{{
			Line:       0,
			StartCol:   0,
			EndCol:     7,
			Foreground: "#88c0d0",
		}},
	}); err != nil {
		t.Fatalf("setPluginHighlightsForEditor() error = %v", err)
	}

	msg := m.startTreeRename(source, "active.txt")().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	ranges := updated.editors[index].PluginHighlightRanges()
	if len(ranges) != 1 || ranges[0].Namespace != 7 || ranges[0].Line != 0 {
		t.Fatalf("prepared plugin highlights after lexer rebuild = %#v", ranges)
	}
}

func TestTreeMoveRelocatesOpenTabsInsideDirectory(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(sourceDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	addDirtyEditor(t, &m, filepath.Join("src", "nested", "one.go"), "package one\n", "package one\n")
	addDirtyEditor(t, &m, filepath.Join("src", "two.go"), "package two\n", "package two\n// local\n")

	cmd := m.startTreeMove(sourceDir, filepath.Join(root, "dest"))
	msg := cmd().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	want := []string{
		filepath.Join(root, "dest", "src", "nested", "one.go"),
		filepath.Join(root, "dest", "src", "two.go"),
	}
	for i, path := range want {
		if got := updated.editors[i+1].Buffer.FilePath; got != path {
			t.Fatalf("editor %d path = %q, want %q", i+1, got, path)
		}
	}
}

func TestTreeMoveAllowsOpenTargetsAndRejectsOutsideDestinations(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n// local\n")
	active := filepath.Join(root, "active.go")
	cmd := m.startTreeMove(active, "dest")
	if cmd == nil {
		t.Fatal("move returned no command")
	}
	updated := completeTreeAction(t, m, cmd)
	if updated.activeEditor().Buffer.FilePath != filepath.Join(root, "dest", "active.go") {
		t.Fatalf("moved open tab path = %q, want destination", updated.activeEditor().Buffer.FilePath)
	}
	if !updated.activeEditor().Buffer.Dirty() {
		t.Fatal("moving an open tab unexpectedly cleared its dirty state")
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}

	outside := filepath.Join(root, "..", "outside")
	if cmd := updated.startTreeMove(filepath.Join(root, "missing.go"), outside); cmd != nil {
		t.Fatal("outside move unexpectedly returned a command")
	}
	if !strings.Contains(updated.status, "outside the workspace") {
		t.Fatalf("outside move status = %q", updated.status)
	}
}

func TestTreeMoveRejectsOpenFileWithSaveInFlight(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	idx := addDirtyEditor(t, &m, "active.go", "package active\n", "package active\n// local\n")
	source := filepath.Join(root, "active.go")
	m.pendingSaves[1] = pendingSaveRequest{EditorID: m.editors[idx].ID(), Path: source}

	if cmd := m.startTreeMove(source, filepath.Join(root, "dest")); cmd != nil {
		t.Fatal("move returned a command while save was in flight")
	}
	if !strings.Contains(m.status, "save in progress") {
		t.Fatalf("status = %q, want save-in-progress guard", m.status)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source was changed despite save guard: %v", err)
	}
}

func TestTreeMoveRestartsPendingLoadAtDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "active.go")
	if err := os.WriteFile(source, []byte("package active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	openedAny, loadCmd := m.openFilePinned(source)
	if loadCmd == nil {
		t.Fatal("open did not schedule a pending load")
	}
	m = openedAny.(Model)
	if len(m.pendingFileLoads) != 1 {
		t.Fatalf("pending loads before move = %d, want 1", len(m.pendingFileLoads))
	}
	m.setPendingCursor(source, text.Position{Line: 3, Col: 2})

	msg := m.startTreeRename(source, "active.txt")().(treeActionResultMsg)
	updatedAny, _ := m.Update(msg)
	updated := updatedAny.(Model)
	want := filepath.Join(root, "active.txt")
	var relocatedCursor *text.Position
	for requestID, request := range updated.pendingFileLoads {
		if request.Path != want {
			t.Fatalf("pending load %d path = %q, want %q", requestID, request.Path, want)
		}
		relocatedCursor = request.Cursor
	}
	if len(updated.pendingFileLoads) != 1 {
		t.Fatalf("pending loads after move = %d, want replacement load", len(updated.pendingFileLoads))
	}
	if relocatedCursor == nil || *relocatedCursor != (text.Position{Line: 3, Col: 2}) {
		t.Fatalf("pending navigation after move = %#v, want preserved position", relocatedCursor)
	}
}
