package editor

import (
	"testing"

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
