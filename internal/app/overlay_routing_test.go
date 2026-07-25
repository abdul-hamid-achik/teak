package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/editor"
)

// showEditorContextMenu opens the editor's context menu the way a user does,
// with a right click, so the test exercises the real path rather than a
// test-only hook.
func showEditorContextMenu(t *testing.T, m *Model) {
	t.Helper()
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("setup: expected an active editor")
	}
	updated, _ := ed.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 5, Y: 3})
	m.setEditor(m.activeTab, updated)
	if !m.activeEditor().IsContextMenuVisible() {
		t.Fatal("setup: right click did not open the editor context menu")
	}
}

// Global shortcuts used to run before the editor's context menu got a chance at
// the key, so F1 opened help while the menu stayed visible behind it — and the
// menu kept capturing input afterwards.
func TestEditorContextMenuTakesPrecedenceOverGlobalKeys(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"F1 (help)", tea.KeyPressMsg{Code: tea.KeyF1}},
		{"Ctrl+G (go to line)", tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}},
		{"Ctrl+B (sidebar)", tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			showEditorContextMenu(t, &m)

			updated, _, handled := m.handleKeyPressPrecedence(tc.key)
			if !handled {
				t.Fatal("key was not captured by the context menu")
			}
			if updated.showHelp {
				t.Error("help opened while the context menu was up")
			}
			if updated.goToLineMode {
				t.Error("go-to-line opened while the context menu was up")
			}
			// The editor dismisses its menu on any unrecognised key, which is the
			// behaviour we want reaching it.
			if updated.activeEditor() != nil && updated.activeEditor().IsContextMenuVisible() {
				t.Error("context menu is still visible after an unrelated key")
			}
		})
	}
}

// Problems, Debugger and Agent already returned focus to the editor on Escape.
// The file tree and git panel did not, so keyboard-only users could not leave
// them without Ctrl+B.
func TestEscapeReturnsFocusToEditorFromSidebar(t *testing.T) {
	tests := []struct {
		name  string
		focus FocusArea
	}{
		{"file tree", FocusTree},
		{"git panel", FocusGitPanel},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m.showTree = true
			m.setFocus(tc.focus)

			// Escape reaches the sidebar through focused-input routing, after the
			// modal precedence chain has declined it.
			updated, _, handled := m.routeFocusedInput(tea.KeyPressMsg{Code: tea.KeyEscape})
			if !handled {
				t.Fatal("Escape was not handled")
			}
			if updated.focus != FocusEditor {
				t.Errorf("focus = %v after Escape, want FocusEditor", updated.focus)
			}
		})
	}
}

func TestEscapeDoesNotStealFocusFromTheEditor(t *testing.T) {
	m := newTestModel(t)
	m.setFocus(FocusEditor)

	// Escape in the editor belongs to the editor (dismissing find, autocomplete,
	// selection); the sidebar rule must not divert focus.
	updated, _, _ := m.routeFocusedInput(tea.KeyPressMsg{Code: tea.KeyEscape})
	if updated.focus != FocusEditor {
		t.Errorf("focus = %v after Escape in the editor, want it unchanged", updated.focus)
	}
}

// editorInputCaptured is the single source of truth for whether a surface owns
// input. Mouse motion asked the same question with its own hand-written list,
// so a modal added to one and not the other kept dragging underneath it.
func TestInputCapturedCoversEveryModalSurface(t *testing.T) {
	tests := []struct {
		name string
		open func(*Model)
	}{
		{"help", func(m *Model) { m.showHelp = true }},
		{"search", func(m *Model) { m.showSearch = true }},
		{"settings", func(m *Model) { m.showSettings = true }},
		{"branch picker", func(m *Model) { m.showBranchPicker = true }},
		{"tree context menu", func(m *Model) {
			m.treeContextMenu.Show([]editor.ContextMenuItem{{Label: "New File"}}, 1, 1)
		}},
		{"git context menu", func(m *Model) {
			m.gitContextMenu.Show([]editor.ContextMenuItem{{Label: "Stage"}}, 1, 1)
		}},
		{"editor context menu", func(m *Model) { showEditorContextMenu(t, m) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			if m.editorInputCaptured() {
				t.Fatal("setup: nothing should be capturing input yet")
			}
			tc.open(&m)
			if !m.editorInputCaptured() {
				t.Error("editorInputCaptured() = false while this surface is open")
			}
		})
	}
}
