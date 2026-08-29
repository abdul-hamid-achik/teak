package app

import (
	"testing"
)

// The editor context menu routes its non-local actions here; Find, Format
// Document and Go to Line must reach the same handlers the keyboard chords
// use.
func TestContextMenuActionFindOpensEditorFindWidget(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")

	if m.activeEditor().IsFindVisible() {
		t.Fatal("find widget already visible before the action")
	}

	updatedAny, _ := m.handleContextMenuAction("find")
	m = updatedAny.(Model)
	if !m.activeEditor().IsFindVisible() {
		t.Fatal("find action did not open the in-buffer find widget")
	}
}

func TestContextMenuActionGoToLineOpensDialog(t *testing.T) {
	m := newInputRoutingTestModel(t)

	updatedAny, _ := m.handleContextMenuAction("go_to_line")
	m = updatedAny.(Model)
	if !m.goToLineMode {
		t.Fatal("go_to_line action did not open the go-to-line dialog")
	}
	if m.goToLineInput != "" {
		t.Fatalf("go-to-line input = %q, want it reset", m.goToLineInput)
	}
}

func TestContextMenuActionFormatDocumentRequestsFormatting(t *testing.T) {
	m := newInputRoutingTestModel(t)

	// Without a file-backed editor there is nothing to format.
	updatedAny, cmd := m.handleContextMenuAction("format_document")
	m = updatedAny.(Model)
	if cmd != nil {
		t.Fatal("format action emitted a command without a file-backed editor")
	}

	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	_, cmd = m.handleContextMenuAction("format_document")
	if cmd == nil {
		t.Fatal("format action did not emit a formatting request for a file-backed editor")
	}
}

// The mouse path routes app-level menu actions straight to
// handleContextMenuAction instead of reconciling an editor edit.
func TestContextMenuAppRoutedActionsCovered(t *testing.T) {
	for _, action := range []string{"find", "go_to_line", "format_document"} {
		if !isAppRoutedContextAction(action) {
			t.Errorf("action %q missing from the app-routed context menu dispatch set", action)
		}
	}
	for _, action := range []string{"goto_definition", "find_references", "rename_symbol"} {
		if !isAppRoutedContextAction(action) {
			t.Errorf("existing app-routed action %q missing from dispatch set", action)
		}
	}
	for _, action := range []string{"cut", "copy", "paste", "select_all", "undo", "redo", "toggle_comment"} {
		if isAppRoutedContextAction(action) {
			t.Errorf("editor-local action %q must not be app-routed", action)
		}
	}
}

// Dispatching an unknown action must remain a no-op, matching the keyboard
// path for editor.ContextMenuActionMsg.
func TestContextMenuActionUnknownIsNoOp(t *testing.T) {
	m := newInputRoutingTestModel(t)

	updatedAny, cmd := m.handleContextMenuAction("does_not_exist")
	if cmd != nil {
		t.Fatal("unknown context menu action emitted a command")
	}
	updated, ok := updatedAny.(Model)
	if !ok {
		t.Fatalf("handleContextMenuAction returned %T, want Model", updatedAny)
	}
	if updated.status != m.status {
		t.Fatalf("unknown action changed the status message: %q -> %q", m.status, updated.status)
	}
}
