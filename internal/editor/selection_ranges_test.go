package editor

import (
	"math/rand/v2"
	"sort"
	"testing"

	"teak/internal/text"
)

func TestSelectionRangeIteratorMatchesReferenceForRandomNormalizedSelections(t *testing.T) {
	rng := rand.New(rand.NewPCG(41, 73))
	const lines = 100

	for trial := range 100 {
		buf := text.NewBufferFromBytes(makeSelectionTestDocument(lines, 80))
		selections := make([]text.Selection, 0, text.MaxSelections)
		for range text.MaxSelections {
			line := rng.IntN(lines)
			start := rng.IntN(75)
			end := start + rng.IntN(6)
			selections = append(selections, text.Selection{
				Anchor: text.Position{Line: line, Col: start},
				Head:   text.Position{Line: line, Col: end},
			})
		}
		// The rendering contract is the normalized non-overlapping model that
		// cursor commands expose to the viewport.
		buf.RestoreSelections(selections, 0)
		buf.Selections.Normalize()

		iterator := newSelectionRangeIterator(buf.Selections)
		for line := 0; line < lines; line++ {
			got := iterator.Ranges(line, 80)
			want := selectionRangesReference(buf, line, 80)
			if !sameSelectionRanges(got, want) {
				t.Fatalf("trial %d line %d: ranges = %#v, want %#v", trial, line, got, want)
			}
		}
	}
}

func TestSelectionRangeIteratorMatchesReferenceForMultilineSelections(t *testing.T) {
	buf := text.NewBufferFromBytes(makeSelectionTestDocument(8, 20))
	buf.RestoreSelections([]text.Selection{
		{Anchor: text.Position{Line: 5, Col: 4}, Head: text.Position{Line: 6, Col: 2}},
		{Anchor: text.Position{Line: 1, Col: 3}, Head: text.Position{Line: 3, Col: 4}},
		{Anchor: text.Position{Line: 3, Col: 5}, Head: text.Position{Line: 4, Col: 1}},
	}, 1)

	iterator := newSelectionRangeIterator(buf.Selections)
	for line := 0; line < buf.LineCount(); line++ {
		got := iterator.Ranges(line, 20)
		want := selectionRangesReference(buf, line, 20)
		if !sameSelectionRanges(got, want) {
			t.Fatalf("line %d: ranges = %#v, want %#v", line, got, want)
		}
	}
}

func makeSelectionTestDocument(lines, width int) []byte {
	data := make([]byte, 0, lines*(width+1))
	for range lines {
		for range width {
			data = append(data, 'x')
		}
		data = append(data, '\n')
	}
	return data
}

func selectionRangesReference(buf *text.Buffer, line, lineLen int) []selectionByteRange {
	ranges := make([]selectionByteRange, 0)
	for _, sel := range buf.Selections.All() {
		if sel.IsEmpty() {
			continue
		}
		start, end := sel.Ordered()
		if line < start.Line || line > end.Line {
			continue
		}
		startCol, endCol := 0, lineLen
		if line == start.Line {
			startCol = start.Col
		}
		if line == end.Line {
			endCol = end.Col
		}
		if startCol < endCol {
			ranges = append(ranges, selectionByteRange{start: startCol, end: endCol})
		}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	return ranges
}

func sameSelectionRanges(a, b []selectionByteRange) bool {
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
