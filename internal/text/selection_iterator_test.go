package text

import "testing"

func TestSelectionLineIteratorWalksSortedSelectionsOnce(t *testing.T) {
	selections := NewSelections(Position{Line: 7, Col: 1})
	selections.Add(Selection{Anchor: Position{Line: 4, Col: 2}, Head: Position{Line: 5, Col: 3}})
	selections.Add(Selection{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 4}})
	selections.Add(Selection{Anchor: Position{Line: 5, Col: 4}, Head: Position{Line: 5, Col: 6}})

	it := selections.LineIterator()
	want := map[int][]Selection{
		1: {{Anchor: Position{Line: 1, Col: 1}, Head: Position{Line: 1, Col: 4}}},
		4: {{Anchor: Position{Line: 4, Col: 2}, Head: Position{Line: 5, Col: 3}}},
		5: {
			{Anchor: Position{Line: 4, Col: 2}, Head: Position{Line: 5, Col: 3}},
			{Anchor: Position{Line: 5, Col: 4}, Head: Position{Line: 5, Col: 6}},
		},
		7: {{Anchor: Position{Line: 7, Col: 1}, Head: Position{Line: 7, Col: 1}}},
	}
	for line := 0; line <= 8; line++ {
		got := it.ForLine(line)
		if len(got) != len(want[line]) {
			t.Fatalf("ForLine(%d) returned %d selections, want %d: %#v", line, len(got), len(want[line]), got)
		}
		for i := range got {
			if got[i] != want[line][i] {
				t.Errorf("ForLine(%d)[%d] = %#v, want %#v", line, i, got[i], want[line][i])
			}
		}
	}
}

func TestSelectionLineIteratorRepeatsSameLogicalLineForWrap(t *testing.T) {
	selections := NewSelections(Position{})
	selections.Add(Selection{Anchor: Position{Line: 2, Col: 1}, Head: Position{Line: 2, Col: 6}})

	it := selections.LineIterator()
	if got := it.ForLine(2); len(got) != 1 {
		t.Fatalf("first ForLine(2) = %#v, want one selection", got)
	}
	if got := it.ForLine(2); len(got) != 1 {
		t.Fatalf("second ForLine(2) = %#v, want one selection", got)
	}
}

func TestSelectionLineIteratorFreshIteratorReflectsSelectionChanges(t *testing.T) {
	selections := NewSelections(Position{Line: 1, Col: 0})
	if got := selections.LineIterator().ForLine(3); len(got) != 0 {
		t.Fatalf("initial iterator = %#v, want no selection on line 3", got)
	}

	selections.Add(Selection{Anchor: Position{Line: 3, Col: 1}, Head: Position{Line: 3, Col: 2}})
	if got := selections.LineIterator().ForLine(3); len(got) != 1 {
		t.Fatalf("fresh iterator after Add = %#v, want updated selection", got)
	}
}

func TestBufferRestoreSelectionsNormalizesSnapshotForLineIteration(t *testing.T) {
	buf := NewBufferFromBytes([]byte("abcdef\n"))
	buf.RestoreSelections([]Selection{
		{Anchor: Position{Line: 0, Col: 3}, Head: Position{Line: 0, Col: 5}},
		{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 4}},
	}, 1)

	got := buf.Selections.LineIterator().ForLine(0)
	if len(got) != 1 || got[0] != (Selection{Anchor: Position{Line: 0, Col: 1}, Head: Position{Line: 0, Col: 4}}) {
		t.Fatalf("restored iterator = %#v, want normalized first range", got)
	}
	if got, want := buf.Cursor, (Position{Line: 0, Col: 4}); got != want {
		t.Errorf("cursor = %#v, want %#v", got, want)
	}
}
