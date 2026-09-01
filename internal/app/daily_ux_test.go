package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/search"
	"teak/internal/session"
	"teak/internal/text"
)

func TestParseGoToLineInput(t *testing.T) {
	line, col, err := parseGoToLineInput("12:4")
	if err != nil || line != 12 || col != 4 {
		t.Fatalf("parseGoToLineInput(12:4) = %d, %d, %v", line, col, err)
	}
	line, col, err = parseGoToLineInput("3")
	if err != nil || line != 3 || col != 1 {
		t.Fatalf("parseGoToLineInput(3) = %d, %d, %v", line, col, err)
	}
}

func TestPasteIntoSaveAsDoesNotEditDocument(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "open.go", "hello\n", "hello\n")
	model.activateTab(idx)
	model.saveAsMode = true
	setPrompt(&model.saveAsInput, &model.saveAsCursor, filepath.Join(model.rootDir, ""))
	before := model.editors[idx].Buffer.Content()

	updatedAny, _ := model.Update(tea.PasteMsg{Content: "renamed.go"})
	updated := updatedAny.(Model)
	if got := updated.editors[idx].Buffer.Content(); got != before {
		t.Fatalf("paste leaked into document: %q", got)
	}
	if !strings.HasSuffix(updated.saveAsInput, "renamed.go") {
		t.Fatalf("save-as input = %q, want pasted filename", updated.saveAsInput)
	}
}

func TestFileLoadErrorMarksMissingDisk(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "missing.go", "", "")
	ed := model.editors[idx]
	model.editors[idx].Buffer.SetDiskPresence(text.DiskAssumedSaved)
	updatedAny, _ := model.Update(FileLoadErrorMsg{
		Path:     ed.Buffer.FilePath,
		EditorID: ed.ID(),
		Err:      os.ErrNotExist,
	})
	updated := updatedAny.(Model)
	if updated.editors[idx].Buffer.DiskPresence() != text.DiskAbsent {
		t.Fatalf("missing file presence = %v, want DiskAbsent", updated.editors[idx].Buffer.DiskPresence())
	}
}

func TestFileLoadErrorMarksUnreadDisk(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "secret.go", "", "")
	ed := model.editors[idx]
	updatedAny, _ := model.Update(FileLoadErrorMsg{
		Path:     ed.Buffer.FilePath,
		EditorID: ed.ID(),
		Err:      errors.New("permission denied"),
	})
	updated := updatedAny.(Model)
	if updated.editors[idx].Buffer.DiskPresence() != text.DiskUnread {
		t.Fatalf("unread presence = %v, want DiskUnread", updated.editors[idx].Buffer.DiskPresence())
	}
}

func TestMissingDiskSaveUsesCreateExpectation(t *testing.T) {
	root := t.TempDir()
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	idx := addDirtyEditor(t, &model, "brand-new.go", "package main\n", "")
	model.editors[idx].Buffer.SetDiskPresence(text.DiskAbsent)
	cmd := model.beginSaveForTab(idx, false, false)
	if cmd == nil {
		t.Fatal("save of a missing new file produced no command")
	}
}

func TestUnreadDiskSaveIsBlocked(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "blocked.go", "stolen\n", "")
	model.editors[idx].Buffer.SetDiskPresence(text.DiskUnread)
	if cmd := model.beginSaveForTab(idx, false, false); cmd != nil {
		t.Fatal("unread-file save should be blocked")
	}
	if !strings.Contains(model.status, "Save As") {
		t.Fatalf("status = %q, want Save As hint", model.status)
	}
}

func TestBackgroundLSPErrorsDoNotClobberStatus(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.status = "Saved main.go"
	updatedAny, _ := model.Update(lsp.LspErrorMsg{Method: "textDocument/foldingRange", Message: "no views", Code: -32601})
	updated := updatedAny.(Model)
	if updated.status != "Saved main.go" {
		t.Fatalf("status overwritten by background LSP error: %q", updated.status)
	}
}

func TestJumpBackReturnsToPushedLocation(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "a.go", "package a\n", "package a\n")
	model.editors[idx].Buffer.SetCursor(text.Position{Line: 0, Col: 8})
	model.pushJump()
	model.editors[idx].Buffer.SetCursor(text.Position{Line: 0, Col: 0})
	updatedAny, _ := model.jumpBack()
	updated := updatedAny.(Model)
	if got := updated.editors[idx].Buffer.Cursor; got.Col != 8 {
		t.Fatalf("cursor after jump back = %+v, want col 8", got)
	}
}

func TestToggleWordWrapUpdatesEditors(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "wrap.go", "package wrap\n", "package wrap\n")
	if model.editors[idx].Config.WordWrap {
		t.Fatal("fixture unexpectedly started with wrap on")
	}
	model.toggleWordWrap()
	if !model.appCfg.Editor.WordWrap || !model.editors[idx].Config.WordWrap {
		t.Fatal("alt+z did not enable word wrap")
	}
	model.toggleWordWrap()
	if model.appCfg.Editor.WordWrap || model.editors[idx].Config.WordWrap {
		t.Fatal("alt+z did not disable word wrap")
	}
}

func TestSaveAsArrowsMoveCaret(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	setPrompt(&model.saveAsInput, &model.saveAsCursor, "ab.go")
	model.saveAsMode = true
	updatedAny, _ := model.handleSaveAsInput(tea.KeyPressMsg{Code: tea.KeyLeft})
	updated := updatedAny.(Model)
	updatedAny, _ = updated.handleSaveAsInput(tea.KeyPressMsg{Text: "X"})
	updated = updatedAny.(Model)
	if updated.saveAsInput != "ab.gXo" {
		t.Fatalf("save-as after left+insert = %q, want ab.gXo", updated.saveAsInput)
	}
}

func TestUniqueSearchResultPathsDedupes(t *testing.T) {
	paths := uniqueSearchResultPaths([]search.Result{
		{FilePath: "/a.go"},
		{FilePath: "/b.go"},
		{FilePath: "/a.go"},
		{FilePath: ""},
	})
	if len(paths) != 2 || paths[0] != "/a.go" || paths[1] != "/b.go" {
		t.Fatalf("unique paths = %#v", paths)
	}
}

func TestRecoveryPrepsCountsOversizedDrops(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "huge.go", "x\n", "x\n")
	model.editors[idx].Buffer.ReplaceRopeSnapshot(
		text.NewOwned(make([]byte, session.MaxRecoveryRecordBytes+1)),
		text.Position{},
	)
	preps, dropped := model.recoveryPrepsCounted()
	if len(preps) != 0 || dropped != 1 {
		t.Fatalf("preps=%d dropped=%d, want 0/1", len(preps), dropped)
	}
}

func TestCloseTabClearsDiagnostics(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "gone.go", "package gone\n", "package gone\n")
	path := model.editors[idx].Buffer.FilePath
	if model.coordinator == nil {
		t.Fatal("test model has no coordinator")
	}
	coord := model.coordinator.GetLSPCoordinator()
	if coord == nil {
		t.Fatal("no LSP coordinator")
	}
	coord.StorePreparedDiagnostics(path, []lsp.Diagnostic{{Message: "boom", Severity: 1}})
	updatedAny, _ := model.closeTab(idx)
	updated := updatedAny.(Model)
	if diags := updated.coordinator.GetLSPCoordinator().GetDiagnostics(path); len(diags) != 0 {
		t.Fatalf("diagnostics survived close: %+v", diags)
	}
}

func TestConfigReloadAppliesEditorSettings(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "package main\n", "package main\n")
	cfg := model.appCfg
	cfg.Editor.WordWrap = true
	cfg.Editor.GitGutter = false
	cfg.Editor.RulerColumn = 80
	updatedAny, _ := model.Update(configReloadedMsg{Config: cfg})
	updated := updatedAny.(Model)
	if !updated.appCfg.Editor.WordWrap || updated.appCfg.Editor.GitGutter || updated.appCfg.Editor.RulerColumn != 80 {
		t.Fatalf("reloaded config not applied: %+v", updated.appCfg.Editor)
	}
	if !updated.editors[0].Config.WordWrap || updated.editors[0].Config.GitGutter {
		t.Fatalf("editor config not applied: %+v", updated.editors[0].Config)
	}
}

func TestConfigReloadKeepsOverlayEdits(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.showSettings = true
	for i := 0; i < 8 && !model.settingsM.Dirty(); i++ {
		model.settingsM.ToggleBoolValue()
		if !model.settingsM.Dirty() {
			model.settingsM.SelectNextSetting()
		}
	}
	if !model.settingsM.Dirty() {
		t.Fatal("expected dirty settings overlay")
	}
	before := model.appCfg.Editor.WordWrap
	cfg := model.appCfg
	cfg.Editor.WordWrap = !before
	updatedAny, _ := model.Update(configReloadedMsg{Config: cfg})
	updated := updatedAny.(Model)
	if updated.appCfg.Editor.WordWrap != before {
		t.Fatal("dirty settings overlay lost in-progress values to a disk reload")
	}
}

func TestToggleTerminalShowsPanel(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.width = 80
	model.height = 24
	updatedAny, _ := model.Update(toggleTerminalMsg{})
	updated := updatedAny.(Model)
	if !updated.showTerminal {
		t.Fatal("terminal did not open")
	}
	if updated.focus != FocusTerminal {
		t.Fatalf("focus = %v, want FocusTerminal", updated.focus)
	}
	if updated.terminalPanelHeight() == 0 {
		t.Fatal("visible terminal reserved no rows")
	}
}

func TestGitGutterReadyAppliesToMatchingPath(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "package main\n", "package main\n")
	path := model.editors[0].Buffer.FilePath
	if path == "" {
		t.Fatal("expected a path-backed editor")
	}
	model.gitGutterGeneration = map[string]uint64{filepath.Clean(path): 3}
	updatedAny, _ := model.Update(gitGutterReadyMsg{
		Path:       filepath.Clean(path),
		Generation: 3,
		Marks:      map[int]editor.GitLineKind{0: editor.GitLineAdded},
	})
	updated := updatedAny.(Model)
	if updated.editors[0].GitLines[0] != editor.GitLineAdded {
		t.Fatalf("git marks = %#v", updated.editors[0].GitLines)
	}
}
