package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestEditorWrapDebugGutterCursorRoundTrip(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("abcdefghijklmnopqrstuvwxyz"))
	cfg := DefaultConfig()
	cfg.WordWrap = true

	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.DebugGutter = &GutterOpts{Breakpoints: map[int]BreakpointState{}}
	ed.SetSize(12, 6)
	ed.Buffer.Cursor = text.Position{Line: 0, Col: 7}

	x, y := ed.CursorPosition()
	if y <= 0 {
		t.Fatalf("expected wrapped cursor row, got y=%d", y)
	}

	pos := ed.screenToBuffer(x, y)
	if pos != ed.Buffer.Cursor {
		t.Fatalf("screenToBuffer(%d,%d) = %+v, want %+v", x, y, pos, ed.Buffer.Cursor)
	}
}

func TestEditorWrapIgnoresFoldGutterMetrics(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("abcdefghijklmnopqrstuvwxyz"))
	cfg := DefaultConfig()
	cfg.WordWrap = true

	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.Folds.Regions = []FoldRegion{{StartLine: 0, EndLine: 1}}
	ed.SetSize(12, 6)
	ed.Buffer.Cursor = text.Position{Line: 0, Col: 6}

	metrics := ed.currentGutterMetrics()
	if metrics.foldWidth != 0 {
		t.Fatalf("fold width = %d, want 0 in wrap mode", metrics.foldWidth)
	}

	x, y := ed.CursorPosition()
	pos := ed.screenToBuffer(x, y)
	if pos != ed.Buffer.Cursor {
		t.Fatalf("screenToBuffer(%d,%d) = %+v, want %+v", x, y, pos, ed.Buffer.Cursor)
	}
}

func TestEditorEnsureCursorVisibleUsesDisplayWidth(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("你好ab"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(7, 4)
	ed.Buffer.Cursor = text.Position{Line: 0, Col: len("你好a")}

	ed.EnsureCursorVisible()

	if ed.Viewport.ScrollX != 3 {
		t.Fatalf("ScrollX = %d, want 3", ed.Viewport.ScrollX)
	}
}

func renderedLineAndTextStartWidth(t *testing.T, ed Editor, row int, marker byte) (string, int) {
	t.Helper()

	lines := strings.Split(ed.View(), "\n")
	if row >= len(lines) {
		t.Fatalf("rendered row %d out of range (%d lines)", row, len(lines))
	}

	line := ansi.Strip(lines[row])
	start := strings.IndexByte(line, marker)
	if start < 0 {
		t.Fatalf("marker %q not found in rendered line %q", marker, line)
	}

	return line, ansi.StringWidth(line[:start])
}

func renderedRuneStartWidth(t *testing.T, ed Editor, row int, marker rune) (string, int) {
	t.Helper()

	lines := strings.Split(ed.View(), "\n")
	if row >= len(lines) {
		t.Fatalf("rendered row %d out of range (%d lines)", row, len(lines))
	}

	line := ansi.Strip(lines[row])
	start := strings.IndexRune(line, marker)
	if start < 0 {
		t.Fatalf("marker %q not found in rendered line %q", marker, line)
	}

	return line, ansi.StringWidth(line[:start])
}

func TestEditorFoldIndicatorDoesNotShiftTextStart(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("@alpha\n!beta\ngamma"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2}}
	ed.SetSize(40, 6)

	_, row0 := renderedLineAndTextStartWidth(t, ed, 0, '@')
	line1, row1 := renderedLineAndTextStartWidth(t, ed, 1, '!')
	line0, _ := renderedLineAndTextStartWidth(t, ed, 0, '@')

	if row0 != row1 {
		t.Fatalf("text starts at different columns: normal=%d fold=%d line0=%q line1=%q", row0, row1, line0, line1)
	}
}

func TestEditorFoldIndicatorClickMapsToRenderedColumn(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("@alpha\n!beta\ngamma"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2}}
	ed.SetSize(40, 6)

	_, textStart := renderedLineAndTextStartWidth(t, ed, 1, '!')

	pos := ed.screenToBuffer(textStart+1, 1)
	want := text.Position{Line: 1, Col: 1}
	if pos != want {
		t.Fatalf("screenToBuffer on fold row = %+v, want %+v", pos, want)
	}
}

func TestEditorNonFoldRowWithFoldsClickMapsToRenderedColumn(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("@alpha\n!beta\ngamma"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2}}
	ed.SetSize(40, 6)

	_, textStart := renderedLineAndTextStartWidth(t, ed, 0, '@')

	pos := ed.screenToBuffer(textStart+1, 0)
	want := text.Position{Line: 0, Col: 1}
	if pos != want {
		t.Fatalf("screenToBuffer on non-fold row = %+v, want %+v", pos, want)
	}
}

func TestEditorBreakpointIndicatorDoesNotShiftTextStart(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("@alpha\n!beta\ngamma"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.DebugGutter = &GutterOpts{
		Breakpoints: map[int]BreakpointState{1: BPActive},
	}
	ed.SetSize(40, 6)

	_, row0 := renderedLineAndTextStartWidth(t, ed, 0, '@')
	_, row1 := renderedLineAndTextStartWidth(t, ed, 1, '!')

	if row0 != row1 {
		t.Fatalf("text starts at different columns: normal=%d breakpoint=%d", row0, row1)
	}
}

func TestEditorFoldGutterClickTogglesExpandedRegion(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2}}
	ed.SetSize(40, 6)

	_, x := renderedRuneStartWidth(t, ed, 1, '\U000f0140')
	updated, cmd := ed.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      x,
		Y:      1,
	})
	if cmd != nil {
		t.Fatalf("fold click returned unexpected cmd")
	}
	if !updated.Folds.Regions[0].Collapsed {
		t.Fatalf("fold region should collapse after gutter click")
	}
}

func TestEditorFoldGutterClickTogglesCollapsedRegion(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2, Collapsed: true}}
	ed.SetSize(40, 6)

	_, x := renderedRuneStartWidth(t, ed, 1, '\U000f0142')
	updated, cmd := ed.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      x,
		Y:      1,
	})
	if cmd != nil {
		t.Fatalf("fold click returned unexpected cmd")
	}
	if updated.Folds.Regions[0].Collapsed {
		t.Fatalf("fold region should expand after gutter click")
	}
}

func TestEditorFoldGutterClickWithBreakpointColumn(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.DebugGutter = &GutterOpts{
		Breakpoints: map[int]BreakpointState{1: BPActive},
	}
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 2}}
	ed.SetSize(40, 6)

	_, x := renderedRuneStartWidth(t, ed, 1, '\U000f0140')
	updated, cmd := ed.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      x,
		Y:      1,
	})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("fold click routed unexpected message %T", msg)
		}
		t.Fatalf("fold click returned unexpected cmd")
	}
	if !updated.Folds.Regions[0].Collapsed {
		t.Fatalf("fold region should collapse when breakpoint column is present")
	}
}

func TestEditorBreakpointClickAfterCollapsedFoldUsesVisibleBufferLine(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree\nfour"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 3, Collapsed: true}}
	ed.SetSize(40, 6)

	updated, cmd := ed.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      0,
		Y:      2,
	})
	if cmd == nil {
		t.Fatalf("breakpoint gutter click should return a command")
	}
	msg := cmd()
	bpMsg, ok := msg.(BreakpointClickMsg)
	if !ok {
		t.Fatalf("breakpoint gutter click returned %T, want BreakpointClickMsg", msg)
	}
	if bpMsg.Line != 4 {
		t.Fatalf("breakpoint click line = %d, want 4", bpMsg.Line)
	}
	if updated.Folds.Regions[0].Collapsed != true {
		t.Fatalf("breakpoint click should not change fold state")
	}
}

func TestEditorRightmostClickAfterCollapsedFoldMapsToLineEnd(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree\nfour"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Folds.Regions = []FoldRegion{{StartLine: 1, EndLine: 3, Collapsed: true}}
	ed.SetSize(20, 6)

	pos := ed.screenToBuffer(ed.Viewport.Width-1, 2)
	want := text.Position{Line: 4, Col: len("four")}
	if pos != want {
		t.Fatalf("screenToBuffer at right edge = %+v, want %+v", pos, want)
	}
}
