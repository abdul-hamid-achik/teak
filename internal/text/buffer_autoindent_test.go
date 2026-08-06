package text

import (
	"strings"
	"testing"
)

var autoIndentBenchmarkRopeSink *Rope

const expectedMaxAutoIndentBytes = 64 << 10

func TestBufferInsertNewlineWithIndentUsesEachSelectionLine(t *testing.T) {
	b := NewBufferFromBytes([]byte("  alpha\n\tbeta\nplain"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 7}, Head: Position{Line: 0, Col: 7}},
		{Anchor: Position{Line: 1, Col: 5}, Head: Position{Line: 1, Col: 5}},
		{Anchor: Position{Line: 2, Col: 5}, Head: Position{Line: 2, Col: 5}},
	}, 1)
	beforeVersion := b.Version()

	b.InsertNewlineWithIndent()

	if got, want := b.Content(), "  alpha\n  \n\tbeta\n\t\nplain\n"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 1, Col: 2}, Head: Position{Line: 1, Col: 2}},
		{Anchor: Position{Line: 3, Col: 1}, Head: Position{Line: 3, Col: 1}},
		{Anchor: Position{Line: 5, Col: 0}, Head: Position{Line: 5, Col: 0}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{Line: 3, Col: 1}); got != want {
		t.Errorf("Cursor = %+v, want %+v", got, want)
	}
	if got, want := b.Version(), beforeVersion+1; got != want {
		t.Errorf("Version() = %d, want %d", got, want)
	}
	if change := b.LastChange(); change != nil {
		t.Errorf("LastChange() = %#v, want full sync for multicursor edit", change)
	}

	b.Undo()
	if got, want := b.Content(), "  alpha\n\tbeta\nplain"; got != want {
		t.Fatalf("Content() after one Undo = %q, want %q", got, want)
	}
}

func TestBufferInsertNewlineWithIndentReplacesEachSelectedRange(t *testing.T) {
	b := NewBufferFromBytes([]byte("  abc\n\txyz"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 2}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 1, Col: 4}, Head: Position{Line: 1, Col: 1}},
	}, 1)

	b.InsertNewlineWithIndent()

	if got, want := b.Content(), "  \n  \n\t\n\t"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 1, Col: 2}, Head: Position{Line: 1, Col: 2}},
		{Anchor: Position{Line: 3, Col: 1}, Head: Position{Line: 3, Col: 1}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
	if got, want := b.Cursor, (Position{Line: 3, Col: 1}); got != want {
		t.Errorf("Cursor = %+v, want %+v", got, want)
	}
}

func TestBufferInsertNewlineWithIndentKeepsIncrementalSingleEdit(t *testing.T) {
	b := NewBufferFromBytes([]byte("  abc"))
	b.SetCursor(Position{Line: 0, Col: 5})

	b.InsertNewlineWithIndent()

	if got, want := b.LastChange(), (&EditChange{
		StartLine: 0, StartCol: 5,
		EndLine: 0, EndCol: 5,
		Text: "\n  ",
	}); got == nil || *got != *want {
		t.Fatalf("LastChange() = %#v, want %#v", got, want)
	}
}

func TestBufferInsertNewlineWithIndentBoundsPathologicalPrefix(t *testing.T) {
	indent := strings.Repeat(" ", expectedMaxAutoIndentBytes+1)
	b := NewBufferFromBytes([]byte(indent + "x"))
	b.SetCursor(Position{Line: 0, Col: len(indent) + 1})

	b.InsertNewlineWithIndent()

	if got, want := b.Rope().Len(), len(indent)+len("x\n"); got != want {
		t.Fatalf("Rope().Len() = %d, want %d; oversized indentation must fall back to a plain newline", got, want)
	}
	if got, want := b.Cursor, (Position{Line: 1, Col: 0}); got != want {
		t.Errorf("Cursor = %+v, want %+v", got, want)
	}
}

func TestBufferInsertNewlineWithIndentBoundsGiantLineAllocation(t *testing.T) {
	const giantIndentBytes = 8 << 20
	source := NewFromString(strings.Repeat(" ", giantIndentBytes) + "x")

	result := testing.Benchmark(func(bench *testing.B) {
		for bench.Loop() {
			b := NewBufferFromRope(source)
			b.SetCursor(Position{Line: 0, Col: giantIndentBytes + 1})
			b.InsertNewlineWithIndent()
			autoIndentBenchmarkRopeSink = b.Rope()
		}
	})
	if got := result.AllocedBytesPerOp(); got > 512<<10 {
		t.Fatalf("auto-indent allocated %d B/op for an 8 MiB line; want below 512 KiB", got)
	}
}

func BenchmarkBufferInsertNewlineWithIndentGiantLine(b *testing.B) {
	const giantIndentBytes = 8 << 20
	source := NewFromString(strings.Repeat(" ", giantIndentBytes) + "x")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buffer := NewBufferFromRope(source)
		buffer.SetCursor(Position{Line: 0, Col: giantIndentBytes + 1})
		buffer.InsertNewlineWithIndent()
		autoIndentBenchmarkRopeSink = buffer.Rope()
	}
}

func selectionSlicesEqual(a, b []Selection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
