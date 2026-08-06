package text

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var lineTransformBenchmarkSink *Rope

func TestPrepareLineTransformMovesIndependentCursors(t *testing.T) {
	const source = "a\nb\nc\nd"
	selections := []Selection{
		{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 1}},
		{Anchor: Position{Line: 3, Col: 1}, Head: Position{Line: 3, Col: 1}},
	}

	result, err := PrepareLineTransform(context.Background(), NewFromString(source), selections, 1, LineTransformMoveUp)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "b\na\nd\nc"; got != want {
		t.Fatalf("move up content = %q, want %q", got, want)
	}
	if got, want := result.Selections, []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 2, Col: 1}, Head: Position{Line: 2, Col: 1}},
	}; !selectionSlicesEqual(got, want) {
		t.Fatalf("move up selections = %#v, want %#v", got, want)
	}
}

func TestPrepareLineTransformMergesAdjacentMoveBlocks(t *testing.T) {
	selections := []Selection{
		{Anchor: Position{Line: 1, Col: 0}, Head: Position{Line: 1, Col: 0}},
		{Anchor: Position{Line: 2, Col: 0}, Head: Position{Line: 2, Col: 0}},
	}
	result, err := PrepareLineTransform(context.Background(), NewFromString("a\nb\nc\nd"), selections, 1, LineTransformMoveUp)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "b\nc\na\nd"; got != want {
		t.Fatalf("adjacent move content = %q, want %q", got, want)
	}
}

func TestPrepareLineTransformMovesDownAndDuplicatesUp(t *testing.T) {
	selections := []Selection{
		{Anchor: Position{Line: 0}, Head: Position{Line: 0}},
		{Anchor: Position{Line: 2}, Head: Position{Line: 2}},
	}
	result, err := PrepareLineTransform(context.Background(), NewFromString("a\nb\nc\nd"), selections, 0, LineTransformMoveDown)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "b\na\nd\nc"; got != want {
		t.Fatalf("move down content = %q, want %q", got, want)
	}

	result, err = PrepareLineTransform(context.Background(), NewFromString("a\nb\nc\nd"), selections, 0, LineTransformDuplicateUp)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "a\na\nb\nc\nc\nd"; got != want {
		t.Fatalf("duplicate up content = %q, want %q", got, want)
	}
}

func TestPrepareLineTransformDuplicatesIndependentCursors(t *testing.T) {
	selections := []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 2, Col: 1}, Head: Position{Line: 2, Col: 1}},
	}
	result, err := PrepareLineTransform(context.Background(), NewFromString("a\nb\nc\nd"), selections, 1, LineTransformDuplicateDown)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "a\na\nb\nc\nc\nd"; got != want {
		t.Fatalf("duplicate content = %q, want %q", got, want)
	}
	if got, want := result.Selections, []Selection{
		{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 1}},
		{Anchor: Position{Line: 4, Col: 1}, Head: Position{Line: 4, Col: 1}},
	}; !selectionSlicesEqual(got, want) {
		t.Fatalf("duplicate selections = %#v, want %#v", got, want)
	}
}

func TestPrepareLineTransformDeletesEverySelectedLineAsOneEdit(t *testing.T) {
	selections := []Selection{
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 1}},
		{Anchor: Position{Line: 2, Col: 1}, Head: Position{Line: 2, Col: 1}},
	}
	result, err := PrepareLineTransform(context.Background(), NewFromString("a\nb\nc\nd"), selections, 1, LineTransformDelete)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "b\nd"; got != want {
		t.Fatalf("delete content = %q, want %q", got, want)
	}
	if got := result.Primary; got != 1 {
		t.Fatalf("delete primary index = %d, want 1", got)
	}
}

func TestBufferLineTransformUndoIsAtomic(t *testing.T) {
	b := NewBufferFromBytes([]byte("a\nb\nc\nd"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 0}, Head: Position{Line: 0, Col: 0}},
		{Anchor: Position{Line: 2, Col: 0}, Head: Position{Line: 2, Col: 0}},
	}, 1)

	b.DeleteLine()
	b.Undo()
	if got, want := b.Content(), "a\nb\nc\nd"; got != want {
		t.Fatalf("Undo content = %q, want %q", got, want)
	}
}

func TestBufferDeleteEmptyLineIsNoOp(t *testing.T) {
	b := NewBufferFromBytes(nil)
	before := b.Version()
	b.DeleteLine()
	if b.Dirty() || b.Version() != before {
		t.Fatalf("empty delete changed state: dirty=%v version=%d", b.Dirty(), b.Version())
	}
}

func TestPrepareLineTransformDeletesLastAndTrailingEmptyLines(t *testing.T) {
	result, err := PrepareLineTransform(context.Background(), NewFromString("a\nb\nc"), []Selection{{Anchor: Position{Line: 2}, Head: Position{Line: 2}}}, 0, LineTransformDelete)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "a\nb"; got != want {
		t.Fatalf("delete last line = %q, want %q", got, want)
	}

	result, err = PrepareLineTransform(context.Background(), NewFromString("a\n"), []Selection{{Anchor: Position{Line: 1}, Head: Position{Line: 1}}}, 0, LineTransformDelete)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Rope.String(), "a"; got != want {
		t.Fatalf("delete trailing empty line = %q, want %q", got, want)
	}
}

func TestPrepareLineTransformHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareLineTransform(ctx, NewFromString("a\nb"), []Selection{{}}, 0, LineTransformMoveDown)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestPrepareLineTransformGiantLineDoesNotMaterializeSource(t *testing.T) {
	const giant = 8 << 20
	source := NewFromString(strings.Repeat("x", giant))
	result, err := PrepareLineTransform(context.Background(), source, []Selection{{Anchor: Position{Col: giant}, Head: Position{Col: giant}}}, 0, LineTransformDuplicateDown)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rope.Len() != giant*2+1 {
		t.Fatalf("duplicate giant line length = %d, want %d", result.Rope.Len(), giant*2+1)
	}
}

func BenchmarkPrepareLineTransformGiantDuplicateDown(b *testing.B) {
	const giant = 8 << 20
	source := NewFromString(strings.Repeat("x", giant))
	selections := []Selection{{Anchor: Position{Col: giant}, Head: Position{Col: giant}}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := PrepareLineTransform(context.Background(), source, selections, 0, LineTransformDuplicateDown)
		if err != nil {
			b.Fatal(err)
		}
		lineTransformBenchmarkSink = result.Rope
	}
}
