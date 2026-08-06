package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/clipboard"
	"teak/internal/text"
)

func editorWithTwoSelectedWords() Editor {
	ed := newEditor("one two\nred blue", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: len("one")}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: len("red")}},
	}, 1)
	return ed
}

func TestEditorCopyMultipleSelectionsInDocumentOrder(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	ed := editorWithTwoSelectedWords()
	wantSelections := append([]text.Selection(nil), ed.Buffer.Selections.All()...)

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("multiselection copy did not prepare clipboard work")
	}
	prepared, ok := cmd().(ClipboardCopyPreparedMsg)
	if !ok {
		t.Fatalf("copy command = %T, want ClipboardCopyPreparedMsg", prepared)
	}
	if got, want := prepared.Content, "one\nred"; got != want {
		t.Fatalf("clipboard content = %q, want %q", got, want)
	}
	if got := updated.Buffer.Selections.All(); len(got) != len(wantSelections) || got[0] != wantSelections[0] || got[1] != wantSelections[1] {
		t.Fatalf("copy selections = %#v, want %#v", got, wantSelections)
	}
}

func TestEditorCopyUsesNonPrimarySelectionWhenPrimaryIsCollapsed(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	ed := newEditor("one two\nred blue", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{}, Head: text.Position{Line: 0, Col: len("one")}},
		{Anchor: text.Position{Line: 1, Col: len("red")}, Head: text.Position{Line: 1, Col: len("red")}},
	}, 1)

	_, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("copy ignored a non-primary selected range")
	}
	prepared := cmd().(ClipboardCopyPreparedMsg)
	if got, want := prepared.Content, "one"; got != want {
		t.Fatalf("clipboard content = %q, want %q", got, want)
	}
}

func TestEditorCutDeletesEverySelectionAtomically(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	ed := editorWithTwoSelectedWords()

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+x"})
	prepared := cmd().(ClipboardCopyPreparedMsg)
	if got, want := prepared.Content, "one\nred"; got != want {
		t.Fatalf("clipboard content = %q, want %q", got, want)
	}
	updated, followup := updated.Update(prepared)
	if followup == nil {
		t.Fatal("prepared cut did not schedule clipboard integration")
	}
	if got, want := updated.Buffer.Content(), " two\n blue"; got != want {
		t.Fatalf("cut content = %q, want %q", got, want)
	}
	if got := updated.Buffer.Selections.Count(); got != 2 {
		t.Fatalf("cut cursor count = %d, want 2", got)
	}
	updated.Buffer.Undo()
	if got, want := updated.Buffer.Content(), "one two\nred blue"; got != want {
		t.Fatalf("undo content = %q, want %q", got, want)
	}
}

func TestEditorCutPreservesCollapsedCursorsBesideSelectedRanges(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	ed := newEditor("one two\nred blue", 0, 0)
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{}, Head: text.Position{Line: 0, Col: len("one")}},
		{Anchor: text.Position{Line: 1, Col: len("red ")}, Head: text.Position{Line: 1, Col: len("red ")}},
	}, 1)

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+x"})
	updated, _ = updated.Update(cmd().(ClipboardCopyPreparedMsg))
	if got, want := updated.Buffer.Content(), " two\nred blue"; got != want {
		t.Fatalf("cut content = %q, want %q", got, want)
	}
	requireEditorSelections(t, updated, []text.Selection{
		{Anchor: text.Position{}, Head: text.Position{}},
		{Anchor: text.Position{Line: 1, Col: len("red ")}, Head: text.Position{Line: 1, Col: len("red ")}},
	})
}

func TestEditorCutRejectsChangedSecondarySelection(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	ed := editorWithTwoSelectedWords()

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+x"})
	prepared := cmd().(ClipboardCopyPreparedMsg)
	updated.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: len("one")}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: len("red")}},
	}, 1)

	updated, _ = updated.Update(prepared)
	if got, want := updated.Buffer.Content(), "one two\nred blue"; got != want {
		t.Fatalf("stale cut changed content to %q, want %q", got, want)
	}
}

func TestEditorCombinedClipboardLimitIncludesSeparators(t *testing.T) {
	content := strings.Repeat("x", clipboard.MaxClipboardBytes)
	ed := newEditor(content, 0, 0)
	middle := len(content) / 2
	ed.Buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{}, Head: text.Position{Line: 0, Col: middle}},
		{Anchor: text.Position{Line: 0, Col: middle}, Head: text.Position{Line: 0, Col: len(content)}},
	}, 1)

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("combined oversized copy did not report a limit")
	}
	msg, ok := cmd().(ClipboardOperationLimitMsg)
	if !ok || msg.Operation != "Copy" {
		t.Fatalf("limit message = %#v, want combined copy limit", msg)
	}
	if updated.Buffer.Rope() != ed.Buffer.Rope() {
		t.Fatal("combined oversized copy changed the document")
	}
}
