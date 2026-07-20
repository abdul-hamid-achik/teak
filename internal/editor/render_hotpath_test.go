package editor

import (
	"strings"
	"testing"

	"teak/internal/text"
)

func TestDiagnosticSeveritiesForVisibleLinesDoesNotExpandRanges(t *testing.T) {
	got := diagnosticSeveritiesForVisibleLines([]Diagnostic{
		{StartLine: 0, EndLine: 1_000_000, Severity: 2},
		{StartLine: 20, EndLine: 30, Severity: 1},
	}, []int{5, 21, 999_999})

	want := map[int]int{5: 2, 21: 1, 999_999: 2}
	if len(got) != len(want) {
		t.Fatalf("diagnostic map length = %d, want %d: %#v", len(got), len(want), got)
	}
	for line, severity := range want {
		if got[line] != severity {
			t.Errorf("severity for line %d = %d, want %d", line, got[line], severity)
		}
	}
}

func TestFindMatchingBracketWithinBudgetDegradesWithoutMaterializingDocument(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("(" + strings.Repeat("x", 128*1024) + ")"))

	if _, found := FindMatchingBracketWithinBudget(buf, text.Position{}, 1024); found {
		t.Fatal("match beyond the scan budget must not be reported")
	}
	match, found := FindMatchingBracketWithinBudget(buf, text.Position{}, 256*1024)
	if !found {
		t.Fatal("match within the scan budget was not found")
	}
	if want := 128*1024 + 1; match != (text.Position{Line: 0, Col: want}) {
		t.Fatalf("match = %+v, want column %d", match, want)
	}
}

func TestFoldStateBuildsAndReusesVisibleIndex(t *testing.T) {
	fs := FoldState{}
	fs.SetRegions([]FoldRegion{
		{StartLine: 10, EndLine: 30, Collapsed: true},
		{StartLine: 20, EndLine: 40, Collapsed: true},
		{StartLine: 60, EndLine: 70, Collapsed: true},
		{StartLine: 80, EndLine: 90, Collapsed: false},
	})

	if got, want := fs.TotalVisibleLines(100), 60; got != want {
		t.Fatalf("TotalVisibleLines = %d, want %d", got, want)
	}
	firstIndex := fs.index
	if firstIndex == nil {
		t.Fatal("fold index was not built")
	}
	if got, want := fs.VisibleLines(8, 5, 100), []int{8, 9, 10, 41, 42}; !equalInts(got, want) {
		t.Fatalf("VisibleLines = %v, want %v", got, want)
	}
	if fs.index != firstIndex {
		t.Fatal("unchanged fold regions rebuilt their visible index")
	}
	if got, want := fs.VisualLineToBuffer(11, 100), 41; got != want {
		t.Fatalf("VisualLineToBuffer = %d, want %d", got, want)
	}
	if got, want := fs.BufferLineToVisual(41, 100), 11; got != want {
		t.Fatalf("BufferLineToVisual = %d, want %d", got, want)
	}

	fs.Unfold(10)
	if fs.index == nil || fs.index == firstIndex {
		t.Fatal("fold mutation did not replace the visible index")
	}
	if got, want := fs.TotalVisibleLines(100), 70; got != want {
		t.Fatalf("TotalVisibleLines after unfold = %d, want %d", got, want)
	}
}

func TestFoldStateIndexedQueriesMatchRegionSemantics(t *testing.T) {
	regions := []FoldRegion{
		{StartLine: 8, EndLine: 16, Collapsed: true},
		{StartLine: 3, EndLine: 12, Collapsed: false},
		{StartLine: 14, EndLine: 22, Collapsed: true},
		{StartLine: 25, EndLine: 25, Collapsed: true},
	}
	var fs FoldState
	fs.SetRegions(regions)
	const totalLines = 30

	wantVisible := make([]int, 0, totalLines)
	for line := 0; line < totalLines; line++ {
		wantHidden := false
		for _, region := range regions {
			wantHidden = wantHidden || (region.Collapsed && line > region.StartLine && line <= region.EndLine)
		}
		if got := fs.IsLineHidden(line); got != wantHidden {
			t.Errorf("IsLineHidden(%d) = %v, want %v", line, got, wantHidden)
		}
		if !wantHidden {
			wantVisible = append(wantVisible, line)
		}
	}
	if got := fs.TotalVisibleLines(totalLines); got != len(wantVisible) {
		t.Fatalf("TotalVisibleLines = %d, want %d", got, len(wantVisible))
	}
	for visual, line := range wantVisible {
		if got := fs.VisualLineToBuffer(visual, totalLines); got != line {
			t.Errorf("VisualLineToBuffer(%d) = %d, want %d", visual, got, line)
		}
		if got := fs.BufferLineToVisual(line, totalLines); got != visual {
			t.Errorf("BufferLineToVisual(%d) = %d, want %d", line, got, visual)
		}
	}
}

func equalInts(a, b []int) bool {
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
