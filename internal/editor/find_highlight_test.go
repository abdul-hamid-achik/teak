package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

var benchmarkFindHighlightsSink []HighlightRange

func findHighlightTestEditor(t *testing.T) Editor {
	t.Helper()
	buf := text.NewBufferFromBytes([]byte("foo bar foo\nbaz\nfoo\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.ShowFind()
	for _, ch := range "foo" {
		var cmd tea.Cmd
		ed, cmd = ed.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		_ = cmd
		// Drain the debounced scan chain: tick -> scan command -> results.
		tick := FindDebounceMsg{EditorID: ed.id, Generation: ed.find.Generation()}
		var scanCmd tea.Cmd
		ed, scanCmd = ed.Update(tick)
		if scanCmd != nil {
			if results, ok := scanCmd().(FindResultsMsg); ok {
				ed, _ = ed.Update(results)
			}
		}
	}
	if !ed.IsFindVisible() {
		t.Fatal("find widget not visible after typing the query")
	}
	if ed.find.MatchCount() != 3 {
		t.Fatalf("match count after typing %q = %d, want 3", "foo", ed.find.MatchCount())
	}
	return ed
}

func TestFindMatchHighlightsCoverVisibleMatches(t *testing.T) {
	ed := findHighlightTestEditor(t)

	highlights := ed.findMatchHighlights()
	if len(highlights) != 3 {
		t.Fatalf("findMatchHighlights() = %d ranges, want 3 (two on line 0, one on line 2)", len(highlights))
	}
	wantLines := []int{0, 0, 2}
	for i, h := range highlights {
		if h.Line != wantLines[i] {
			t.Fatalf("highlight %d on line %d, want line %d", i, h.Line, wantLines[i])
		}
	}
	// Line 0: "foo bar foo" — matches at bytes 0-3 and 8-11.
	if highlights[0].StartCol != 0 || highlights[0].EndCol != 3 {
		t.Errorf("first match = %d-%d, want 0-3", highlights[0].StartCol, highlights[0].EndCol)
	}
	if highlights[1].StartCol != 8 || highlights[1].EndCol != 11 {
		t.Errorf("second match = %d-%d, want 8-11", highlights[1].StartCol, highlights[1].EndCol)
	}
}

func TestFindMatchHighlightsDistinguishCurrentMatch(t *testing.T) {
	ed := findHighlightTestEditor(t)

	current := ed.find.CurrentMatchPosition()
	if current == nil {
		t.Fatal("no current match after entering the query")
	}
	theme := ui.DefaultTheme()
	wantCurrent := theme.FindMatchCurrent.Render("x")
	wantOther := theme.FindMatch.Render("x")
	var sawCurrent bool
	for _, h := range ed.findMatchHighlights() {
		got := h.Style.Render("x")
		isCurrent := h.StartCol == current.Start.Col && h.EndCol == current.End.Col && h.Line == current.Start.Line
		if isCurrent {
			sawCurrent = true
			if got != wantCurrent {
				t.Errorf("current match style render = %q, want %q", got, wantCurrent)
			}
		} else if got != wantOther {
			t.Errorf("non-current match style render = %q, want %q", got, wantOther)
		}
	}
	if !sawCurrent {
		t.Fatal("no highlight matched the current match position")
	}
}

func TestFindMatchHighlightsBoundedToViewport(t *testing.T) {
	ed := findHighlightTestEditor(t)
	ed.SetSize(40, 1)
	ed.Viewport.ScrollY = 2 // only the last line ("foo") is visible

	highlights := ed.findMatchHighlights()
	if len(highlights) != 1 {
		t.Fatalf("findMatchHighlights() with scrolled viewport = %d ranges, want 1", len(highlights))
	}
	if highlights[0].Line != 2 {
		t.Fatalf("highlight on line %d, want line 2", highlights[0].Line)
	}
}

func TestFindMatchHighlightsExcludeCollapsedLines(t *testing.T) {
	const lineCount = 100
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("x\n", lineCount)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 3)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: 0, EndLine: 97, Collapsed: true}})
	ed.find.visible = true
	ed.find.matches = make([]FindMatch, lineCount)
	for line := range lineCount {
		ed.find.matches[line] = FindMatch{
			Start: text.Position{Line: line},
			End:   text.Position{Line: line, Col: 1},
		}
	}

	highlights := ed.findMatchHighlights()
	if len(highlights) != 3 {
		t.Fatalf("findMatchHighlights() = %d ranges, want only 3 visible matches", len(highlights))
	}
	for i, wantLine := range []int{0, 98, 99} {
		if highlights[i].Line != wantLine {
			t.Fatalf("highlight %d is on line %d, want visible line %d", i, highlights[i].Line, wantLine)
		}
	}
}

func TestFindMatchHighlightsEmptyWhenWidgetHidden(t *testing.T) {
	ed := findHighlightTestEditor(t)
	ed.HideFind()
	if highlights := ed.findMatchHighlights(); len(highlights) != 0 {
		t.Fatalf("findMatchHighlights() with hidden widget = %d ranges, want 0", len(highlights))
	}
}

func TestViewIncludesFindHighlights(t *testing.T) {
	ed := findHighlightTestEditor(t)

	view := ed.View()
	if len(view) == 0 {
		t.Fatal("empty view")
	}
	// The plain (unhighlighted) document would render "foo" with no styling;
	// with find highlights the view must differ from one rendered with the
	// widget hidden.
	ed.HideFind()
	plain := ed.View()
	if view == plain {
		t.Fatal("view with find highlights identical to plain view — matches are not rendered")
	}
}

func BenchmarkFindMatchHighlightsDeepViewport(b *testing.B) {
	const lineCount = maxFindMatches
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("x\n", lineCount)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Viewport.ScrollY = lineCount - 10
	ed.find.visible = true
	ed.find.matches = make([]FindMatch, lineCount)
	for line := range lineCount {
		ed.find.matches[line] = FindMatch{
			Start: text.Position{Line: line},
			End:   text.Position{Line: line, Col: 1},
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		if got := len(ed.findMatchHighlights()); got != 10 {
			b.Fatalf("visible highlights = %d, want 10", got)
		}
	}
}

func BenchmarkFindMatchHighlightsCollapsedTenThousand(b *testing.B) {
	const lineCount = maxFindMatches
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("x\n", lineCount)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: 0, EndLine: lineCount - 10, Collapsed: true}})
	ed.find.visible = true
	ed.find.matches = make([]FindMatch, lineCount)
	for line := range lineCount {
		ed.find.matches[line] = FindMatch{
			Start: text.Position{Line: line},
			End:   text.Position{Line: line, Col: 1},
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		benchmarkFindHighlightsSink = ed.findMatchHighlights()
	}
}
