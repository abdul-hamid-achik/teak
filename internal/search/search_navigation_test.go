package search

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

// replaceRowFixture returns a text-mode search model with the replace row
// visible, the find input focused and three results on screen.
func replaceRowFixture() Model {
	m := New(ui.DefaultTheme(), "", ModeText)
	m.SetSize(100, 40)
	m.SetShowReplace(true)
	m.input.SetValue("needle")
	m.results = []Result{
		{FilePath: "a.go", Line: 1, Col: 0, Preview: "needle"},
		{FilePath: "b.go", Line: 2, Col: 0, Preview: "needle"},
		{FilePath: "c.go", Line: 3, Col: 0, Preview: "needle"},
	}
	return m
}

// With the replace row visible, up/down must move the result cursor rather
// than shuttle focus between the two inputs; focus cycling belongs to
// Tab/Shift+Tab. Otherwise keyboard users cannot reach results at all in
// Ctrl+H mode.
func TestUpDownMoveResultCursorWithReplaceRowVisible(t *testing.T) {
	m := replaceRowFixture()

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("down with replace row: cursor = %d, want 1 (result navigation)", m.cursor)
	}
	if m.focusedInput != 0 {
		t.Fatalf("down with replace row moved focus to input %d; down must not change focus", m.focusedInput)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("second down: cursor = %d, want 2", m.cursor)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("up with replace row: cursor = %d, want 1 (result navigation)", m.cursor)
	}
	if m.focusedInput != 0 {
		t.Fatalf("up with replace row moved focus to input %d; up must not change focus", m.focusedInput)
	}
}

func TestUpDownMoveResultCursorFromReplaceInput(t *testing.T) {
	m := replaceRowFixture()
	m.focusedInput = 1

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("down from replace input: cursor = %d, want 1", m.cursor)
	}
	if m.focusedInput != 1 {
		t.Fatalf("down from replace input stole focus to %d", m.focusedInput)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("up from replace input: cursor = %d, want 0", m.cursor)
	}
}

func TestTabAndShiftTabCycleInputFocusWithReplaceRow(t *testing.T) {
	m := replaceRowFixture()

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusedInput != 1 || cmd == nil {
		t.Fatalf("tab: focusedInput = %d, cmd = %v; want focus 1 and a focus command", m.focusedInput, cmd)
	}
	// Tab is directional: from the replace input it stays put instead of
	// cycling back; only Shift+Tab returns to the find input.
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusedInput != 1 {
		t.Fatalf("second tab: focusedInput = %d, want it to stay on 1 (forward cycling only)", m.focusedInput)
	}

	// Shift+Tab moves focus back and, like Tab, never toggles the
	// search mode while the replace row is on screen.
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focusedInput != 0 || cmd == nil {
		t.Fatalf("shift+tab: focusedInput = %d, cmd = %v; want focus 0 and a focus command", m.focusedInput, cmd)
	}
	if m.mode != ModeText {
		t.Fatalf("shift+tab with replace row toggled mode to %v", m.mode)
	}
}

func TestShiftTabWithoutReplaceRowLeavesModeAlone(t *testing.T) {
	m := New(ui.DefaultTheme(), "", ModeText)
	m.SetSize(100, 40)

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.mode != ModeText {
		t.Fatalf("shift+tab without replace row toggled mode to %v; only Tab switches mode", m.mode)
	}
}
