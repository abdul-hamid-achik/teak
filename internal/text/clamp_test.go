package text

import "testing"

func TestClampCursorAfterDocumentShrinks(t *testing.T) {
	buf := NewBufferFromBytes([]byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk"))
	buf.Cursor = Position{Line: 8, Col: 1}

	// A formatter collapsing the document is the common trigger.
	buf.ReplaceRange(Position{Line: 0, Col: 0}, Position{Line: 10, Col: 1}, []byte("x\ny\nz"))
	buf.ClampCursor()

	if buf.Cursor.Line >= buf.LineCount() {
		t.Errorf("cursor line = %d, want < %d", buf.Cursor.Line, buf.LineCount())
	}
	if got, want := buf.Cursor, (Position{Line: 2, Col: 1}); got != want {
		t.Errorf("cursor = %+v, want %+v", got, want)
	}
}

func TestClampPositionConfinesLineAndColumn(t *testing.T) {
	buf := NewBufferFromBytes([]byte("abc\nde"))

	tests := []struct {
		name string
		in   Position
		want Position
	}{
		{"line past end", Position{Line: 99, Col: 0}, Position{Line: 1, Col: 0}},
		{"column past line end", Position{Line: 1, Col: 99}, Position{Line: 1, Col: 2}},
		{"negative line", Position{Line: -3, Col: 1}, Position{Line: 0, Col: 1}},
		{"negative column", Position{Line: 0, Col: -3}, Position{Line: 0, Col: 0}},
		{"already valid", Position{Line: 0, Col: 2}, Position{Line: 0, Col: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buf.ClampPosition(tc.in); got != tc.want {
				t.Errorf("ClampPosition(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestClampCursorConfinesSelections(t *testing.T) {
	buf := NewBufferFromBytes([]byte("abcdef\nghijkl\nmnopqr"))
	buf.SetSelection(Position{Line: 2, Col: 5}, Position{Line: 2, Col: 6})

	buf.ReplaceRange(Position{Line: 0, Col: 0}, Position{Line: 2, Col: 6}, []byte("hi"))
	buf.ClampCursor()

	// A selection left pointing past the end would resolve to a bogus byte
	// range on the next copy or delete.
	for i, sel := range buf.Selections.selections {
		if sel.Anchor.Line >= buf.LineCount() || sel.Head.Line >= buf.LineCount() {
			t.Errorf("selection %d = %+v addresses a line past %d", i, sel, buf.LineCount()-1)
		}
		if sel.Head.Col > buf.rope.LineLen(sel.Head.Line) {
			t.Errorf("selection %d head col = %d, past line length %d",
				i, sel.Head.Col, buf.rope.LineLen(sel.Head.Line))
		}
	}
}

func TestClampCursorOnEmptyBuffer(t *testing.T) {
	buf := NewBufferFromBytes(nil)
	buf.Cursor = Position{Line: 5, Col: 5}

	buf.ClampCursor()

	if got, want := buf.Cursor, (Position{}); got != want {
		t.Errorf("cursor = %+v, want %+v", got, want)
	}
}
