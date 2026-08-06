package text

import (
	"strings"
	"testing"
)

var structuralEditBenchmarkRopeSink *Rope

func TestBufferIndentLinesTargetsEveryUniqueSelectedLine(t *testing.T) {
	b := NewBufferFromBytes([]byte("a\nb\nc\nd"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 2, Col: 0}},
		{Anchor: Position{Line: 3, Col: 0}, Head: Position{Line: 3, Col: 0}},
	}, 1)
	beforeVersion := b.Version()

	b.IndentLines(4)

	if got, want := b.Content(), "    a\n    b\nc\n    d"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 2, Col: 0}},
		{Anchor: Position{Line: 3, Col: 0}, Head: Position{Line: 3, Col: 0}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
	if got, want := b.Version(), beforeVersion+1; got != want {
		t.Errorf("Version() = %d, want %d", got, want)
	}
	b.Undo()
	if got, want := b.Content(), "a\nb\nc\nd"; got != want {
		t.Fatalf("Content() after one Undo = %q, want %q", got, want)
	}
}

func TestBufferDedentLinesTargetsSpacesTabsAndNoOpLines(t *testing.T) {
	b := NewBufferFromBytes([]byte("    a\n\tb\n  c\nd"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 5}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 1, Col: 2}, Head: Position{Line: 1, Col: 2}},
		{Anchor: Position{Line: 2, Col: 3}, Head: Position{Line: 2, Col: 3}},
		{Anchor: Position{Line: 3, Col: 0}, Head: Position{Line: 3, Col: 0}},
	}, 2)

	b.DedentLines(4)

	if got, want := b.Content(), "a\nb\nc\nd"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 1}},
		{Anchor: Position{Line: 2, Col: 1}, Head: Position{Line: 2, Col: 1}},
		{Anchor: Position{Line: 3, Col: 0}, Head: Position{Line: 3, Col: 0}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
}

func TestBufferDedentLinesNoOpDoesNotDirtyHistory(t *testing.T) {
	b := NewBufferFromBytes([]byte("alpha\nbeta"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 0}},
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
	}, 1)
	beforeVersion := b.Version()

	b.DedentLines(4)

	if b.Dirty() {
		t.Fatal("no-op dedent marked the buffer dirty")
	}
	if got := b.Version(); got != beforeVersion {
		t.Fatalf("Version() = %d, want unchanged %d", got, beforeVersion)
	}
	b.Undo()
	if got, want := b.Content(), "alpha\nbeta"; got != want {
		t.Fatalf("no-op dedent created Undo history: content = %q, want %q", got, want)
	}
}

func TestBufferToggleLineCommentTreatsIndependentCursorsAsBlocks(t *testing.T) {
	b := NewBufferFromBytes([]byte("  alpha\n\t// beta"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 2}, Head: Position{Line: 0, Col: 2}},
		{Anchor: Position{Line: 1, Col: 3}, Head: Position{Line: 1, Col: 3}},
	}, 1)

	b.ToggleLineComment("//")

	if got, want := b.Content(), "  // alpha\n\tbeta"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Line: 0, Col: 5}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 1}},
	}; !selectionSlicesEqual(got, want) {
		t.Errorf("Selections = %#v, want %#v", got, want)
	}
}

func TestBufferToggleLineCommentSkipsBlankLinesInSelectedBlock(t *testing.T) {
	b := NewBufferFromBytes([]byte("  alpha\n\n    beta"))
	b.SetSelection(Position{Line: 0, Col: 0}, Position{Line: 2, Col: 8})

	b.ToggleLineComment("//")

	if got, want := b.Content(), "  // alpha\n\n  //   beta"; got != want {
		t.Fatalf("Content() = %q, want %q", got, want)
	}
}

func TestBufferToggleLineCommentBoundsGiantIndentation(t *testing.T) {
	const giantIndentBytes = 8 << 20
	source := NewFromString(strings.Repeat(" ", giantIndentBytes) + "value")
	probe := NewBufferFromRope(source)
	probe.SetCursor(Position{Line: 0, Col: giantIndentBytes + len("value")})
	if got := probe.ToggleLineComment("//"); got != StructuralEditLimit {
		t.Fatalf("ToggleLineComment() = %v, want StructuralEditLimit", got)
	}
	if probe.Dirty() || probe.Version() != 0 || probe.Rope() != source {
		t.Fatal("over-budget comment edit changed the buffer")
	}

	result := testing.Benchmark(func(bench *testing.B) {
		for bench.Loop() {
			b := NewBufferFromRope(source)
			b.SetCursor(Position{Line: 0, Col: giantIndentBytes + len("value")})
			b.ToggleLineComment("//")
			structuralEditBenchmarkRopeSink = b.Rope()
		}
	})
	if got := result.AllocedBytesPerOp(); got > 512<<10 {
		t.Fatalf("comment toggle allocated %d B/op for an 8 MiB indentation; want below 512 KiB", got)
	}
}

func BenchmarkBufferToggleLineCommentGiantIndentation(b *testing.B) {
	const giantIndentBytes = 8 << 20
	source := NewFromString(strings.Repeat(" ", giantIndentBytes) + "value")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buffer := NewBufferFromRope(source)
		buffer.SetCursor(Position{Line: 0, Col: giantIndentBytes + len("value")})
		buffer.ToggleLineComment("//")
		structuralEditBenchmarkRopeSink = buffer.Rope()
	}
}
