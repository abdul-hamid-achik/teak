package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/editor/overlays"
	"teak/internal/text"
)

// A no-op undo or redo must not run the edit epilogue: an empty stack leaves
// the buffer untouched, so hiding popups and invalidating highlight state for
// a stale change record would churn for nothing.
func TestNoOpUndoRedoSkipsEditEpilogue(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "undo with empty stack", key: "ctrl+z"},
		{name: "redo with empty stack", key: "ctrl+y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := tokenizedEditor(t, 40)
			ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})
			ed.ShowHover("func example()")

			updated, cmd := ed.Update(tea.KeyPressMsg{Text: tt.key})

			if !updated.hover.Visible {
				t.Error("hover hidden by an undo that changed nothing")
			}
			if updated.Highlighter.IsDirty() {
				t.Error("highlighter marked dirty by an undo that changed nothing")
			}
			if !updated.Highlighter.CoversRange(0, 40) {
				t.Error("highlight coverage dropped from an undo that changed nothing")
			}
			if cmd != nil {
				t.Error("undo with an empty stack scheduled retokenization work")
			}
		})
	}
}

func TestRealUndoStillRunsEditEpilogue(t *testing.T) {
	ed := tokenizedEditor(t, 40)
	ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})
	typed, _ := ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := string(typed.Buffer.Line(20)); got != "xfunc example() error { return nil }" {
		t.Fatalf("setup: typed line = %q", got)
	}
	typed.ShowHover("func example()")

	updated, cmd := typed.Update(tea.KeyPressMsg{Text: "ctrl+z"})

	if got := string(updated.Buffer.Line(20)); got != "func example() error { return nil }" {
		t.Fatalf("undo did not restore the line: %q", got)
	}
	if updated.hover.Visible {
		t.Error("real undo left the hover popup visible")
	}
	if !updated.Highlighter.IsDirty() {
		t.Error("real undo did not invalidate the highlight cache")
	}
	if cmd == nil {
		t.Error("real undo scheduled no retokenization")
	}
}

// An accepted completion with empty InsertText leaves the buffer untouched, so
// it must not invalidate the token cache against the previous edit's stale
// change record.
func TestApplyCompletionWithoutInsertKeepsHighlightClean(t *testing.T) {
	paths := []struct {
		name   string
		accept func(Editor) Editor
	}{
		{
			name: "keyboard enter",
			accept: func(e Editor) Editor {
				updated, _ := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				return updated
			},
		},
		{
			name: "mouse selection",
			accept: func(e Editor) Editor {
				e.AutocompleteSelectAt(0)
				return e
			},
		},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			ed := tokenizedEditor(t, 40)
			ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})
			ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "noop", InsertText: ""}})

			updated := path.accept(ed)

			if updated.Highlighter.IsDirty() {
				t.Error("empty completion marked the highlighter dirty")
			}
			if !updated.Highlighter.CoversRange(0, 40) {
				t.Error("empty completion dropped highlight coverage")
			}
			if got, want := updated.Buffer.Version(), ed.Buffer.Version(); got != want {
				t.Errorf("buffer version changed from %d to %d on an empty completion", want, got)
			}
		})
	}
}
