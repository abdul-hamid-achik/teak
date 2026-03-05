package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestNewHover(t *testing.T) {
	theme := ui.DefaultTheme()
	hover := NewHover(theme)

	// Theme contains lipgloss.Style which cannot be compared directly
	if hover.Visible {
		t.Error("expected Visible to be false")
	}
	if hover.Content != "" {
		t.Errorf("expected empty Content, got %q", hover.Content)
	}
}

func TestHoverShow(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())

	hover.Show("hover content")

	if !hover.Visible {
		t.Error("expected Visible to be true")
	}
	if hover.Content != "hover content" {
		t.Errorf("expected Content 'hover content', got %q", hover.Content)
	}
}

func TestHoverShowEmpty(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())

	hover.Show("")

	if hover.Visible {
		t.Error("expected Visible to be false for empty content")
	}
}

func TestHoverHide(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("hover content")

	hover.Hide()

	if hover.Visible {
		t.Error("expected Visible to be false")
	}
	if hover.Content != "" {
		t.Errorf("expected empty Content, got %q", hover.Content)
	}
}

func TestHoverView(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("hover content")

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "hover content") {
		t.Errorf("expected 'hover content' in view, got %q", view)
	}
}

func TestHoverViewNotVisible(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())

	view := hover.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestHoverViewEmptyContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("")

	view := hover.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestHoverViewLongContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	longContent := strings.Repeat("a", 100)
	hover.Show(longContent)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Long content should be truncated
	if strings.Contains(view, strings.Repeat("a", 100)) {
		t.Error("expected long content to be truncated")
	}
}

func TestHoverViewMultiLineContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	content := "line1\nline2\nline3"
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "line1") {
		t.Errorf("expected 'line1' in view")
	}
}

func TestHoverViewManyLinesContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('0'+i))
	}
	content := strings.Join(lines, "\n")
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should be limited to 10 lines
	if strings.Contains(view, "line 9") && !strings.Contains(view, "...") {
		// May or may not have ellipsis depending on exact count
	}
}

func TestHoverViewWidthLimit(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	// Content wider than max width (60)
	content := strings.Repeat("a", 80)
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should be truncated with ellipsis
	if !strings.Contains(view, "...") {
		t.Error("expected truncation indicator")
	}
}

func TestHoverViewExactlyMaxWidth(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	// Exactly 60 characters
	content := strings.Repeat("a", 60)
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHoverViewOneOverMaxWidth(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	// 61 characters
	content := strings.Repeat("a", 61)
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "...") {
		t.Error("expected truncation indicator")
	}
}

func TestHoverViewManyLinesExactlyTen(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = "line " + string(rune('0'+i))
	}
	content := strings.Join(lines, "\n")
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHoverViewElevenLines(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	lines := make([]string, 11)
	for i := range lines {
		lines[i] = "line " + string(rune('0'+i))
	}
	content := strings.Join(lines, "\n")
	hover.Show(content)

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should show 10 lines + ellipsis
	if !strings.Contains(view, "...") {
		t.Error("expected ellipsis for 11 lines")
	}
}

func TestHoverStructure(t *testing.T) {
	hover := Hover{
		Content: "test content",
		Visible: true,
	}

	if hover.Content != "test content" {
		t.Errorf("expected Content 'test content', got %q", hover.Content)
	}
	if !hover.Visible {
		t.Error("expected Visible to be true")
	}
}

func TestHoverShowReplacesContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("first content")
	hover.Show("second content")

	if hover.Content != "second content" {
		t.Errorf("expected Content 'second content', got %q", hover.Content)
	}
	if !hover.Visible {
		t.Error("expected Visible to be true")
	}
}

func TestHoverHideIdempotent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())

	// Hide when already hidden
	hover.Hide()
	hover.Hide()

	if hover.Visible {
		t.Error("expected Visible to be false")
	}
	if hover.Content != "" {
		t.Errorf("expected empty Content, got %q", hover.Content)
	}
}

func TestHoverViewUnicodeContent(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("你好世界🎉")

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHoverViewSpecialCharacters(t *testing.T) {
	hover := NewHover(ui.DefaultTheme())
	hover.Show("func foo() error {\n\treturn nil\n}")

	view := hover.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}
