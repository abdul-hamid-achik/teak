package text

import "testing"

func TestIndentLinesRebasesCursorAndSelections(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo\nbar\n"))
	b.Selections = NewSelections(Position{Line: 0, Col: 2})
	b.Selections.selections[0] = Selection{Anchor: Position{Line: 0, Col: 2}, Head: Position{Line: 1, Col: 3}}
	b.Selections.primary = 0
	b.Cursor = Position{Line: 1, Col: 3}

	b.IndentLines(4)

	if b.Content() != "    foo\n    bar\n" {
		t.Fatalf("content = %q", b.Content())
	}
	if b.Cursor != (Position{Line: 1, Col: 7}) {
		t.Errorf("cursor = %v, want {1 7}", b.Cursor)
	}
	sel := b.Selections.Primary()
	start, end := sel.Ordered()
	if start != (Position{Line: 0, Col: 6}) || end != (Position{Line: 1, Col: 7}) {
		t.Errorf("selection = %v..%v, want {0 6}..{1 7}", start, end)
	}
}

func TestIndentLinesKeepsCursorAtLineStart(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo\n"))
	b.SetCursor(Position{Line: 0, Col: 0})

	b.IndentLines(4)

	if b.Cursor != (Position{Line: 0, Col: 0}) {
		t.Errorf("cursor = %v, want {0 0}", b.Cursor)
	}
}

func TestDedentLinesRebasesCursorAndSelections(t *testing.T) {
	b := NewBufferFromBytes([]byte("        foo\n    bar\n"))

	// A selection spanning both lines: both endpoints must follow their text
	// when each line loses a different amount of indentation.
	b.Selections = NewSelections(Position{Line: 0, Col: 8})
	b.Selections.selections[0] = Selection{Anchor: Position{Line: 0, Col: 8}, Head: Position{Line: 1, Col: 6}}
	b.Selections.primary = 0
	b.Cursor = Position{Line: 1, Col: 6}

	b.DedentLines(4)

	if b.Content() != "    foo\nbar\n" {
		t.Fatalf("content = %q", b.Content())
	}
	if b.Cursor != (Position{Line: 1, Col: 2}) {
		t.Errorf("buffer cursor = %v, want {1 2}", b.Cursor)
	}
	sel := b.Selections.Primary()
	start, end := sel.Ordered()
	if start != (Position{Line: 0, Col: 4}) || end != (Position{Line: 1, Col: 2}) {
		t.Errorf("selection = %v..%v, want {0 4}..{1 2}", start, end)
	}
}

func TestDedentLinesClampsCursorIntoShortenedLine(t *testing.T) {
	b := NewBufferFromBytes([]byte("  x\n"))
	b.SetCursor(Position{Line: 0, Col: 3})

	b.DedentLines(4)

	if b.Content() != "x\n" {
		t.Fatalf("content = %q", b.Content())
	}
	if b.Cursor != (Position{Line: 0, Col: 1}) {
		t.Errorf("cursor = %v, want {0 1}", b.Cursor)
	}
}

func TestToggleLineCommentRebasesCursorWhenCommenting(t *testing.T) {
	b := NewBufferFromBytes([]byte("  foo\n"))
	b.SetCursor(Position{Line: 0, Col: 4})

	b.ToggleLineComment("//")

	if b.Content() != "  // foo\n" {
		t.Fatalf("content = %q", b.Content())
	}
	if b.Cursor != (Position{Line: 0, Col: 7}) {
		t.Errorf("cursor = %v, want {0 7}", b.Cursor)
	}
}

func TestToggleLineCommentRebasesCursorWhenUncommenting(t *testing.T) {
	b := NewBufferFromBytes([]byte("    // foo\n"))
	b.SetCursor(Position{Line: 0, Col: 10})

	b.ToggleLineComment("//")

	if b.Content() != "    foo\n" {
		t.Fatalf("content = %q", b.Content())
	}
	// "// " (3 bytes) removed at column 4; a cursor left behind would sit
	// past the new end of the line.
	if b.Cursor != (Position{Line: 0, Col: 7}) {
		t.Errorf("cursor = %v, want {0 7}", b.Cursor)
	}
}

func TestToggleLineCommentLeavesCursorBeforePrefixAlone(t *testing.T) {
	b := NewBufferFromBytes([]byte("    // foo\n"))
	b.SetCursor(Position{Line: 0, Col: 2})

	b.ToggleLineComment("//")

	if b.Content() != "    foo\n" {
		t.Fatalf("content = %q", b.Content())
	}
	if b.Cursor != (Position{Line: 0, Col: 2}) {
		t.Errorf("cursor = %v, want {0 2}", b.Cursor)
	}
}

func TestIndentLinesRebasesSelectionRange(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo\n"))
	b.Selections = NewSelections(Position{Line: 0, Col: 1})
	b.Selections.selections[0] = Selection{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 3}}
	b.Selections.primary = 0
	b.Cursor = Position{Line: 0, Col: 3}

	b.IndentLines(4)

	if b.Content() != "    foo\n" {
		t.Fatalf("content = %q", b.Content())
	}
	sel := b.Selections.Primary()
	start, end := sel.Ordered()
	if start != (Position{Line: 0, Col: 5}) || end != (Position{Line: 0, Col: 7}) {
		t.Errorf("selection = %v..%v, want {0 5}..{0 7}", start, end)
	}
}
