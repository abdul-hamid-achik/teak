package text

import (
	"math/rand"
	"strings"
	"testing"
)

var selectionEditRopeSink *Rope

func TestBufferApplySelectionEditsRebasesDifferentReplacements(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcd"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Col: 1}, Head: Position{Col: 1}},
		{Anchor: Position{Col: 3}, Head: Position{Col: 3}},
	}, 1)

	changed := b.ApplySelectionEdits([]EditOp{
		{Offset: 1, Delete: 1, Insert: []byte("XY"), Cursor: 2},
		{Offset: 3, Insert: []byte("!"), Cursor: 4},
	})
	if !changed {
		t.Fatal("ApplySelectionEdits() reported no document change")
	}
	if got, want := b.Content(), "aXYc!d"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := b.Selections.All(), []Selection{
		{Anchor: Position{Col: 2}, Head: Position{Col: 2}},
		{Anchor: Position{Col: 5}, Head: Position{Col: 5}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("selections = %#v, want %#v", got, want)
	}
	if b.LastChange() != nil {
		t.Fatalf("LastChange() = %#v, want full-sync fallback", b.LastChange())
	}
	b.Undo()
	if got, want := b.Content(), "abcd"; got != want {
		t.Fatalf("one undo content = %q, want %q", got, want)
	}
}

func TestBufferApplySelectionEditsCanMoveWithoutEditing(t *testing.T) {
	b := NewBufferFromBytes([]byte("()"))
	b.SetCursor(Position{Col: 1})
	version := b.Version()

	changed := b.ApplySelectionEdits([]EditOp{{Offset: 1, Cursor: 2}})
	if changed {
		t.Fatal("cursor-only operation reported a document change")
	}
	if got, want := b.Cursor, (Position{Col: 2}); got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	if b.Version() != version || b.Dirty() {
		t.Fatalf("cursor-only operation changed version or dirty state")
	}
	if b.undo.CanUndo() {
		t.Fatal("cursor-only operation created an undo snapshot")
	}
}

func TestBufferApplySelectionEditsRejectsOverlapAtomically(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcd"))
	b.RestoreSelections([]Selection{
		{Anchor: Position{Col: 1}, Head: Position{Col: 1}},
		{Anchor: Position{Col: 2}, Head: Position{Col: 2}},
	}, 1)
	beforeSelections := append([]Selection(nil), b.Selections.All()...)

	changed := b.ApplySelectionEdits([]EditOp{
		{Offset: 1, Delete: 2, Cursor: 1},
		{Offset: 2, Delete: 1, Cursor: 2},
	})
	if changed {
		t.Fatal("overlapping edits reported a document change")
	}
	if got, want := b.Content(), "abcd"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got := b.Selections.All(); len(got) != len(beforeSelections) || got[0] != beforeSelections[0] || got[1] != beforeSelections[1] {
		t.Fatalf("selections = %#v, want %#v", got, beforeSelections)
	}
	if b.undo.CanUndo() {
		t.Fatal("rejected edits created an undo snapshot")
	}
}

func TestBufferApplySingleSelectionEditRecordsIncrementalChange(t *testing.T) {
	b := NewBufferFromBytes([]byte("abcd"))
	b.SetSelection(Position{Col: 1}, Position{Col: 3})

	if !b.ApplySelectionEdits([]EditOp{{Offset: 1, Delete: 2, Insert: []byte("X"), Cursor: 2}}) {
		t.Fatal("single edit reported no change")
	}
	if got, want := b.Content(), "aXd"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := b.LastChange(), (&EditChange{
		StartLine: 0, StartCol: 1,
		EndLine: 0, EndCol: 3,
		Text: "X",
	}); got == nil || *got != *want {
		t.Fatalf("LastChange() = %#v, want %#v", got, want)
	}
}

func TestBufferApplySelectionEditsMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iteration := 0; iteration < 300; iteration++ {
		docLen := 24 + rng.Intn(96)
		document := make([]byte, docLen)
		for i := range document {
			document[i] = byte('a' + rng.Intn(26))
		}

		count := 1 + rng.Intn(8)
		ops := make([]EditOp, 0, count)
		selections := make([]Selection, 0, count)
		nextStart := 0
		for len(ops) < count && nextStart <= docLen {
			remainingSlots := count - len(ops)
			maxStart := docLen - remainingSlots + 1
			if nextStart > maxStart {
				break
			}
			start := nextStart
			if room := maxStart - nextStart; room > 0 {
				start += rng.Intn(min(room, 3) + 1)
			}
			maxDelete := min(3, docLen-start-(remainingSlots-1))
			deleteBytes := 0
			if maxDelete > 0 {
				deleteBytes = rng.Intn(maxDelete + 1)
			}
			insert := make([]byte, rng.Intn(4))
			for i := range insert {
				insert[i] = byte('A' + rng.Intn(26))
			}
			cursor := start
			if len(insert) > 0 {
				cursor += rng.Intn(len(insert) + 1)
			}
			ops = append(ops, EditOp{Offset: start, Delete: deleteBytes, Insert: insert, Cursor: cursor})
			selections = append(selections, Selection{Anchor: Position{Col: start}, Head: Position{Col: start}})
			nextStart = start + max(deleteBytes, 1)
		}
		if len(ops) == 0 {
			continue
		}
		primary := rng.Intn(len(ops))
		buffer := NewBufferFromBytes(document)
		buffer.RestoreSelections(selections, primary)

		wantContent := append([]byte(nil), document...)
		for i := len(ops) - 1; i >= 0; i-- {
			op := ops[i]
			replaced := make([]byte, 0, len(wantContent)-op.Delete+len(op.Insert))
			replaced = append(replaced, wantContent[:op.Offset]...)
			replaced = append(replaced, op.Insert...)
			replaced = append(replaced, wantContent[op.Offset+op.Delete:]...)
			wantContent = replaced
		}
		wantSelections := make([]Selection, len(ops))
		shift := 0
		for i, op := range ops {
			pos := Position{Col: op.Cursor + shift}
			wantSelections[i] = Selection{Anchor: pos, Head: pos}
			shift += len(op.Insert) - op.Delete
		}
		wantBuffer := NewBufferFromBytes(wantContent)
		wantBuffer.RestoreSelections(wantSelections, primary)

		changed := buffer.ApplySelectionEdits(ops)
		wantChanged := false
		for _, op := range ops {
			wantChanged = wantChanged || op.Delete > 0 || len(op.Insert) > 0
		}
		if changed != wantChanged {
			t.Fatalf("iteration %d: changed = %v, want %v", iteration, changed, wantChanged)
		}
		if got := buffer.Content(); got != string(wantContent) {
			t.Fatalf("iteration %d: content = %q, want %q", iteration, got, wantContent)
		}
		gotSelections := buffer.Selections.All()
		wantNormalized := wantBuffer.Selections.All()
		if len(gotSelections) != len(wantNormalized) {
			t.Fatalf("iteration %d: selection count = %d, want %d", iteration, len(gotSelections), len(wantNormalized))
		}
		for i := range gotSelections {
			if gotSelections[i] != wantNormalized[i] {
				t.Fatalf("iteration %d selection %d = %#v, want %#v", iteration, i, gotSelections[i], wantNormalized[i])
			}
		}
		if got, want := buffer.Selections.PrimaryIndex(), wantBuffer.Selections.PrimaryIndex(); got != want {
			t.Fatalf("iteration %d: primary = %d, want %d", iteration, got, want)
		}
	}
}

func BenchmarkBufferApplySelectionEdits1000Cursors(b *testing.B) {
	content := strings.Repeat("x ", MaxSelections)
	snapshot := NewFromString(content)
	selections := make([]Selection, MaxSelections)
	ops := make([]EditOp, MaxSelections)
	for i := range MaxSelections {
		offset := i * 2
		pos := Position{Col: offset}
		selections[i] = Selection{Anchor: pos, Head: pos}
		ops[i] = EditOp{Offset: offset, Insert: []byte("()"), Cursor: offset + 1}
	}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		buffer := NewBufferFromRope(snapshot)
		buffer.RestoreSelections(selections, len(selections)-1)
		b.StartTimer()
		buffer.ApplySelectionEdits(ops)
		selectionEditRopeSink = buffer.Rope()
	}
}
