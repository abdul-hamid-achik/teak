package text

import (
	"slices"
	"strings"
	"testing"
)

// --- Selections Type Tests ---

func TestSelectionsNew(t *testing.T) {
	s := NewSelections(Position{0, 5})

	if s.Count() != 1 {
		t.Errorf("Count() = %d, want 1", s.Count())
	}

	if s.PrimaryCursor() != (Position{0, 5}) {
		t.Errorf("PrimaryCursor() = %v, want {0, 5}", s.PrimaryCursor())
	}
}

func TestSelectionsAdd(t *testing.T) {
	s := NewSelections(Position{0, 0})
	s.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 5}})

	if s.Count() != 2 {
		t.Errorf("Count() = %d, want 2", s.Count())
	}

	if s.PrimaryCursor() != (Position{1, 5}) {
		t.Errorf("PrimaryCursor() should be last added selection")
	}
}

func TestSelectionsMaxLimit(t *testing.T) {
	s := NewSelections(Position{0, 0})

	// Try to add 1001 selections (limit is 1000)
	for i := 1; i < 1001; i++ {
		s.Add(Selection{Anchor: Position{i, 0}, Head: Position{i, 5}})
	}

	if s.Count() > 1000 {
		t.Errorf("Count() = %d, should be capped at 1000", s.Count())
	}
}

func TestSelectionsClear(t *testing.T) {
	s := NewSelections(Position{0, 0})
	s.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 5}})
	s.Add(Selection{Anchor: Position{2, 0}, Head: Position{2, 5}})

	if s.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", s.Count())
	}

	s.Clear()

	if s.Count() != 1 {
		t.Errorf("After Clear(), Count() = %d, want 1", s.Count())
	}
}

func TestSelectionsNormalize(t *testing.T) {
	s := NewSelections(Position{2, 0})
	s.Add(Selection{Anchor: Position{0, 0}, Head: Position{0, 5}})
	s.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 5}})

	s.Normalize()

	all := s.All()
	if all[0].Head.Line != 0 {
		t.Error("Selections not sorted after Normalize()")
	}
	if all[1].Head.Line != 1 {
		t.Error("Selections not sorted after Normalize()")
	}
}

func TestSelectionsNormalizeRemovesOverlaps(t *testing.T) {
	s := &Selections{
		selections: []Selection{
			{Anchor: Position{0, 0}, Head: Position{0, 5}},
			{Anchor: Position{0, 5}, Head: Position{0, 10}}, // Adjacent, not overlapping.
			{Anchor: Position{0, 8}, Head: Position{0, 12}}, // Overlaps the second selection.
		},
		primary: 2,
		dirty:   true,
	}

	s.Normalize()

	if got, want := s.All(), []Selection{
		{Anchor: Position{0, 0}, Head: Position{0, 5}},
		{Anchor: Position{0, 5}, Head: Position{0, 10}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Normalize() = %#v, want %#v", got, want)
	}
	// The dropped primary overlaps the adjacent canonical range, so focus is
	// transferred to that retained range rather than leaving primary invalid.
	if got, want := s.Primary(), (Selection{Anchor: Position{0, 5}, Head: Position{0, 10}}); got != want {
		t.Errorf("Primary() = %#v, want %#v", got, want)
	}
}

func TestSelectionsNormalizeDeduplicatesCollapsedCursors(t *testing.T) {
	s := &Selections{
		selections: []Selection{
			{Anchor: Position{0, 4}, Head: Position{0, 4}},
			{Anchor: Position{0, 4}, Head: Position{0, 4}},
		},
		primary: 1,
		dirty:   true,
	}

	s.Normalize()

	if got := s.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	if got, want := s.PrimaryCursor(), (Position{0, 4}); got != want {
		t.Errorf("PrimaryCursor() = %v, want %v", got, want)
	}
}

func TestSelectionsNormalizeCoalescesCursorsInsideRanges(t *testing.T) {
	tests := []struct {
		name        string
		selections  []Selection
		primary     int
		want        []Selection
		wantPrimary int
	}{
		{
			name: "cursor in middle transfers primary to range",
			selections: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
				{Anchor: Position{0, 2}, Head: Position{0, 2}},
			},
			primary: 1,
			want: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
			},
			wantPrimary: 0,
		},
		{
			name: "cursor at range start is absorbed regardless of input order",
			selections: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 0}},
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
			},
			primary: 0,
			want: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
			},
			wantPrimary: 0,
		},
		{
			name: "cursor at half-open range end remains independent",
			selections: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
				{Anchor: Position{0, 5}, Head: Position{0, 5}},
			},
			primary: 1,
			want: []Selection{
				{Anchor: Position{0, 0}, Head: Position{0, 5}},
				{Anchor: Position{0, 5}, Head: Position{0, 5}},
			},
			wantPrimary: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Selections{selections: append([]Selection(nil), tt.selections...), primary: tt.primary, dirty: true}

			s.Normalize()

			if got := s.All(); !selectionSlicesEqual(got, tt.want) {
				t.Errorf("Normalize() = %#v, want %#v", got, tt.want)
			}
			if got := s.PrimaryIndex(); got != tt.wantPrimary {
				t.Errorf("PrimaryIndex() = %d, want %d", got, tt.wantPrimary)
			}
		})
	}
}

func TestSelectionsSetPrimary(t *testing.T) {
	s := NewSelections(Position{0, 0})
	s.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 5}})
	s.Add(Selection{Anchor: Position{2, 0}, Head: Position{2, 5}})

	s.SetPrimary(0)

	if s.PrimaryCursor() != (Position{0, 0}) {
		t.Errorf("PrimaryCursor() = %v, want {0, 0}", s.PrimaryCursor())
	}
}

func TestSelectionsSetPrimaryOutOfBounds(t *testing.T) {
	s := NewSelections(Position{0, 0})
	s.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 5}})

	// Should not panic
	s.SetPrimary(100)
	s.SetPrimary(-1)

	// Should still work
	if s.Count() != 2 {
		t.Error("SetPrimary with invalid index should not modify selections")
	}
}

// --- Buffer Multi-Selection Tests ---

func TestBufferInsertAtCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello\nworld\nfoo"))

	// Set up multiple cursors
	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 0}})

	b.InsertAtCursor([]byte("X"))

	if b.Content() != "Xhello\nXworld\nfoo" {
		t.Errorf("got %q, want %q", b.Content(), "Xhello\nXworld\nfoo")
	}
}

func TestBufferInsertAtCursorsUpdatesAllPositions(t *testing.T) {
	b := NewBufferFromBytes([]byte("a\nb\nc"))

	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 0}})
	b.Selections.Add(Selection{Anchor: Position{2, 0}, Head: Position{2, 0}})

	b.InsertAtCursor([]byte("X"))

	// All cursors should have moved
	for i, sel := range b.Selections.All() {
		if sel.Head.Col != 1 {
			t.Errorf("Selection %d Head.Col = %d, want 1", i, sel.Head.Col)
		}
	}
}

func TestBufferInsertAtCursorsSameLineRebasesPositions(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello"))

	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{0, 5}, Head: Position{0, 5}})

	b.InsertAtCursor([]byte("X"))

	if b.Content() != "XhelloX" {
		t.Fatalf("got %q, want %q", b.Content(), "XhelloX")
	}

	all := b.Selections.All()
	if all[0].Head != (Position{0, 1}) {
		t.Errorf("first cursor = %v, want {0 1}", all[0].Head)
	}
	if all[1].Head != (Position{0, 7}) {
		t.Errorf("second cursor = %v, want {0 7}", all[1].Head)
	}
	if b.Cursor != (Position{0, 7}) {
		t.Errorf("buffer cursor = %v, want {0 7}", b.Cursor)
	}
	if b.LastChange() != nil {
		t.Errorf("LastChange() = %#v, want nil for multi-cursor insert", b.LastChange())
	}
}

func TestBufferDeleteSelectionsMultiple(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello\nworld\nfoo"))

	// Select "hell" on line 0 and "worl" on line 1
	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{0, 0}, Head: Position{0, 4}})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 4}})

	b.DeleteSelection()

	// After delete: "o" on line 0, "d" on line 1, "foo" on line 2
	if b.Content() != "o\nd\nfoo" {
		t.Errorf("got %q, want %q", b.Content(), "o\nd\nfoo")
	}
}

func TestBufferDeleteSelectionsMultipleRebasesPrimaryCursor(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello\nworld\nfoo"))

	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{0, 0}, Head: Position{0, 4}})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 4}})

	b.DeleteSelection()

	if b.Content() != "o\nd\nfoo" {
		t.Fatalf("got %q, want %q", b.Content(), "o\nd\nfoo")
	}
	if b.Selections.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", b.Selections.Count())
	}
	if got := b.Selections.Primary().Head; got != (Position{1, 0}) {
		t.Errorf("primary cursor = %v, want {1 0}", got)
	}
	if b.Cursor != (Position{1, 0}) {
		t.Errorf("buffer cursor = %v, want {1 0}", b.Cursor)
	}
	if b.LastChange() != nil {
		t.Errorf("LastChange() = %#v, want nil for multi-selection delete", b.LastChange())
	}
}

func TestBufferDeleteSelectionPreservesCollapsedCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("abc\ndef"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 2}},
		{Anchor: Position{Line: 1, Col: 2}, Head: Position{Line: 1, Col: 2}},
	}, 1)

	b.DeleteSelection()

	if got, want := b.Content(), "ac\ndef"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	want := []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 1, Col: 2}, Head: Position{Line: 1, Col: 2}},
	}
	if got := b.Selections.All(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("selections = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{Line: 1, Col: 2}); got != want {
		t.Fatalf("primary cursor = %+v, want %+v", got, want)
	}
}

func TestBufferDeleteSelectionRebasesCursorBetweenDeletedRanges(t *testing.T) {
	b := NewBufferFromBytes([]byte("abc def ghi"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 3}},
		{Anchor: Position{Line: 0, Col: 5}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 0, Col: 8}, Head: Position{Line: 0, Col: 11}},
	}, 1)

	b.DeleteSelection()

	if got, want := b.Content(), " def "; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := b.Cursor, (Position{Line: 0, Col: 2}); got != want {
		t.Fatalf("middle cursor = %+v, want %+v", got, want)
	}
}

func TestBufferBackspaceWordAppliesAtEverySelection(t *testing.T) {
	b := NewBufferFromBytes([]byte("one two\nred blue"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: len("one ")}, Head: Position{Line: 0, Col: len("one two")}},
		{Anchor: Position{Line: 1, Col: len("red blue")}, Head: Position{Line: 1, Col: len("red blue")}},
	}, 1)

	b.BackspaceWord()

	if got, want := b.Content(), "one \nred "; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	want := []Selection{
		{Anchor: Position{Line: 0, Col: len("one ")}, Head: Position{Line: 0, Col: len("one ")}},
		{Anchor: Position{Line: 1, Col: len("red ")}, Head: Position{Line: 1, Col: len("red ")}},
	}
	if got := b.Selections.All(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("selections = %#v, want %#v", got, want)
	}
}

func TestBufferDeleteWordAppliesAtEveryCursor(t *testing.T) {
	b := NewBufferFromBytes([]byte("one two\nred blue"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 0}},
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
	}, 1)

	b.DeleteWord()

	if got, want := b.Content(), "two\nblue"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	want := []Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 0}},
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
	}
	if got := b.Selections.All(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("selections = %#v, want %#v", got, want)
	}
}

func TestBufferWordDeletionMergesOverlappingCursorRanges(t *testing.T) {
	tests := []struct {
		name       string
		positions  []Position
		delete     func(*Buffer)
		want       string
		wantCursor Position
	}{
		{
			name:       "forward",
			positions:  []Position{{Line: 0, Col: 0}, {Line: 0, Col: 2}},
			delete:     (*Buffer).DeleteWord,
			want:       "soup",
			wantCursor: Position{},
		},
		{
			name:       "backward",
			positions:  []Position{{Line: 0, Col: 6}, {Line: 0, Col: len("alphabet")}},
			delete:     (*Buffer).BackspaceWord,
			want:       " soup",
			wantCursor: Position{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBufferFromBytes([]byte("alphabet soup"))
			selections := make([]Selection, len(tt.positions))
			for i, pos := range tt.positions {
				selections[i] = Selection{Anchor: pos, Head: pos}
			}
			b.RestoreSelections(selections, len(selections)-1)

			tt.delete(b)

			if got := b.Content(); got != tt.want {
				t.Fatalf("content = %q, want %q", got, tt.want)
			}
			if got := b.Selections.Count(); got != 1 {
				t.Fatalf("selection count = %d, want coalesced cursor", got)
			}
			if got := b.Cursor; got != tt.wantCursor {
				t.Fatalf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

func TestBufferMultiCursorWordDeleteUndoRestoresContent(t *testing.T) {
	b := NewBufferFromBytes([]byte("one two\nred blue"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 0}},
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
	}, 1)

	b.DeleteWord()
	b.Undo()
	if got, want := b.Content(), "one two\nred blue"; got != want {
		t.Fatalf("content after undo = %q, want %q", got, want)
	}
	b.Redo()
	if got, want := b.Content(), "two\nblue"; got != want {
		t.Fatalf("content after redo = %q, want %q", got, want)
	}
}

func TestWordNavigationBoundsGiantTokenWork(t *testing.T) {
	const giantTokenBytes = 8 << 20
	b := NewBufferFromBytes([]byte(strings.Repeat("x", giantTokenBytes)))

	result := testing.Benchmark(func(bench *testing.B) {
		for bench.Loop() {
			b.SetCursor(Position{})
			b.MoveCursorWordRight()
		}
	})
	if got, want := b.Cursor.Col, maxInteractiveWordNavigationBytes; got != want {
		t.Fatalf("bounded word jump = %d bytes, want %d", got, want)
	}
	if got := result.AllocedBytesPerOp(); got > 512<<10 {
		t.Fatalf("word navigation allocated %d B/op for an 8 MiB token; want below 512 KiB", got)
	}
}

func TestBufferSelectNextOccurrenceMulti(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo bar foo baz foo"))

	// Start with selection on first "foo"
	b.SetSelection(Position{0, 0}, Position{0, 3})

	// Select next occurrence
	b.SelectNextOccurrence()

	if b.Selections.Count() != 2 {
		t.Errorf("Count() = %d, want 2", b.Selections.Count())
	}
	if got, want := b.Cursor, (Position{0, 11}); got != want {
		t.Errorf("Cursor after first occurrence = %v, want %v", got, want)
	}

	// Select another
	b.SelectNextOccurrence()

	if b.Selections.Count() != 3 {
		t.Errorf("After second SelectNextOccurrence, Count() = %d, want 3", b.Selections.Count())
	}
	if got, want := b.Cursor, (Position{0, 19}); got != want {
		t.Errorf("Cursor after second occurrence = %v, want %v", got, want)
	}
}

func TestBufferSelectAllOccurrences(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo bar foo baz foo"))

	// Start with selection on first "foo"
	b.SetSelection(Position{0, 0}, Position{0, 3})

	// Select all occurrences
	b.SelectAllOccurrences()

	if b.Selections.Count() != 3 {
		t.Errorf("Count() = %d, want 3", b.Selections.Count())
	}
	if got, want := b.Cursor, (Position{0, 19}); got != want {
		t.Errorf("Cursor = %v, want %v", got, want)
	}
}

func TestOccurrenceSearchRejectsOversizedDocumentWithoutFlattening(t *testing.T) {
	b := NewBufferFromBytes([]byte(strings.Repeat("word ", MaxOccurrenceSearchBytes/5+2)))
	b.SetSelection(Position{}, Position{Col: 4})
	before := slices.Clone(b.Selections.All())

	if b.SelectNextOccurrence() {
		t.Fatal("SelectNextOccurrence accepted a document above its interactive scan budget")
	}
	if got := b.Selections.All(); !slices.Equal(got, before) {
		t.Fatalf("oversized occurrence search changed selections: got %+v want %+v", got, before)
	}
	if b.SelectAllOccurrences() {
		t.Fatal("SelectAllOccurrences accepted a document above its interactive scan budget")
	}
	if got := b.Selections.All(); !slices.Equal(got, before) {
		t.Fatalf("oversized select-all changed selections: got %+v want %+v", got, before)
	}
}

func TestBufferAddCursorAbove(t *testing.T) {
	b := NewBufferFromBytes([]byte("line1\nline2\nline3"))

	b.SetCursor(Position{1, 3}) // Middle line
	b.AddCursorAbove()

	if b.Selections.Count() != 2 {
		t.Errorf("Count() = %d, want 2", b.Selections.Count())
	}

	// Check positions (selections are sorted after Normalize)
	all := b.Selections.All()
	// After sorting, line 0 comes first, line 1 second
	if all[0].Head.Line != 0 {
		t.Errorf("First cursor line = %d, want 0", all[0].Head.Line)
	}
	if all[1].Head.Line != 1 {
		t.Errorf("Second cursor line = %d, want 1", all[1].Head.Line)
	}
}

func TestBufferAddCursorBelow(t *testing.T) {
	b := NewBufferFromBytes([]byte("line1\nline2\nline3"))

	b.SetCursor(Position{1, 3}) // Middle line
	b.AddCursorBelow()

	if b.Selections.Count() != 2 {
		t.Errorf("Count() = %d, want 2", b.Selections.Count())
	}
}

func TestBufferAddCursorAboveAtTop(t *testing.T) {
	b := NewBufferFromBytes([]byte("line1\nline2"))

	b.SetCursor(Position{0, 3}) // Top line
	b.AddCursorAbove()

	// Should not add cursor (already at top)
	if b.Selections.Count() != 1 {
		t.Errorf("Count() = %d, want 1", b.Selections.Count())
	}
}

func TestBufferAddCursorBelowAtBottom(t *testing.T) {
	b := NewBufferFromBytes([]byte("line1\nline2"))

	b.SetCursor(Position{1, 3}) // Bottom line
	b.AddCursorBelow()

	// Should not add cursor (already at bottom)
	if b.Selections.Count() != 1 {
		t.Errorf("Count() = %d, want 1", b.Selections.Count())
	}
}

func TestBufferSplitSelectionIntoLines(t *testing.T) {
	b := NewBufferFromBytes([]byte("ab\n\ncdef\nxy"))
	b.SetSelection(Position{0, 1}, Position{3, 1})

	b.SplitSelectionIntoLines()

	want := []Selection{
		{Anchor: Position{0, 1}, Head: Position{0, 2}},
		// An empty intermediate line is still a useful cursor target.
		{Anchor: Position{1, 0}, Head: Position{1, 0}},
		{Anchor: Position{2, 0}, Head: Position{2, 4}},
		{Anchor: Position{3, 0}, Head: Position{3, 1}},
	}
	if got := b.Selections.All(); len(got) != len(want) {
		t.Fatalf("selection count = %d, want %d (%#v)", len(got), len(want), got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("selection %d = %#v, want %#v", i, got[i], want[i])
			}
		}
	}
	if got, want := b.Selections.Primary(), want[len(want)-1]; got != want {
		t.Errorf("primary = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{3, 1}); got != want {
		t.Errorf("Cursor = %v, want %v", got, want)
	}
}

func TestBufferSplitSelectionIntoLinesPartial(t *testing.T) {
	t.Run("reversed selection keeps the head-side selection primary", func(t *testing.T) {
		b := NewBufferFromBytes([]byte("ab\n\ncdef\nxy"))
		b.SetSelection(Position{3, 1}, Position{0, 1})

		b.SplitSelectionIntoLines()

		if got, want := b.Selections.Count(), 4; got != want {
			t.Fatalf("Count() = %d, want %d", got, want)
		}
		if got, want := b.Selections.Primary(), (Selection{Anchor: Position{0, 1}, Head: Position{0, 2}}); got != want {
			t.Errorf("Primary() = %#v, want %#v", got, want)
		}
		if got, want := b.Cursor, (Position{0, 2}); got != want {
			t.Errorf("Cursor = %v, want %v", got, want)
		}
	})

	t.Run("end at the next line start does not add a spurious cursor", func(t *testing.T) {
		b := NewBufferFromBytes([]byte("ab\ncd"))
		b.SetSelection(Position{0, 1}, Position{1, 0})

		b.SplitSelectionIntoLines()

		if got, want := b.Selections.All(), []Selection{{Anchor: Position{0, 1}, Head: Position{0, 2}}}; len(got) != len(want) || got[0] != want[0] {
			t.Errorf("Selections = %#v, want %#v", got, want)
		}
	})
}

func TestBufferSplitSelectionIntoLinesRespectsMaximum(t *testing.T) {
	content := strings.Repeat("x\n", MaxSelections+1) + "x"
	b := NewBufferFromBytes([]byte(content))
	lastLine := MaxSelections + 1
	b.SetSelection(Position{0, 0}, Position{lastLine, 1})

	b.SplitSelectionIntoLines()

	if got, want := b.Selections.Count(), MaxSelections; got != want {
		t.Fatalf("Count() = %d, want %d", got, want)
	}
	if got, want := b.Selections.All()[0].Head.Line, 2; got != want {
		t.Errorf("first retained line = %d, want %d", got, want)
	}
	if got, want := b.Selections.PrimaryCursor(), (Position{lastLine, 1}); got != want {
		t.Errorf("PrimaryCursor() = %v, want %v", got, want)
	}
}

func TestBufferMoveCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("éx\n🙂"))
	b.SetCursor(Position{0, 2}) // after the two-byte "é"
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 0}})

	b.MoveCursors(DirRight)

	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{0, 3}, Head: Position{0, 3}},
		{Anchor: Position{1, 4}, Head: Position{1, 4}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("MoveCursors(DirRight) = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{1, 4}); got != want {
		t.Errorf("Cursor = %v, want %v", got, want)
	}

	b.MoveCursors(DirLeft)
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{0, 2}, Head: Position{0, 2}},
		{Anchor: Position{1, 0}, Head: Position{1, 0}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("MoveCursors(DirLeft) = %#v, want %#v", got, want)
	}
}

func TestBufferMoveCursorsDeduplicatesCollisionAndKeepsPrimary(t *testing.T) {
	b := NewBufferFromBytes([]byte("é"))
	b.SetCursor(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{0, 2}, Head: Position{0, 2}})

	b.MoveCursors(DirLeft)

	if got, want := b.Selections.Count(), 1; got != want {
		t.Fatalf("Count() = %d, want %d", got, want)
	}
	if got, want := b.Selections.PrimaryCursor(), (Position{0, 0}); got != want {
		t.Errorf("PrimaryCursor() = %v, want %v", got, want)
	}
}

func TestBufferHorizontalMultiCursorMovementBoundsGiantLineWork(t *testing.T) {
	const giantLineBytes = 8 << 20
	b := NewBufferFromBytes([]byte(strings.Repeat("x", giantLineBytes)))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 0, Col: giantLineBytes - 1}, Head: Position{Line: 0, Col: giantLineBytes - 1}},
	}, 1)

	result := testing.Benchmark(func(bench *testing.B) {
		for bench.Loop() {
			b.MoveCursors(DirRight)
			b.MoveCursors(DirLeft)
		}
	})
	if got := result.AllocedBytesPerOp(); got > 256<<10 {
		t.Fatalf("horizontal movement allocated %d B/op for an 8 MiB line; want below 256 KiB", got)
	}
	if got, want := b.Selections.Count(), 2; got != want {
		t.Fatalf("cursor count = %d, want %d", got, want)
	}
}

func TestBufferMoveCursorsRespectsLineBounds(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello"))

	b.SetCursor(Position{0, 3})

	// Try to move down from last line
	b.MoveCursors(DirDown)

	// Should stay on same line
	if b.Selections.Primary().Head.Line != 0 {
		t.Errorf("Cursor moved past last line")
	}
}

func TestBufferExtendCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello\nworld"))

	b.SetCursor(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 0}})

	b.ExtendCursors(DirRight)

	// Both selections should be extended
	for i, sel := range b.Selections.All() {
		if sel.Head.Col != 1 {
			t.Errorf("Selection %d Head.Col = %d, want 1", i, sel.Head.Col)
		}
		if sel.Anchor.Col != 0 {
			t.Errorf("Selection %d Anchor.Col = %d, want 0", i, sel.Anchor.Col)
		}
	}
}

func TestBufferExtendCursorsToDocumentBounds(t *testing.T) {
	// Every head lands on the same bound, so the nested ranges coalesce the
	// same way the line-bound variants do: the first cursor's range survives.
	tests := []struct {
		name   string
		extend func(*Buffer)
		want   []Selection
	}{
		{
			name:   "doc start",
			extend: (*Buffer).ExtendCursorsToDocStart,
			want: []Selection{
				{Anchor: Position{0, 1}, Head: Position{0, 0}},
			},
		},
		{
			name:   "doc end",
			extend: (*Buffer).ExtendCursorsToDocEnd,
			want: []Selection{
				{Anchor: Position{0, 1}, Head: Position{1, 3}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBufferFromBytes([]byte("ab\ncde"))
			b.Selections = NewSelections(Position{0, 1})
			b.Selections.Add(Selection{Anchor: Position{1, 2}, Head: Position{1, 2}})

			tt.extend(b)

			if got := b.Selections.All(); !selectionSlicesEqual(got, tt.want) {
				t.Fatalf("%s = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

func TestBufferSetCursor(t *testing.T) {
	b := NewBuffer()

	b.SetCursor(Position{5, 10})

	if b.Cursor != (Position{5, 10}) {
		t.Errorf("Cursor = %v, want {5, 10}", b.Cursor)
	}

	// Selection should also be updated
	sel := b.Selections.Primary()
	if sel.Anchor != (Position{5, 10}) || sel.Head != (Position{5, 10}) {
		t.Error("Selection not updated to match cursor")
	}
}

func TestBufferUndoRedoWithMultiSelection(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello\nworld"))

	b.SetCursor(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{1, 0}, Head: Position{1, 0}})

	b.InsertAtCursor([]byte("X"))

	if b.Content() != "Xhello\nXworld" {
		t.Fatalf("After insert: got %q", b.Content())
	}

	// Undo
	b.Undo()

	if b.Content() != "hello\nworld" {
		t.Errorf("After undo: got %q, want %q", b.Content(), "hello\nworld")
	}
	if b.Selections.Count() != 2 {
		t.Fatalf("undo collapsed multi-cursors: count = %d", b.Selections.Count())
	}
	// Add makes the second cursor primary. The atomic edit reconciles the
	// compatibility Buffer.Cursor field before saving Undo, so history restores
	// that active cursor instead of the stale first-cursor value.
	if got, want := b.Selections.Primary(), (Selection{Anchor: Position{1, 0}, Head: Position{1, 0}}); got != want {
		t.Errorf("selection after undo = %#v, want %#v", got, want)
	}

	// Redo
	b.Redo()

	if b.Content() != "Xhello\nXworld" {
		t.Errorf("After redo: got %q, want %q", b.Content(), "Xhello\nXworld")
	}
	if got, want := b.Selections.Primary(), (Selection{Anchor: Position{1, 1}, Head: Position{1, 1}}); got != want {
		t.Errorf("selection after redo = %#v, want %#v", got, want)
	}
}

func TestBufferInsertAtMultipleSelectedRangesReplacesEachRange(t *testing.T) {
	b := NewBufferFromBytes([]byte("one two three"))
	b.SetSelection(Position{0, 0}, Position{0, 3})
	b.Selections.Add(Selection{Anchor: Position{0, 8}, Head: Position{0, 13}})

	b.InsertAtCursor([]byte("X"))

	if got, want := b.Content(), "X two X"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{0, 1}, Head: Position{0, 1}},
		{Anchor: Position{0, 7}, Head: Position{0, 7}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{0, 7}); got != want {
		t.Errorf("Cursor = %v, want %v", got, want)
	}
	if b.LastChange() != nil {
		t.Errorf("LastChange() = %#v, want nil for multi-selection replacement", b.LastChange())
	}
}

func TestBufferInsertAtCursorCoalescesCursorInsideSelectedRange(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcdef"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 0, Col: 2}, Head: Position{Line: 0, Col: 2}},
	}, 1)

	b.InsertAtCursor([]byte("X"))

	if got, want := b.Content(), "Xf"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
	if got, want := b.LastChange(), (&EditChange{
		StartLine: 0, StartCol: 0,
		EndLine: 0, EndCol: 5,
		Text: "X",
	}); got == nil || *got != *want {
		t.Errorf("LastChange() = %#v, want %#v", got, want)
	}
}

func TestBufferMultiSelectionEmptySelections(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello"))

	b.SetCursor(Position{0, 3})

	// Selection is empty (cursor == anchor)
	sel := b.Selections.Primary()
	if !sel.IsEmpty() {
		t.Error("Initial selection should be empty")
	}

	// Typing should still work
	b.InsertAtCursor([]byte("X"))

	if b.Content() != "helXlo" {
		t.Errorf("got %q, want %q", b.Content(), "helXlo")
	}
}

func TestBufferBackspaceWithMultipleCollapsedCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcdef"))

	b.Selections = NewSelections(Position{0, 2})
	b.Selections.Add(Selection{Anchor: Position{0, 5}, Head: Position{0, 5}})

	b.Backspace()

	if b.Content() != "acdf" {
		t.Fatalf("got %q, want %q", b.Content(), "acdf")
	}
	all := b.Selections.All()
	if len(all) != 2 {
		t.Fatalf("cursor count = %d, want 2", len(all))
	}
	if all[0].Head != (Position{0, 1}) {
		t.Errorf("first cursor = %v, want {0 1}", all[0].Head)
	}
	if all[1].Head != (Position{0, 3}) {
		t.Errorf("second cursor = %v, want {0 3}", all[1].Head)
	}
	if b.LastChange() != nil {
		t.Errorf("LastChange() = %#v, want nil for multi-cursor edit", b.LastChange())
	}

	// A follow-up multi-cursor edit must land at the rebased positions, not
	// at the stale offsets recorded before the backspace.
	b.InsertAtCursor([]byte("X"))
	if b.Content() != "aXcdXf" {
		t.Fatalf("after insert got %q, want %q", b.Content(), "aXcdXf")
	}
}

func TestBufferDeleteWithMultipleCollapsedCursors(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcdef"))

	b.Selections = NewSelections(Position{0, 1})
	b.Selections.Add(Selection{Anchor: Position{0, 4}, Head: Position{0, 4}})

	b.Delete()

	if b.Content() != "acdf" {
		t.Fatalf("got %q, want %q", b.Content(), "acdf")
	}
	all := b.Selections.All()
	if len(all) != 2 {
		t.Fatalf("cursor count = %d, want 2", len(all))
	}
	if all[0].Head != (Position{0, 1}) {
		t.Errorf("first cursor = %v, want {0 1}", all[0].Head)
	}
	if all[1].Head != (Position{0, 3}) {
		t.Errorf("second cursor = %v, want {0 3}", all[1].Head)
	}

	b.InsertAtCursor([]byte("X"))
	if b.Content() != "aXcdXf" {
		t.Fatalf("after insert got %q, want %q", b.Content(), "aXcdXf")
	}
}

func TestBufferBackspaceMultiCursorJoinsLines(t *testing.T) {
	b := NewBufferFromBytes([]byte("ab\ncd\nef"))

	// Cursors at the start of lines 1 and 2: backspace removes the preceding
	// newline, joining each line with the previous one.
	b.Selections = NewSelections(Position{1, 0})
	b.Selections.Add(Selection{Anchor: Position{2, 0}, Head: Position{2, 0}})

	b.Backspace()

	if b.Content() != "abcdef" {
		t.Fatalf("got %q, want %q", b.Content(), "abcdef")
	}
	all := b.Selections.All()
	if all[0].Head != (Position{0, 2}) {
		t.Errorf("first cursor = %v, want {0 2}", all[0].Head)
	}
	if all[1].Head != (Position{0, 4}) {
		t.Errorf("second cursor = %v, want {0 4}", all[1].Head)
	}
}

func TestBufferBackspaceMultiCursorAtDocumentStart(t *testing.T) {
	b := NewBufferFromBytes([]byte("ab\ncd"))

	b.Selections = NewSelections(Position{0, 0})
	b.Selections.Add(Selection{Anchor: Position{1, 1}, Head: Position{1, 1}})

	b.Backspace()

	if b.Content() != "ab\nd" {
		t.Fatalf("got %q, want %q", b.Content(), "ab\nd")
	}
	all := b.Selections.All()
	if all[0].Head != (Position{0, 0}) {
		t.Errorf("first cursor = %v, want {0 0}", all[0].Head)
	}
	if all[1].Head != (Position{1, 0}) {
		t.Errorf("second cursor = %v, want {1 0}", all[1].Head)
	}
}

func TestBufferBackspaceMultiCursorRemovesWholeRunes(t *testing.T) {
	b := NewBufferFromBytes([]byte("xé zé"))

	// Cursors just after each multibyte é.
	b.Selections = NewSelections(Position{0, 3})
	b.Selections.Add(Selection{Anchor: Position{0, 7}, Head: Position{0, 7}})

	b.Backspace()

	if b.Content() != "x z" {
		t.Fatalf("got %q, want %q", b.Content(), "x z")
	}
	all := b.Selections.All()
	if all[0].Head != (Position{0, 1}) {
		t.Errorf("first cursor = %v, want {0 1}", all[0].Head)
	}
	if all[1].Head != (Position{0, 3}) {
		t.Errorf("second cursor = %v, want {0 3}", all[1].Head)
	}
}

func TestBufferDeleteMultiCursorAtDocumentEnd(t *testing.T) {
	b := NewBufferFromBytes([]byte("ab\ncd"))

	b.Selections = NewSelections(Position{0, 1})
	b.Selections.Add(Selection{Anchor: Position{1, 2}, Head: Position{1, 2}})

	b.Delete()

	if b.Content() != "a\ncd" {
		t.Fatalf("got %q, want %q", b.Content(), "a\ncd")
	}
	all := b.Selections.All()
	if all[0].Head != (Position{0, 1}) {
		t.Errorf("first cursor = %v, want {0 1}", all[0].Head)
	}
	if all[1].Head != (Position{1, 2}) {
		t.Errorf("second cursor = %v, want {1 2}", all[1].Head)
	}
}

func TestBufferDropSecondaryCursorsCollapsesToPrimaryCaret(t *testing.T) {
	b := NewBufferFromBytes([]byte("alpha beta\nalpha gamma\n"))
	b.SetSelection(Position{0, 0}, Position{0, 5})
	if !b.SelectAllOccurrences() {
		t.Fatal("SelectAllOccurrences failed to arm multiple selections")
	}
	if b.Selections.Count() < 2 {
		t.Fatalf("selection count = %d, want at least 2 before dropping", b.Selections.Count())
	}
	primary := b.Selections.PrimaryCursor()
	dirtyBefore := b.Dirty()
	versionBefore := b.Version()
	changeBefore := b.LastChange()

	b.DropSecondaryCursors()

	if got := b.Selections.Count(); got != 1 {
		t.Fatalf("selection count after drop = %d, want 1", got)
	}
	if got := b.Selections.Primary(); got.Anchor != primary || got.Head != primary {
		t.Fatalf("primary selection after drop = %#v, want collapsed caret at %+v", got, primary)
	}
	if b.Cursor != primary {
		t.Fatalf("cursor after drop = %+v, want primary cursor %+v", b.Cursor, primary)
	}
	if b.Dirty() != dirtyBefore {
		t.Error("dropping cursors flipped the dirty flag")
	}
	if b.Version() != versionBefore {
		t.Error("dropping cursors bumped the buffer version")
	}
	if b.LastChange() != changeBefore {
		t.Error("dropping cursors fabricated a change record")
	}
}

func TestBufferDropSecondaryCursorsLeavesSingleSelectionAlone(t *testing.T) {
	b := NewBufferFromBytes([]byte("alpha beta"))
	b.SetSelection(Position{0, 0}, Position{0, 5})

	b.DropSecondaryCursors()

	if got := b.Selections.Count(); got != 1 {
		t.Fatalf("selection count = %d, want 1", got)
	}
	want := Selection{Anchor: Position{0, 0}, Head: Position{0, 5}}
	if got := b.Selections.Primary(); got != want {
		t.Fatalf("single selection after drop = %#v, want untouched %#v", got, want)
	}
}

func TestBufferDropSecondaryCursorsNilSelections(t *testing.T) {
	b := NewBufferFromBytes([]byte("ab"))
	b.Selections = nil

	b.DropSecondaryCursors()

	if b.Selections != nil {
		t.Fatal("drop with nil selections rebuilt a selection set")
	}
}

func TestBufferUndoLastCursorReversesSelectNextOccurrence(t *testing.T) {
	b := NewBufferFromBytes([]byte("foo foo foo\n"))
	b.SetSelection(Position{0, 0}, Position{0, 3})
	if !b.SelectNextOccurrence() {
		t.Fatal("SelectNextOccurrence failed")
	}
	if b.Selections.Count() != 2 {
		t.Fatalf("selection count after next = %d, want 2", b.Selections.Count())
	}

	b.UndoLastCursor()

	if got := b.Selections.Count(); got != 1 {
		t.Fatalf("selection count after undo = %d, want 1", got)
	}
	want := Selection{Anchor: Position{0, 0}, Head: Position{0, 3}}
	if got := b.Selections.Primary(); got != want {
		t.Fatalf("primary after undo = %#v, want %#v", got, want)
	}
}

func TestBufferUndoLastCursorFallsBackToDropWhenStackEmpty(t *testing.T) {
	b := NewBufferFromBytes([]byte("alpha beta\nalpha gamma\n"))
	b.SetSelection(Position{0, 0}, Position{0, 5})
	if !b.SelectAllOccurrences() {
		t.Fatal("SelectAllOccurrences failed")
	}
	b.cursorUndo = nil
	b.UndoLastCursor()
	if got := b.Selections.Count(); got != 1 {
		t.Fatalf("empty-stack undo count = %d, want 1", got)
	}
}
