package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestViewportFindBracketHighlightsAtCursor(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("(hello)"))
	buf.Cursor = text.Position{Line: 0, Col: 0}

	v := Viewport{Width: 80, Height: 10}
	pos1, pos2, found := v.findBracketHighlights(buf)

	if !found {
		t.Fatal("should find bracket match at cursor")
	}
	if pos1.Col != 0 || pos2.Col != 6 {
		t.Errorf("expected (0,6), got (%d,%d)", pos1.Col, pos2.Col)
	}
}

func TestViewportFindBracketHighlightsBeforeCursor(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("(hello)"))
	buf.Cursor = text.Position{Line: 0, Col: 7} // after last char

	v := Viewport{Width: 80, Height: 10}
	pos1, pos2, found := v.findBracketHighlights(buf)

	if !found {
		t.Fatal("should find bracket match before cursor")
	}
	if pos1.Col != 6 || pos2.Col != 0 {
		t.Errorf("expected (6,0), got (%d,%d)", pos1.Col, pos2.Col)
	}
}

func TestViewportFindBracketHighlightsNoBracket(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	buf.Cursor = text.Position{Line: 0, Col: 2}

	v := Viewport{Width: 80, Height: 10}
	_, _, found := v.findBracketHighlights(buf)

	if found {
		t.Error("should not find bracket match without brackets")
	}
}

func TestViewportEnsureCursorVisibleHorizontal(t *testing.T) {
	v := Viewport{Width: 20, Height: 10, ScrollX: 0}

	// Cursor far to the right
	cursor := text.Position{Line: 0, Col: 50}
	v.EnsureCursorVisible(cursor, 10)

	if v.ScrollX == 0 {
		t.Error("should scroll right when cursor is beyond viewport")
	}

	// Now cursor to the left of scroll
	v.ScrollX = 30
	cursor = text.Position{Line: 0, Col: 5}
	v.EnsureCursorVisible(cursor, 10)

	if v.ScrollX != 5 {
		t.Errorf("should scroll left, got ScrollX=%d", v.ScrollX)
	}
}

func TestViewportScrollUpClampsToZero(t *testing.T) {
	v := Viewport{Width: 80, Height: 10, ScrollY: 0}
	v.ScrollUp(5)
	if v.ScrollY != 0 {
		t.Errorf("expected ScrollY=0, got %d", v.ScrollY)
	}
}

func TestViewportScrollDownClampsToMax(t *testing.T) {
	v := Viewport{Width: 80, Height: 10, ScrollY: 0}
	v.ScrollDown(100, 15)
	// maxScroll = 15 - 10 + 1 = 6
	if v.ScrollY != 6 {
		t.Errorf("expected ScrollY=6, got %d", v.ScrollY)
	}
}

func TestReplaceAtDisplayCol(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		col      int
		old      string
		replace  string
		expected string
	}{
		{
			name:     "simple replace",
			input:    "hello",
			col:      0,
			old:      "h",
			replace:  "H",
			expected: "Hello",
		},
		{
			name:     "middle replace",
			input:    "hello",
			col:      2,
			old:      "l",
			replace:  "L",
			expected: "heLlo",
		},
		{
			name:     "no match at col",
			input:    "hello",
			col:      0,
			old:      "x",
			replace:  "X",
			expected: "hello", // unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceAtDisplayCol(tt.input, tt.col, tt.old, tt.replace)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestReplaceAtDisplayColWithANSI(t *testing.T) {
	// String with ANSI escape: "\x1b[31mhello\x1b[0m"
	input := "\x1b[31mhello\x1b[0m"
	got := replaceAtDisplayCol(input, 0, "h", "H")
	if got == input {
		t.Error("should replace character even with ANSI codes")
	}
}

func TestViewportRenderEmptyBuffer(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte(""))
	theme := ui.DefaultTheme()

	v := Viewport{Width: 80, Height: 5}
	result := v.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("should render something even for empty buffer")
	}
}

func TestViewportScreenToBufferPositionWithTabs(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("\thello"))
	v := Viewport{Width: 80, Height: 10}

	pos := v.ScreenToBufferPosition(10, 0, buf, 4, nil)
	if pos.Line != 0 {
		t.Errorf("expected line 0, got %d", pos.Line)
	}
}

func TestViewportSelectionRangeReversed(t *testing.T) {
	// Selection with Head before Anchor (reversed)
	sel := &text.Selection{
		Anchor: text.Position{Line: 1, Col: 5},
		Head:   text.Position{Line: 0, Col: 2},
	}

	start, end := selectionRange(sel, 0, 10)
	if start != 2 {
		t.Errorf("expected start 2, got %d", start)
	}
	if end != 10 {
		t.Errorf("expected end 10, got %d", end)
	}

	start, end = selectionRange(sel, 1, 10)
	if start != 0 {
		t.Errorf("expected start 0, got %d", start)
	}
	if end != 5 {
		t.Errorf("expected end 5, got %d", end)
	}
}

func TestViewportSelectionRangeSingleLine(t *testing.T) {
	sel := &text.Selection{
		Anchor: text.Position{Line: 0, Col: 2},
		Head:   text.Position{Line: 0, Col: 7},
	}

	start, end := selectionRange(sel, 0, 10)
	if start != 2 || end != 7 {
		t.Errorf("expected (2,7), got (%d,%d)", start, end)
	}

	// No overlap with line 1
	start, end = selectionRange(sel, 1, 10)
	if start != -1 || end != -1 {
		t.Errorf("expected (-1,-1), got (%d,%d)", start, end)
	}
}

func TestApplyScrollXCountPartial(t *testing.T) {
	result, remaining := applyScrollXCount("hello", 3)
	if result != "lo" || remaining != 0 {
		t.Errorf("expected 'lo', 0, got %q, %d", result, remaining)
	}
}

func TestApplyScrollXCountExact(t *testing.T) {
	result, remaining := applyScrollXCount("hello", 5)
	if result != "" || remaining != 0 {
		t.Errorf("expected '', 0, got %q, %d", result, remaining)
	}
}

func TestDisplayWidthEmpty(t *testing.T) {
	w := displayWidth("")
	if w != 0 {
		t.Errorf("expected 0, got %d", w)
	}
}

func TestDisplayWidthTabs(t *testing.T) {
	w := displayWidth("\t")
	if w < 0 {
		t.Errorf("expected non-negative, got %d", w)
	}
}

func TestTruncateToWidthExact(t *testing.T) {
	result := truncateToWidth("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncateToWidthNegative(t *testing.T) {
	result := truncateToWidth("hello", -1)
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

func TestApplyScrollXNegative(t *testing.T) {
	result := applyScrollX("hello", -1)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}
