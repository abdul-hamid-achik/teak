package text

import "testing"

// primaryHead returns the head of the primary selection.
func primaryHead(b *Buffer) Position {
	return b.Selections.Primary().Head
}

// InsertAtCursor edits at Cursor, so it must report the range it actually
// touched. Deriving the record from the primary selection meant that any
// desynchronised cursor produced a change record pointing somewhere else —
// which is then sent verbatim to the language server as an incremental edit.
func TestLastChangeRecordsWhereTheEditHappened(t *testing.T) {
	buf := NewBufferFromBytes([]byte("l0\nl1\nl2\nl3\nl4"))

	// Reproduce the desync that a cursor jump leaves behind: the cursor moves
	// but the selection stays where it was.
	buf.Cursor = Position{Line: 3, Col: 1}

	buf.InsertAtCursor([]byte("Z"))

	if got, want := string(buf.Line(3)), "lZ3"; got != want {
		t.Fatalf("line 3 = %q, want %q", got, want)
	}
	change := buf.LastChange()
	if change == nil {
		t.Fatal("LastChange() = nil after an insert")
	}
	if change.StartLine != 3 {
		t.Errorf("LastChange().StartLine = %d, want 3 — the line the edit landed on", change.StartLine)
	}
	if change.StartCol != 1 {
		t.Errorf("LastChange().StartCol = %d, want 1", change.StartCol)
	}
}

// Backspace and Delete both branch on whether the primary selection is empty,
// so a stale selection makes a later press delete a range the user has since
// moved away from — data loss unrelated to the change record.
func TestMutatorsLeaveCursorAndPrimarySelectionInSync(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Buffer)
	}{
		{"Backspace", func(b *Buffer) { b.Cursor = Position{Line: 1, Col: 2}; b.Backspace() }},
		{"InsertAtCursor at a collapsed cursor", func(b *Buffer) { b.InsertAtCursor([]byte("x")) }},
		{"InsertAtCursor replacing a selection", func(b *Buffer) {
			b.SetSelection(Position{Line: 0, Col: 1}, Position{Line: 0, Col: 3})
			b.InsertAtCursor([]byte("x"))
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte("alpha beta\ngamma delta\nepsilon"))
			tc.mutate(buf)

			if buf.Selections.Count() != 1 {
				t.Skipf("multi-cursor state (%d selections) is out of scope here", buf.Selections.Count())
			}
			if got, want := primaryHead(buf), buf.Cursor; got != want {
				t.Errorf("primary selection head = %+v, cursor = %+v; they must not drift apart", got, want)
			}
		})
	}
}

// Pure cursor movement can still leave the primary selection behind; the editor
// collapses it separately with ClearSelection. That desync is now cosmetic
// rather than corrupting, because the change record is taken from the cursor —
// which is exactly what this asserts.
func TestChangeRecordIsCorrectEvenWhenSelectionIsStale(t *testing.T) {
	buf := NewBufferFromBytes([]byte("alpha\nbeta\ngamma"))
	buf.MoveCursor(DirDown)
	buf.MoveCursor(DirRight)

	buf.InsertAtCursor([]byte("Z"))

	change := buf.LastChange()
	if change == nil {
		t.Fatal("LastChange() = nil")
	}
	if change.StartLine != 1 || change.StartCol != 1 {
		t.Errorf("LastChange() start = (%d,%d), want (1,1) where the edit landed",
			change.StartLine, change.StartCol)
	}
}

// Replacing a selection collapses it, so the cursor and selection must agree
// afterwards — otherwise the *next* keystroke records the wrong position.
func TestSelectionReplaceLeavesCursorAndSelectionInSync(t *testing.T) {
	buf := NewBufferFromBytes([]byte("abcdef"))
	buf.SetSelection(Position{Line: 0, Col: 1}, Position{Line: 0, Col: 3})

	buf.InsertAtCursor([]byte("X"))

	if got, want := string(buf.Line(0)), "aXdef"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if got, want := primaryHead(buf), buf.Cursor; got != want {
		t.Errorf("primary selection head = %+v, cursor = %+v after replacing a selection", got, want)
	}
}

// The sequence that makes this a data-correctness bug rather than a cosmetic
// one: an edit whose record is wrong is applied verbatim to the language
// server's mirror of the document, which then permanently disagrees with the
// buffer.
func TestConsecutiveEditsRecordConsistentRanges(t *testing.T) {
	buf := NewBufferFromBytes([]byte("abcdef"))
	buf.SetSelection(Position{Line: 0, Col: 1}, Position{Line: 0, Col: 3})
	buf.InsertAtCursor([]byte("X")) // replaces "bc" -> "aXdef"

	buf.InsertAtCursor([]byte("Y")) // types at the cursor -> "aXYdef"

	if got, want := string(buf.Line(0)), "aXYdef"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	change := buf.LastChange()
	if change == nil {
		t.Fatal("LastChange() = nil")
	}
	// Applying the recorded change to the previous content must reproduce the
	// current content; this is exactly what the LSP client does to its mirror.
	if got, want := change.StartCol, 2; got != want {
		t.Errorf("LastChange().StartCol = %d, want %d — where 'Y' was actually inserted", got, want)
	}
}

func TestCursorMutatorsKeepPrimarySelectionSynchronized(t *testing.T) {
	tests := []struct {
		name   string
		cursor Position
		mutate func(*Buffer)
	}{
		{name: "delete", cursor: Position{Line: 1, Col: 2}, mutate: func(b *Buffer) { b.Delete() }},
		{name: "dedent line", cursor: Position{Line: 0, Col: 4}, mutate: func(b *Buffer) { b.DedentLine(4) }},
		{name: "line start", cursor: Position{Line: 1, Col: 4}, mutate: func(b *Buffer) { b.CursorToLineStart() }},
		{name: "line end", cursor: Position{Line: 1, Col: 0}, mutate: func(b *Buffer) { b.CursorToLineEnd() }},
		{name: "document start", cursor: Position{Line: 2, Col: 3}, mutate: func(b *Buffer) { b.CursorToDocStart() }},
		{name: "document end", cursor: Position{Line: 0, Col: 0}, mutate: func(b *Buffer) { b.CursorToDocEnd() }},
		{name: "word left", cursor: Position{Line: 0, Col: 10}, mutate: func(b *Buffer) { b.MoveCursorWordLeft() }},
		{name: "word right", cursor: Position{Line: 0, Col: 2}, mutate: func(b *Buffer) { b.MoveCursorWordRight() }},
		{name: "delete word", cursor: Position{Line: 0, Col: 2}, mutate: func(b *Buffer) { b.DeleteWord() }},
		{name: "move line up", cursor: Position{Line: 1, Col: 2}, mutate: func(b *Buffer) { b.MoveLineUp() }},
		{name: "move line down", cursor: Position{Line: 0, Col: 2}, mutate: func(b *Buffer) { b.MoveLineDown() }},
		{name: "duplicate line down", cursor: Position{Line: 0, Col: 2}, mutate: func(b *Buffer) { b.DuplicateLineDown() }},
		{name: "delete line", cursor: Position{Line: 1, Col: 2}, mutate: func(b *Buffer) { b.DeleteLine() }},
		{name: "add cursor above", cursor: Position{Line: 1, Col: 2}, mutate: func(b *Buffer) { b.AddCursorAbove() }},
		{name: "add cursor below", cursor: Position{Line: 0, Col: 2}, mutate: func(b *Buffer) { b.AddCursorBelow() }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte("  alpha beta\n  gamma delta\n  omega\n"))
			// Exercise the compatibility shape that caused the original bug: a
			// caller changed Cursor without updating the selection set.
			buf.Cursor = tc.cursor
			tc.mutate(buf)

			if buf.Selections == nil || buf.Selections.Count() == 0 {
				t.Fatal("mutator removed all selections")
			}
			if got, want := buf.Selections.PrimaryCursor(), buf.Cursor; got != want {
				t.Errorf("primary cursor = %+v, Buffer.Cursor = %+v; invariant was broken", got, want)
			}
		})
	}
}

func TestSetCursorRepairsEmptySelectionContainer(t *testing.T) {
	buf := NewBuffer()
	buf.Selections = &Selections{}
	want := Position{Line: 0, Col: 0}
	buf.SetCursor(want)

	if got := buf.Selections.PrimaryCursor(); got != want {
		t.Fatalf("primary cursor = %+v, want %+v", got, want)
	}
}

func TestSetSelectionRepairsNilSelectionContainer(t *testing.T) {
	for name, selections := range map[string]*Selections{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte("abcdef"))
			buf.Selections = selections
			anchor := Position{Line: 0, Col: 1}
			head := Position{Line: 0, Col: 4}

			buf.SetSelection(anchor, head)

			if got := buf.Selections.Primary(); got != (Selection{Anchor: anchor, Head: head}) {
				t.Fatalf("primary selection = %+v, want anchor/head %+v/%+v", got, anchor, head)
			}
			if buf.Cursor != head {
				t.Fatalf("cursor = %+v, want %+v", buf.Cursor, head)
			}
		})
	}
}
