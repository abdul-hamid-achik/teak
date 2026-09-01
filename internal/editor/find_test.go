package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/text"
	"teak/internal/ui"
)

func newTestBuffer(content string) *text.Buffer {
	return text.NewBufferFromRope(text.NewFromString(content))
}

func TestFindModelShowHide(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	if f.Visible() {
		t.Error("find should start hidden")
	}
	f.Show()
	if !f.Visible() {
		t.Error("find should be visible after Show")
	}
	f.Hide()
	if f.Visible() {
		t.Error("find should be hidden after Hide")
	}
}

func TestFindModelMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("hello world\nhello again\nnothing here")
	f.Show()
	f.input.SetValue("hello")
	f.updateMatches(buf)

	if f.MatchCount() != 2 {
		t.Errorf("expected 2 matches, got %d", f.MatchCount())
	}
	if f.CurrentMatch() != 0 {
		t.Errorf("expected current match 0, got %d", f.CurrentMatch())
	}

	m := f.CurrentMatchPosition()
	if m == nil {
		t.Fatal("expected current match position")
	}
	if m.Start.Line != 0 || m.Start.Col != 0 {
		t.Errorf("expected match at 0:0, got %d:%d", m.Start.Line, m.Start.Col)
	}
}

func TestFindModelRegex(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("foo123bar foo456bar")
	f.Show()
	f.regex = true
	f.input.SetValue(`foo\d+bar`)
	f.updateMatches(buf)

	if f.MatchCount() != 2 {
		t.Errorf("expected 2 regex matches, got %d", f.MatchCount())
	}
}

func TestFindModelNoMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("hello world")
	f.Show()
	f.input.SetValue("xyz")
	f.updateMatches(buf)

	if f.MatchCount() != 0 {
		t.Errorf("expected 0 matches, got %d", f.MatchCount())
	}
	if f.CurrentMatch() != -1 {
		t.Errorf("expected current match -1, got %d", f.CurrentMatch())
	}
}

func TestFindModelMatchRangesForLine(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("aaa bbb aaa\nccc aaa")
	f.Show()
	f.input.SetValue("aaa")
	f.updateMatches(buf)

	ranges0 := f.FindMatchRangesForLine(0)
	if len(ranges0) != 2 {
		t.Errorf("expected 2 ranges on line 0, got %d", len(ranges0))
	}

	ranges1 := f.FindMatchRangesForLine(1)
	if len(ranges1) != 1 {
		t.Errorf("expected 1 range on line 1, got %d", len(ranges1))
	}

	ranges2 := f.FindMatchRangesForLine(2)
	if len(ranges2) != 0 {
		t.Errorf("expected 0 ranges on line 2, got %d", len(ranges2))
	}
}

func TestFindModelView(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	if f.View() != "" {
		t.Error("hidden find should render empty")
	}
	f.Show()
	view := f.View()
	if view == "" {
		t.Error("visible find should render non-empty")
	}
}

func TestFindModelMaxMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	// Create a buffer with many matches
	content := ""
	for i := 0; i < 12000; i++ {
		content += "x\n"
	}
	buf := newTestBuffer(content)
	f.Show()
	f.input.SetValue("x")
	f.updateMatches(buf)

	if f.MatchCount() != 10000 {
		t.Errorf("expected matches capped at 10000, got %d", f.MatchCount())
	}
}

func TestFindMatchesBeyondFormerEightMiBCap(t *testing.T) {
	const formerLimit = 8 << 20
	rope := text.NewFromString(strings.Repeat("a", formerLimit+1) + "needle")

	matches, _, err := findMatches(rope, "needle", false, text.Position{})
	if err != nil {
		t.Fatalf("findMatches() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("findMatches() matches = %d, want one beyond the former 8 MiB cap", len(matches))
	}
	if got, want := matches[0].Start.Col, formerLimit+1; got != want {
		t.Fatalf("match column = %d, want %d", got, want)
	}
}

func BenchmarkFindMatchesDenseSingleLine(b *testing.B) {
	rope := text.NewFromString(strings.Repeat("x", 1<<20))
	b.ReportAllocs()
	for b.Loop() {
		matches, _, err := findMatches(rope, "x", false, text.Position{})
		if err != nil || len(matches) != maxFindMatches {
			b.Fatalf("findMatches() = %d matches, error %v", len(matches), err)
		}
	}
}

// --- F5a: seeding the query from the primary selection ---

func TestFindModelSeedFromSelection(t *testing.T) {
	buf := newTestBuffer("foo bar foo\nfoo baz")
	buf.SetSelection(text.Position{Line: 0, Col: 8}, text.Position{Line: 0, Col: 11})
	f := NewFindModel(ui.DefaultTheme())

	if !f.SeedFromSelection(buf) {
		t.Fatal("SeedFromSelection rejected a non-empty single-line selection")
	}
	if got := f.input.Value(); got != "foo" {
		t.Fatalf("seeded input = %q, want %q", got, "foo")
	}
	f.updateMatches(buf)
	if got := f.MatchCount(); got != 3 {
		t.Fatalf("seeded matches = %d, want 3 from the initial scan", got)
	}
	if got := f.CurrentMatch(); got != 2 {
		t.Fatalf("seeded current match = %d, want 2 (first match at or after the cursor)", got)
	}
}

func TestFindModelSeedFromSelectionRejectsEmptyAndMultiline(t *testing.T) {
	buf := newTestBuffer("one two\nthree")
	f := NewFindModel(ui.DefaultTheme())
	f.input.SetValue("kept")
	f.query = "kept"

	buf.SetSelection(text.Position{Line: 0, Col: 4}, text.Position{Line: 0, Col: 4})
	if f.SeedFromSelection(buf) {
		t.Error("SeedFromSelection accepted an empty selection")
	}

	buf.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 1, Col: 5})
	if f.SeedFromSelection(buf) {
		t.Error("SeedFromSelection accepted a multiline selection")
	}

	if got := f.input.Value(); got != "kept" {
		t.Fatalf("input after rejected seeds = %q, want untouched %q", got, "kept")
	}
	if got := f.MatchCount(); got != 0 {
		t.Fatalf("matches after rejected seeds = %d, want 0", got)
	}
}

func TestFindModelSeedFromSelectionEscapesRegexMetacharacters(t *testing.T) {
	buf := newTestBuffer("a.b ab")
	buf.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 3})
	f := NewFindModel(ui.DefaultTheme())
	f.regex = true

	if !f.SeedFromSelection(buf) {
		t.Fatal("SeedFromSelection rejected a non-empty single-line selection")
	}
	f.updateMatches(buf)
	if got := f.MatchCount(); got != 1 {
		t.Fatalf("seeded regex-mode matches = %d, want 1 (the selection must match literally)", got)
	}
	if f.errMsg != "" {
		t.Fatalf("seeded regex-mode query failed to compile: %s", f.errMsg)
	}
}

func TestFindModelSeedFromSelectionNilSelections(t *testing.T) {
	buf := newTestBuffer("one two")
	buf.Selections = nil
	f := NewFindModel(ui.DefaultTheme())

	if f.SeedFromSelection(buf) {
		t.Fatal("SeedFromSelection accepted a buffer without selections")
	}
}

// --- F5b: origin capture and navigation tracking ---

func TestFindModelNavigationMarksVisited(t *testing.T) {
	buf := newTestBuffer("needle\nneedle")
	f := NewFindModel(ui.DefaultTheme())
	f.Show()
	f.input.SetValue("needle")
	f.updateMatches(buf)

	next, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, buf)
	if !next.visited {
		t.Fatal("stepping to the next match did not mark the session as visited")
	}

	back, _ := next.Update(tea.KeyPressMsg{Code: tea.KeyF3, Mod: tea.ModShift}, buf)
	if !back.visited {
		t.Fatal("stepping backwards lost the visited flag")
	}
}

func TestFindModelEnterWithoutMatchesDoesNotMarkVisited(t *testing.T) {
	buf := newTestBuffer("needle")
	f := NewFindModel(ui.DefaultTheme())
	f.Show()
	f.input.SetValue("absent")
	f.updateMatches(buf)

	next, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, buf)
	if next.visited {
		t.Fatal("Enter with no matches marked the session as visited")
	}
}

func TestFindModelOriginCaptureAndReset(t *testing.T) {
	buf := newTestBuffer("one two\nthree")
	buf.SetSelection(text.Position{Line: 1, Col: 2}, text.Position{Line: 1, Col: 5})
	f := NewFindModel(ui.DefaultTheme())

	f.CaptureOrigin(buf)

	if !f.origin.valid {
		t.Fatal("CaptureOrigin did not mark the origin as valid")
	}
	if got := f.origin.cursor; got != (text.Position{Line: 1, Col: 5}) {
		t.Fatalf("origin cursor = %+v, want the selection head", got)
	}
	if len(f.origin.selections) != 1 || f.origin.selections[0].Anchor != (text.Position{Line: 1, Col: 2}) {
		t.Fatalf("origin selections = %#v, want a copy of the buffer selection", f.origin.selections)
	}
	// The snapshot must not alias live buffer state.
	buf.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 3})
	if f.origin.selections[0].Head != (text.Position{Line: 1, Col: 5}) {
		t.Fatalf("origin snapshot aliased the buffer selections: %#v", f.origin.selections)
	}

	f.visited = true
	f.Hide()

	if f.origin.valid {
		t.Fatal("Hide kept a stale origin snapshot")
	}
	if f.visited {
		t.Fatal("Hide kept the visited flag")
	}
}

func TestFormatMatchCountKeepsPositionAtCap(t *testing.T) {
	tests := []struct {
		name    string
		current int
		total   int
		want    string
	}{
		{"under cap", 3, 12, "3/12"},
		{"exactly at cap", 3, 999, "3/999"},
		{"over cap keeps position", 5, 1200, "5/999+"},
		{"over cap large current", 1042, 5000, "1042/999+"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMatchCount(tt.current, tt.total); got != tt.want {
				t.Fatalf("formatMatchCount(%d, %d) = %q, want %q", tt.current, tt.total, got, tt.want)
			}
		})
	}
}
