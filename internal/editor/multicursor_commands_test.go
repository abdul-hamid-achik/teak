package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
