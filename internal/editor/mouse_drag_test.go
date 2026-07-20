package editor

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func dragFrom(t *testing.T, e Editor, x, y int) Editor {
	t.Helper()
	updated, _ := e.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	if !updated.dragging {
		t.Fatal("left click did not start a selection drag")
	}
	return updated
}

func TestEditorMouseDragAutoscrollDown(t *testing.T) {
	e := newEditor("zero\none\ntwo\nthree\nfour", 0, 0)
	e.SetSize(20, 2)
	e = dragFrom(t, e, e.effectiveGutterWidth(), 0)

	e, _ = e.Update(tea.MouseMotionMsg{X: e.effectiveGutterWidth(), Y: e.Viewport.Height})

	if e.Viewport.ScrollY != 1 {
		t.Fatalf("ScrollY = %d, want 1 after dragging below viewport", e.Viewport.ScrollY)
	}
	if got := e.Buffer.Selections.Primary().Head.Line; got != 2 {
		t.Fatalf("selection head line = %d, want 2", got)
	}
}

func TestEditorMouseDragAutoscrollUp(t *testing.T) {
	e := newEditor("zero\none\ntwo\nthree\nfour", 0, 0)
	e.SetSize(20, 2)
	e.Viewport.ScrollY = 2
	e = dragFrom(t, e, e.effectiveGutterWidth(), 0)

	e, _ = e.Update(tea.MouseMotionMsg{X: e.effectiveGutterWidth(), Y: -1})

	if e.Viewport.ScrollY != 1 {
		t.Fatalf("ScrollY = %d, want 1 after dragging above viewport", e.Viewport.ScrollY)
	}
	if got := e.Buffer.Selections.Primary().Head.Line; got != 1 {
		t.Fatalf("selection head line = %d, want 1", got)
	}
}

func TestEditorMouseDragAutoscrollWrap(t *testing.T) {
	e := newEditor("abcdefghijklmnop", 0, 0)
	e.Config.WordWrap = true
	e.SetSize(8, 2)
	if e.Wrap == nil || e.Wrap.LineRows(0) < 3 {
		t.Fatalf("test setup requires a wrapped line, got %#v", e.Wrap)
	}
	e = dragFrom(t, e, e.effectiveGutterWidth(), 0)

	e, _ = e.Update(tea.MouseMotionMsg{X: e.effectiveGutterWidth(), Y: e.Viewport.Height})

	if e.Viewport.WrapScrollY != 1 {
		t.Fatalf("WrapScrollY = %d, want 1 after dragging below viewport", e.Viewport.WrapScrollY)
	}
	if e.Buffer.Selections == nil || e.Buffer.Selections.Primary().IsEmpty() {
		t.Fatal("wrapped drag should retain a non-empty selection")
	}
}

func TestEditorMouseDragAutoscrollFoldedLines(t *testing.T) {
	e := newEditor("zero\none\ntwo\nthree\nfour\nfive", 0, 0)
	e.SetSize(20, 2)
	e.Folds.SetRegions([]FoldRegion{{StartLine: 1, EndLine: 3, Collapsed: true}})
	e = dragFrom(t, e, e.effectiveGutterWidth(), 0)

	e, _ = e.Update(tea.MouseMotionMsg{X: e.effectiveGutterWidth(), Y: e.Viewport.Height})

	if e.Viewport.ScrollY != 1 {
		t.Fatalf("folded visual ScrollY = %d, want 1", e.Viewport.ScrollY)
	}
	if got := e.Buffer.Selections.Primary().Head.Line; e.Folds.IsLineHidden(got) {
		t.Fatalf("selection head line %d is hidden by the collapsed fold", got)
	}
}

func TestEditorMouseDragAutoscrollZeroViewportIsSafe(t *testing.T) {
	e := newEditor("zero\none", 0, 0)
	e.SetSize(0, 0)
	e.dragging = true

	e, _ = e.Update(tea.MouseMotionMsg{X: -100, Y: 100})

	if e.Viewport.ScrollY < 0 || e.Viewport.WrapScrollY < 0 {
		t.Fatalf("scroll offsets must remain non-negative: ScrollY=%d WrapScrollY=%d", e.Viewport.ScrollY, e.Viewport.WrapScrollY)
	}
}

func TestEditorMouseTripleClickSelectsLine(t *testing.T) {
	e := newEditor("zero\none", 0, 0)
	x := e.effectiveGutterWidth() + 1
	for range 3 {
		e, _ = e.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: 0})
		time.Sleep(time.Millisecond)
	}

	selection := e.Buffer.Selections.Primary()
	start, end := selection.Ordered()
	if start.Line != 0 || start.Col != 0 || end.Line != 1 || end.Col != 0 {
		t.Fatalf("triple-click selection = %#v, want whole first line", selection)
	}
	if e.dragging {
		t.Fatal("triple-click should not leave a selection drag active")
	}
}
