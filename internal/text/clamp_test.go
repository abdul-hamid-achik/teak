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

func TestClampPositionAlignsValidUTF8ButPreservesMalformedBytes(t *testing.T) {
	buf := NewBufferFromBytes([]byte{'a', '\n', 0xc3, 0xa9, 'x', '\n', 'a', 0x80, 'x'})

	tests := []struct {
		name string
		in   Position
		want Position
	}{
		{name: "inside valid multibyte rune", in: Position{Line: 1, Col: 1}, want: Position{Line: 1, Col: 0}},
		{name: "after valid multibyte rune", in: Position{Line: 1, Col: 2}, want: Position{Line: 1, Col: 2}},
		{name: "stray continuation byte", in: Position{Line: 2, Col: 1}, want: Position{Line: 2, Col: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buf.ClampPosition(tt.in); got != tt.want {
				t.Fatalf("ClampPosition(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVerticalCursorMovementNeverLandsInsideUTF8(t *testing.T) {
	tests := []struct {
		name  string
		start Position
		dir   Direction
		want  Position
	}{
		{name: "down", start: Position{Line: 0, Col: 1}, dir: DirDown, want: Position{Line: 1, Col: 0}},
		{name: "up", start: Position{Line: 2, Col: 1}, dir: DirUp, want: Position{Line: 1, Col: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte("ab\néx\ncd"))
			buf.SetCursor(tt.start)
			buf.MoveCursor(tt.dir)
			if got := buf.Cursor; got != tt.want {
				t.Fatalf("cursor = %+v, want %+v", got, tt.want)
			}
			buf.InsertAtCursor([]byte("!"))
			if got, want := buf.Content(), "ab\n!éx\ncd"; got != want {
				t.Fatalf("content after safe insertion = %q, want %q", got, want)
			}
		})
	}
}

func TestVerticalMultiCursorMovementNeverLandsInsideUTF8(t *testing.T) {
	buf := NewBufferFromBytes([]byte("ab\néx\ncd"))
	buf.SetCursor(Position{Line: 0, Col: 1})
	buf.Selections.Add(Selection{
		Anchor: Position{Line: 2, Col: 1},
		Head:   Position{Line: 2, Col: 1},
	})

	buf.MoveCursors(DirUp)
	if got, want := buf.Selections.All(), []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("MoveCursors(DirUp) = %#v, want %#v", got, want)
	}
}

func TestVerticalMultiCursorExtensionNeverLandsInsideUTF8(t *testing.T) {
	buf := NewBufferFromBytes([]byte("ab\néx\ncd"))
	buf.SetCursor(Position{Line: 0, Col: 1})
	buf.Selections.Add(Selection{
		Anchor: Position{Line: 2, Col: 1},
		Head:   Position{Line: 2, Col: 1},
	})

	buf.ExtendCursors(DirUp)
	for i, sel := range buf.Selections.All() {
		if sel.Head.Line == 1 && sel.Head.Col != 0 {
			t.Fatalf("selection %d head = %+v, want UTF-8 boundary at col 0", i, sel.Head)
		}
	}
}

func TestAddVerticalCursorNeverLandsInsideUTF8(t *testing.T) {
	tests := []struct {
		name  string
		start Position
		add   func(*Buffer)
	}{
		{name: "below", start: Position{Line: 0, Col: 1}, add: (*Buffer).AddCursorBelow},
		{name: "above", start: Position{Line: 2, Col: 1}, add: (*Buffer).AddCursorAbove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte("ab\néx\ncd"))
			buf.SetCursor(tt.start)
			tt.add(buf)
			for i, sel := range buf.Selections.All() {
				if sel.Head.Line == 1 && sel.Head.Col != 0 {
					t.Fatalf("selection %d = %+v, want UTF-8 boundary at col 0", i, sel)
				}
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

func TestClampCursorRepairsNilSelections(t *testing.T) {
	buf := NewBufferFromBytes([]byte("hello\nworld"))
	buf.Selections = nil
	buf.Cursor = Position{Line: 99, Col: 99}

	buf.ClampCursor()

	if buf.Selections == nil || buf.Selections.Count() != 1 {
		t.Fatalf("selections = %#v, want one repaired selection", buf.Selections)
	}
	if got, want := buf.Selections.PrimaryCursor(), (Position{Line: 1, Col: 5}); got != want {
		t.Fatalf("primary cursor = %+v, want %+v", got, want)
	}
	if buf.Cursor != buf.Selections.PrimaryCursor() {
		t.Fatalf("cursor = %+v and primary cursor = %+v drifted", buf.Cursor, buf.Selections.PrimaryCursor())
	}
}

func TestReplaceRangeRebasesCursorAndSelections(t *testing.T) {
	buf := NewBufferFromBytes([]byte("zero\none\ntwo\nthree"))
	buf.SetSelection(Position{Line: 2, Col: 1}, Position{Line: 3, Col: 2})

	buf.ReplaceRange(Position{Line: 0, Col: 0}, Position{Line: 0, Col: 4}, []byte("header\n"))

	if got, want := buf.Cursor, (Position{Line: 4, Col: 2}); got != want {
		t.Errorf("cursor = %+v, want %+v after inserting a line before it", got, want)
	}
	if got, want := buf.Selections.Primary(), (Selection{
		Anchor: Position{Line: 3, Col: 1},
		Head:   Position{Line: 4, Col: 2},
	}); got != want {
		t.Errorf("primary selection = %+v, want %+v after rebasing", got, want)
	}
}

func TestReplaceRangeMapsPositionsInsideReplacedRangeToReplacementEnd(t *testing.T) {
	buf := NewBufferFromBytes([]byte("before target after"))
	buf.SetSelection(Position{Line: 0, Col: 8}, Position{Line: 0, Col: 12})

	buf.ReplaceRange(Position{Line: 0, Col: 7}, Position{Line: 0, Col: 13}, []byte("new"))

	if got, want := buf.Cursor, (Position{Line: 0, Col: 10}); got != want {
		t.Errorf("cursor = %+v, want %+v at replacement end", got, want)
	}
	if got, want := buf.Selections.Primary(), (Selection{
		Anchor: Position{Line: 0, Col: 10},
		Head:   Position{Line: 0, Col: 10},
	}); got != want {
		t.Errorf("primary selection = %+v, want collapsed replacement end %+v", got, want)
	}
}

func TestDuplicateLineUpKeepsCursorAndPrimarySelectionInSync(t *testing.T) {
	buf := NewBufferFromBytes([]byte("first\nsecond"))
	buf.SetCursor(Position{Line: 1, Col: 3})
	buf.DuplicateLineUp()

	if got, want := buf.Cursor, (Position{Line: 1, Col: 3}); got != want {
		t.Errorf("cursor = %+v, want %+v on duplicated line", got, want)
	}
	if got, want := buf.Selections.PrimaryCursor(), buf.Cursor; got != want {
		t.Errorf("primary cursor = %+v, cursor = %+v; duplicate left selection stale", got, want)
	}
}
