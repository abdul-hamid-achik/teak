package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
)

func TestEditorAutoClosesBracketAtEveryCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"a\nb",
		text.Position{Line: 0, Col: 1},
		text.Position{Line: 1, Col: 1},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "("})
	if got, want := ed.Buffer.Content(), "a()\nb()"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 2}, Head: text.Position{Line: 0, Col: 2}},
		{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
	})

	ed.Buffer.Undo()
	if got, want := ed.Buffer.Content(), "a\nb"; got != want {
		t.Fatalf("one undo content = %q, want %q", got, want)
	}
}

func TestEditorAutoCloseWrapsEverySelectedRange(t *testing.T) {
	ed := newEditor("one\nred", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{}, Head: text.Position{Line: 0, Col: len("one")}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: len("red")}},
	}, 1)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "["})
	if got, want := ed.Buffer.Content(), "[one]\n[red]"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	})
}

func TestEditorClosingBracketSkipsAndInsertsPerCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"()\n[]",
		text.Position{Line: 0, Col: 1},
		text.Position{Line: 1, Col: 1},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: ")"})
	if got, want := ed.Buffer.Content(), "()\n[)]"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 2}, Head: text.Position{Line: 0, Col: 2}},
		{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
	})
}

func TestEditorClosingBracketSkipDoesNotEditDocument(t *testing.T) {
	ed := editorWithTwoCursors(
		"()\n()",
		text.Position{Line: 0, Col: 1},
		text.Position{Line: 1, Col: 1},
	)
	version := ed.Buffer.Version()

	ed, _ = ed.Update(tea.KeyPressMsg{Text: ")"})
	if got, want := ed.Buffer.Content(), "()\n()"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got := ed.Buffer.Version(); got != version {
		t.Fatalf("version = %d, want unchanged %d", got, version)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 2}, Head: text.Position{Line: 0, Col: 2}},
		{Anchor: text.Position{Line: 1, Col: 2}, Head: text.Position{Line: 1, Col: 2}},
	})
}

func TestEditorBackspaceHandlesPairsAndCharactersPerCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"()\nabc",
		text.Position{Line: 0, Col: 1},
		text.Position{Line: 1, Col: 2},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "backspace"})
	if got, want := ed.Buffer.Content(), "\nac"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	})
	ed.Buffer.Undo()
	if got, want := ed.Buffer.Content(), "()\nabc"; got != want {
		t.Fatalf("one undo content = %q, want %q", got, want)
	}
}

func TestEditorBackspaceDeletesEveryEmptyPair(t *testing.T) {
	ed := editorWithTwoCursors(
		"()\n[]",
		text.Position{Line: 0, Col: 1},
		text.Position{Line: 1, Col: 1},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "backspace"})
	if got, want := ed.Buffer.Content(), "\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	})
}

func TestEditorBackspaceHandlesSelectedRangesBesidePairs(t *testing.T) {
	ed := newEditor("word\n()", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{}, Head: text.Position{Line: 0, Col: len("word")}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	}, 1)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "backspace"})
	if got, want := ed.Buffer.Content(), "\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	})
}

func TestEditorMultiCursorBackspaceRemovesWholeUTF8Runes(t *testing.T) {
	ed := editorWithTwoCursors(
		"éx\n🙂y",
		text.Position{Line: 0, Col: len("é")},
		text.Position{Line: 1, Col: len("🙂")},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "backspace"})
	if got, want := ed.Buffer.Content(), "x\ny"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 0}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	})
}

func TestEditorMultiCursorBackspacePreservesDocumentStartCursor(t *testing.T) {
	ed := editorWithTwoCursors(
		"a\n()",
		text.Position{},
		text.Position{Line: 1, Col: 1},
	)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "backspace"})
	if got, want := ed.Buffer.Content(), "a\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	requireEditorSelections(t, ed, []text.Selection{
		{Anchor: text.Position{}, Head: text.Position{}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	})
}
