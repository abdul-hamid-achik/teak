package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestIndentGuideRanges(t *testing.T) {
	got := indentGuideRanges(0, []byte("        foo"), 4, ui.DefaultTheme().IndentGuide)
	if len(got) != 2 {
		t.Fatalf("guides = %d, want 2 (columns 4 and 8)", len(got))
	}
	if got[0].StartCol != 3 || got[1].StartCol != 7 {
		t.Fatalf("guide cols = %d,%d want 3,7", got[0].StartCol, got[1].StartCol)
	}
}

func TestTrailingWSRange(t *testing.T) {
	got, ok := trailingWSRange(2, []byte("ok  "), ui.DefaultTheme().TrailingWS)
	if !ok || got.StartCol != 2 || got.EndCol != 4 {
		t.Fatalf("trailing = %+v ok=%v", got, ok)
	}
	if _, ok := trailingWSRange(0, []byte("clean"), ui.DefaultTheme().TrailingWS); ok {
		t.Fatal("clean line should not highlight trailing whitespace")
	}
}

func TestRulerRange(t *testing.T) {
	line := []byte("0123456789abcdef")
	got, ok := rulerRange(0, line, 8, 4, ui.DefaultTheme().Ruler)
	if !ok || got.StartCol != 8 {
		t.Fatalf("ruler = %+v ok=%v", got, ok)
	}
	if _, ok := rulerRange(0, []byte("short"), 80, 4, ui.DefaultTheme().Ruler); ok {
		t.Fatal("ruler past end of line should be skipped")
	}
}

func TestDecorationHighlightsVisibleLinesOnly(t *testing.T) {
	ed := New(text.NewBufferFromBytes([]byte("    a\n    b  \n    c\n")), ui.DefaultTheme(), DefaultConfig())
	ed.Config.IndentGuides = true
	ed.Config.HighlightTrailingWS = true
	ed.Config.RulerColumn = 0
	got := ed.decorationHighlights([]int{1}, 0, 2)
	var sawTrail bool
	for _, r := range got {
		if r.Line != 1 {
			t.Fatalf("decorated off-screen line %d", r.Line)
		}
		if r.StartCol >= 4 {
			sawTrail = true
		}
	}
	if !sawTrail {
		t.Fatal("expected trailing whitespace highlight on line 1")
	}
}

func TestGutterOptsMergesGitWithoutDebug(t *testing.T) {
	ed := New(text.NewBufferFromBytes([]byte("x\n")), ui.DefaultTheme(), DefaultConfig())
	ed.Config.GitGutter = true
	ed.GitLines = map[int]GitLineKind{0: GitLineAdded}
	opts := ed.gutterOpts()
	if opts == nil || !opts.ShowGit || opts.GitLines[0] != GitLineAdded {
		t.Fatalf("gutterOpts = %+v", opts)
	}
	if opts.Breakpoints != nil {
		t.Fatal("git-only gutter should not reserve the debug column")
	}
}

func TestGutterOptsHiddenWhenDisabled(t *testing.T) {
	ed := New(text.NewBufferFromBytes([]byte("x\n")), ui.DefaultTheme(), DefaultConfig())
	ed.Config.GitGutter = false
	ed.GitLines = map[int]GitLineKind{0: GitLineAdded}
	if ed.gutterOpts() != nil {
		t.Fatal("disabled git gutter still produced opts")
	}
}

func TestGitOnlyGutterMetricsSkipBreakpointColumn(t *testing.T) {
	metrics := computeGutterMetrics(10, &GutterOpts{ShowGit: true}, false)
	if metrics.markerWidth != 0 || metrics.gitWidth != 1 {
		t.Fatalf("metrics marker=%d git=%d", metrics.markerWidth, metrics.gitWidth)
	}
}
