package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/editor/overlays"
	"teak/internal/text"
)

func editorWithTwoCursors(content string, first, second text.Position) Editor {
	ed := newEditor(content, first.Line, first.Col)
	ed.Buffer.Selections.Add(text.Selection{Anchor: second, Head: second})
	ed.Buffer.Selections.Normalize()
	ed.Buffer.SetCursor(ed.Buffer.Selections.PrimaryCursor())
	return ed
}

func requireEditorSelections(t *testing.T, ed Editor, want []text.Selection) {
	t.Helper()
	got := ed.Buffer.Selections.All()
	if len(got) != len(want) {
		t.Fatalf("selection count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selection %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestEditorCtrlWordNavigationMovesEveryCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"one two\nthree four",
		text.Position{Line: 0, Col: 0},
		text.Position{Line: 1, Col: 0},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+right"})
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: len("one ")}, Head: text.Position{Line: 0, Col: len("one ")}},
		{Anchor: text.Position{Line: 1, Col: len("three ")}, Head: text.Position{Line: 1, Col: len("three ")}},
	})
}

func TestEditorCtrlShiftWordNavigationExtendsEveryCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"one two\nthree four",
		text.Position{Line: 0, Col: 0},
		text.Position{Line: 1, Col: 0},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+shift+right"})
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: len("one ")}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: len("three ")}},
	})
}

func TestEditorHomeEndNavigationMovesEveryCursor(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []text.Selection
	}{
		{
			name: "home",
			key:  "home",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
				{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
			},
		},
		{
			name: "end",
			key:  "end",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: len("one")}, Head: text.Position{Line: 0, Col: len("one")}},
				{Anchor: text.Position{Line: 1, Col: len("three")}, Head: text.Position{Line: 1, Col: len("three")}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := editorWithTwoCursors(
				"one\nthree",
				text.Position{Line: 0, Col: 1},
				text.Position{Line: 1, Col: 2},
			)
			ed, _ = ed.Update(tea.KeyPressMsg{Text: tt.key})
			requireEditorSelections(t, ed, tt.want)
		})
	}
}

func TestEditorShiftHomeEndExtendsEveryCursor(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []text.Selection
	}{
		{
			name: "shift home",
			key:  "shift+home",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 0}},
				{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 0}},
			},
		},
		{
			name: "shift end",
			key:  "shift+end",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: len("one")}},
				{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: len("three")}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := editorWithTwoCursors(
				"one\nthree",
				text.Position{Line: 0, Col: 1},
				text.Position{Line: 1, Col: 2},
			)
			ed, _ = ed.Update(tea.KeyPressMsg{Text: tt.key})
			requireEditorSelections(t, ed, tt.want)
		})
	}
}

// Both heads land on the same document bound, so the nested ranges coalesce
// per the selection invariants and the first cursor's range survives. The
// bug this guards against: ExtendSelection collapsed to the primary's range
// alone (here the second cursor's), which for ctrl+shift+end silently dropped
// the first cursor's anchor from the selection.
func TestEditorCtrlShiftHomeEndExtendsEveryCursor(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []text.Selection
	}{
		{
			name: "ctrl shift home",
			key:  "ctrl+shift+home",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 0}},
			},
		},
		{
			name: "ctrl shift end",
			key:  "ctrl+shift+end",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 1, Col: len("three")}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := editorWithTwoCursors(
				"one\nthree",
				text.Position{Line: 0, Col: 1},
				text.Position{Line: 1, Col: 2},
			)
			ed, _ = ed.Update(tea.KeyPressMsg{Text: tt.key})
			requireEditorSelections(t, ed, tt.want)
		})
	}
}

func TestEditorPageNavigationMovesEveryCursor(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		first  text.Position
		second text.Position
		want   []text.Selection
	}{
		{
			name:   "page down",
			key:    "pgdown",
			first:  text.Position{Line: 0, Col: 1},
			second: text.Position{Line: 1, Col: 1},
			want: []text.Selection{
				{Anchor: text.Position{Line: 2, Col: 1}, Head: text.Position{Line: 2, Col: 1}},
				{Anchor: text.Position{Line: 3, Col: 1}, Head: text.Position{Line: 3, Col: 1}},
			},
		},
		{
			name:   "page up",
			key:    "pgup",
			first:  text.Position{Line: 2, Col: 1},
			second: text.Position{Line: 3, Col: 1},
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
				{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := editorWithTwoCursors("aa\nbb\ncc\ndd\nee", tt.first, tt.second)
			ed.Viewport.Height = 2
			ed, _ = ed.Update(tea.KeyPressMsg{Text: tt.key})
			requireEditorSelections(t, ed, tt.want)
		})
	}
}

func TestEditorCtrlDeleteWordEditsEveryCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"one two\nred blue",
		text.Position{Line: 0, Col: 0},
		text.Position{Line: 1, Col: 0},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+delete"})
	if got, want := editorContent(ed), "two\nblue"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	})
}

func TestEditorHorizontalNavigationCollapsesSelectionsToDirectionalEdges(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []text.Selection
	}{
		{
			name: "left",
			key:  "left",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
				{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
			},
		},
		{
			name: "right",
			key:  "right",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 3}, Head: text.Position{Line: 0, Col: 3}},
				{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
			},
		},
		{
			name: "word left",
			key:  "ctrl+left",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
				{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
			},
		},
		{
			name: "word right",
			key:  "ctrl+right",
			want: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 3}, Head: text.Position{Line: 0, Col: 3}},
				{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newEditor("abcd\nefgh", 0, 0)
			ed.Buffer.RestoreSelections([]text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 3}},
				{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 0}},
			}, 1)

			ed, _ = ed.Update(tea.KeyPressMsg{Text: tt.key})
			requireEditorSelections(t, ed, tt.want)
		})
	}
}

func TestEditorHorizontalNavigationMovesCollapsedCursorsBesideSelections(t *testing.T) {
	ed := newEditor("abcd\nefgh", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 3}},
		{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
	}, 1)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "left"})
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	})
}

// editorWithCursors returns an editor with one collapsed caret per position.
func editorWithCursors(content string, positions ...text.Position) Editor {
	ed := newEditor(content, positions[0].Line, positions[0].Col)
	for _, p := range positions[1:] {
		ed.Buffer.Selections.Add(text.Selection{Anchor: p, Head: p})
	}
	ed.Buffer.Selections.Normalize()
	ed.Buffer.SetCursor(ed.Buffer.Selections.PrimaryCursor())
	return ed
}

// --- F7: Escape defuses armed secondary cursors ---

func TestEditorEscapeDropsSecondaryCursors(t *testing.T) {
	ed := editorWithCursors("one two\nthree four\nfive six",
		text.Position{Line: 0, Col: 0},
		text.Position{Line: 1, Col: 0},
		text.Position{Line: 2, Col: 0},
	)
	primary := ed.Buffer.Selections.PrimaryCursor()

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	requireEditorSelections(t, ed, []text.Selection{{Anchor: primary, Head: primary}})
	if ed.Buffer.Cursor != primary {
		t.Fatalf("cursor after escape = %+v, want primary cursor %+v", ed.Buffer.Cursor, primary)
	}
}

func TestEditorEscapeKeepsSingleSelection(t *testing.T) {
	ed := newEditor("hello world", 0, 0)
	ed.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 5}},
	})
}

func TestEditorEscapeClosesAutocompleteBeforeDroppingCursors(t *testing.T) {
	ed := editorWithCursors("fm\nsecond\nthird",
		text.Position{Line: 0, Col: 2},
		text.Position{Line: 1, Col: 0},
		text.Position{Line: 2, Col: 0},
	)
	ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "fmt", InsertText: "fmt"}})

	// First Escape only closes the popup: overlays win over cursor cleanup.
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})
	if ed.autocomplete.Visible {
		t.Fatal("escape left the autocomplete popup visible")
	}
	if got := ed.Buffer.Selections.Count(); got != 3 {
		t.Fatalf("cursor count after closing autocomplete = %d, want 3 (cursors must survive the first escape)", got)
	}

	// A second Escape, now without overlays, drops the secondary cursors.
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})
	if got := ed.Buffer.Selections.Count(); got != 1 {
		t.Fatalf("cursor count after second escape = %d, want 1", got)
	}
}

func TestEditorEscapeHidesFindBeforeDroppingCursors(t *testing.T) {
	ed := editorWithCursors("needle one\nneedle two",
		text.Position{Line: 0, Col: 0},
		text.Position{Line: 1, Col: 0},
		text.Position{Line: 1, Col: 8},
	)
	ed.ShowFind()

	// First Escape only closes the find widget; its origin restore returns the
	// multi-cursor state captured on open.
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})
	if ed.IsFindVisible() {
		t.Fatal("escape left the find widget visible")
	}
	if got := ed.Buffer.Selections.Count(); got != 3 {
		t.Fatalf("cursor count after closing find = %d, want 3 (cursors must survive the first escape)", got)
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})
	if got := ed.Buffer.Selections.Count(); got != 1 {
		t.Fatalf("cursor count after second escape = %d, want 1", got)
	}
}
