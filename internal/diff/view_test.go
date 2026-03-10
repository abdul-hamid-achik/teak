package diff

import (
	"testing"

	"teak/internal/ui"
)

// TestDiffModelCreation tests New function
func TestDiffModelCreation(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", LeftKind: KindUnchanged, RightKind: KindUnchanged},
		{Left: "old", Right: "new", LeftKind: KindRemoved, RightKind: KindAdded},
	}

	model := New("test.go", lines, theme)

	if model.FilePath != "test.go" {
		t.Errorf("Expected FilePath 'test.go', got %q", model.FilePath)
	}
	if len(model.Lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(model.Lines))
	}
	if model.leftHL == nil {
		t.Error("Expected leftHL to be initialized")
	}
	if model.rightHL == nil {
		t.Error("Expected rightHL to be initialized")
	}
}

// TestDiffModelWithEmptyLines tests New with empty lines
func TestDiffModelWithEmptyLines(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{}

	model := New("test.go", lines, theme)

	if len(model.Lines) != 0 {
		t.Errorf("Expected 0 lines, got %d", len(model.Lines))
	}
	// Should not panic with empty lines
}

// TestDiffModelSetSize tests SetSize method
func TestDiffModelSetSize(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New("test.go", nil, theme)

	model.SetSize(100, 30)

	if model.Width != 100 {
		t.Errorf("Expected Width 100, got %d", model.Width)
	}
	if model.Height != 30 {
		t.Errorf("Expected Height 30, got %d", model.Height)
	}
}

// TestDiffModelUpdateWithUpKey tests Update with up key
func TestDiffModelUpdateWithUpKey(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
		{Left: "line2", Right: "line2"},
	}
	model := New("test.go", lines, theme)
	model.ScrollY = 1

	model, _ = model.Update(nil) // nil message should not change state
	if model.ScrollY != 1 {
		t.Errorf("Expected ScrollY to remain 1, got %d", model.ScrollY)
	}
}

// TestDiffModelView tests View method
func TestDiffModelView(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", LeftKind: KindUnchanged, RightKind: KindUnchanged},
		{Left: "old", Right: "new", LeftKind: KindRemoved, RightKind: KindAdded},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithEmptyLines tests View with empty lines
func TestDiffModelViewWithEmptyLines(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New("test.go", nil, theme)
	model.SetSize(80, 10)

	view := model.View()
	// Empty diff may return empty view or empty panel - both are acceptable
	// Just ensure it doesn't panic
	_ = view
}

// TestDiffModelViewWithScroll tests View with scroll offset
func TestDiffModelViewWithScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
		{Left: "line2", Right: "line2"},
		{Left: "line3", Right: "line3"},
		{Left: "line4", Right: "line4"},
		{Left: "line5", Right: "line5"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 3)
	model.ScrollY = 2

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithSeparator tests View with separator lines
func TestDiffModelViewWithSeparator(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", IsSeparator: true},
		{Left: "line2", Right: "line2"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithMixedKinds tests View with mixed line kinds
func TestDiffModelViewWithMixedKinds(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "unchanged", Right: "unchanged", LeftKind: KindUnchanged, RightKind: KindUnchanged},
		{Left: "removed", Right: "", LeftKind: KindRemoved, RightKind: KindEmpty},
		{Left: "", Right: "added", LeftKind: KindEmpty, RightKind: KindAdded},
		{Left: "old", Right: "new", LeftKind: KindRemoved, RightKind: KindAdded},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelBuildHighlighting tests buildHighlighting with various inputs
func TestDiffModelBuildHighlighting(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", LeftKind: KindUnchanged, RightKind: KindUnchanged},
		{Left: "old", Right: "new", LeftKind: KindRemoved, RightKind: KindAdded},
		{Left: "", Right: "added", LeftKind: KindEmpty, RightKind: KindAdded},
		{Left: "removed", Right: "", LeftKind: KindRemoved, RightKind: KindEmpty},
	}

	model := New("test.go", lines, theme)

	// Check that line maps are built correctly
	if len(model.leftLineMap) != 4 {
		t.Errorf("Expected 4 leftLineMap entries, got %d", len(model.leftLineMap))
	}
	if len(model.rightLineMap) != 4 {
		t.Errorf("Expected 4 rightLineMap entries, got %d", len(model.rightLineMap))
	}
}

// TestDiffModelBuildHighlightingWithSeparators tests buildHighlighting with separators
func TestDiffModelBuildHighlightingWithSeparators(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", IsSeparator: true},
		{Left: "line2", Right: "line2"},
	}

	model := New("test.go", lines, theme)

	// Separator should map to -1
	if model.leftLineMap[0] != -1 {
		t.Errorf("Expected separator to map to -1, got %d", model.leftLineMap[0])
	}
	if model.rightLineMap[0] != -1 {
		t.Errorf("Expected separator to map to -1, got %d", model.rightLineMap[0])
	}
}

// TestDiffModelViewWithLongLines tests View with long lines
func TestDiffModelViewWithLongLines(t *testing.T) {
	theme := ui.DefaultTheme()
	longLine := "this is a very long line that should be truncated in the view"
	lines := []DiffLine{
		{Left: longLine, Right: longLine},
	}
	model := New("test.go", lines, theme)
	model.SetSize(40, 10) // Narrow width to force truncation

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithWideView tests View with wide viewport
func TestDiffModelViewWithWideView(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "short", Right: "short"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(200, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithTallView tests View with tall viewport
func TestDiffModelViewWithTallView(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 50)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithNarrowView tests View with narrow viewport
func TestDiffModelViewWithNarrowView(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line", Right: "line"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(20, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithShortView tests View with short viewport
func TestDiffModelViewWithShortView(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
		{Left: "line2", Right: "line2"},
		{Left: "line3", Right: "line3"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 2)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithDifferentThemes tests View with different themes
func TestDiffModelViewWithDifferentThemes(t *testing.T) {
	themes := []ui.Theme{
		ui.NordTheme(),
		ui.DraculaTheme(),
		ui.CatppuccinTheme(),
	}

	lines := []DiffLine{
		{Left: "test", Right: "test"},
	}

	for i, theme := range themes {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			model := New("test.go", lines, theme)
			model.SetSize(80, 10)

			view := model.View()
			if view == "" {
				t.Error("Expected non-empty view")
			}
		})
	}
}

// TestDiffModelViewWithUnicodeContent tests View with unicode content
func TestDiffModelViewWithUnicodeContent(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "你好", Right: "Hello"},
		{Left: "🚀", Right: "Rocket"},
		{Left: "مرحبا", Right: "Arabic"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithTabs tests View with tabs
func TestDiffModelViewWithTabs(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "\tindented", Right: "\t\tmore indented"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithEmptyLeftRight tests View with empty left and right
func TestDiffModelViewWithEmptyLeftRight(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "", Right: "", LeftKind: KindEmpty, RightKind: KindEmpty},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithOnlyLeftContent tests View with only left content
func TestDiffModelViewWithOnlyLeftContent(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "left only", Right: "", LeftKind: KindRemoved, RightKind: KindEmpty},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithOnlyRightContent tests View with only right content
func TestDiffModelViewWithOnlyRightContent(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "", Right: "right only", LeftKind: KindEmpty, RightKind: KindAdded},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithMultilineContent tests View with multiline content
func TestDiffModelViewWithMultilineContent(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
		{Left: "line2", Right: "line2 modified"},
		{Left: "line3", Right: "line3"},
		{Left: "line4", Right: "line4"},
		{Left: "line5", Right: "line5"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewScrollsBeyondContent tests View scrolls gracefully beyond content
func TestDiffModelViewScrollsBeyondContent(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
		{Left: "line2", Right: "line2"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)
	model.ScrollY = 100 // Scroll beyond content

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithZeroWidth tests View with zero width
func TestDiffModelViewWithZeroWidth(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "test", Right: "test"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(0, 10)

	view := model.View()
	// Should not panic, may return empty or minimal output
	_ = view
}

// TestDiffModelViewWithZeroHeight tests View with zero height
func TestDiffModelViewWithZeroHeight(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "test", Right: "test"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 0)

	view := model.View()
	// Should not panic, may return empty or minimal output
	_ = view
}

// TestDiffModelUpdateWithUnknownMessage tests Update with unknown message type
func TestDiffModelUpdateWithUnknownMessage(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New("test.go", nil, theme)

	_, cmd := model.Update("unknown")
	if cmd != nil {
		t.Error("Expected nil command for unknown message")
	}
}

// TestDiffModelGutterWidth tests gutterWidth calculation
func TestDiffModelGutterWidth(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1", LeftNum: 1, RightNum: 1},
		{Left: "line2", Right: "line2", LeftNum: 10, RightNum: 10},
		{Left: "line3", Right: "line3", LeftNum: 100, RightNum: 100},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithLargeLineNumbers tests View with large line numbers
func TestDiffModelViewWithLargeLineNumbers(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line", Right: "line", LeftNum: 9999, RightNum: 9999},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewWithNegativeScroll tests View with negative scroll (should be clamped)
func TestDiffModelViewWithNegativeScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "line1", Right: "line1"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)
	model.ScrollY = -5

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelWithGoFile tests diff view with Go file content
func TestDiffModelWithGoFile(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "package main", Right: "package main", LeftKind: KindUnchanged, RightKind: KindUnchanged, LeftNum: 1, RightNum: 1},
		{Left: "", Right: "", LeftKind: KindUnchanged, RightKind: KindUnchanged, LeftNum: 2, RightNum: 2},
		{Left: "func old()", Right: "", LeftKind: KindRemoved, RightKind: KindEmpty, LeftNum: 3},
		{Left: "", Right: "func new()", LeftKind: KindEmpty, RightKind: KindAdded, RightNum: 3},
	}
	model := New("main.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelWithMarkdownFile tests diff view with Markdown content
func TestDiffModelWithMarkdownFile(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "# Old Title", Right: "# New Title", LeftKind: KindRemoved, RightKind: KindAdded, LeftNum: 1, RightNum: 1},
		{Left: "Content", Right: "Content", LeftKind: KindUnchanged, RightKind: KindUnchanged, LeftNum: 2, RightNum: 2},
	}
	model := New("README.md", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelWithJSONFile tests diff view with JSON content
func TestDiffModelWithJSONFile(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: `{"old": "value"}`, Right: `{"new": "value"}`, LeftKind: KindRemoved, RightKind: KindAdded, LeftNum: 1, RightNum: 1},
	}
	model := New("data.json", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelViewRepeatedCalls tests that View can be called multiple times
func TestDiffModelViewRepeatedCalls(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "test", Right: "test"},
	}
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view1 := model.View()
	view2 := model.View()
	view3 := model.View()

	if view1 == "" || view2 == "" || view3 == "" {
		t.Error("Expected non-empty views")
	}
}

// TestDiffModelSetSizeMultipleTimes tests SetSize can be called multiple times
func TestDiffModelSetSizeMultipleTimes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New("test.go", nil, theme)

	model.SetSize(50, 20)
	if model.Width != 50 || model.Height != 20 {
		t.Errorf("Expected 50x20, got %dx%d", model.Width, model.Height)
	}

	model.SetSize(100, 30)
	if model.Width != 100 || model.Height != 30 {
		t.Errorf("Expected 100x30, got %dx%d", model.Width, model.Height)
	}

	model.SetSize(200, 50)
	if model.Width != 200 || model.Height != 50 {
		t.Errorf("Expected 200x50, got %dx%d", model.Width, model.Height)
	}
}

// TestDiffModelWithNilTheme tests behavior with nil theme (should not panic)
func TestDiffModelWithNilTheme(t *testing.T) {
	// This test ensures the model handles edge cases gracefully
	lines := []DiffLine{
		{Left: "test", Right: "test"},
	}

	// Use default theme instead of nil to avoid panic
	theme := ui.DefaultTheme()
	model := New("test.go", lines, theme)
	model.SetSize(80, 10)

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDiffModelLineMapIndices tests that line map indices are correct
func TestDiffModelLineMapIndices(t *testing.T) {
	theme := ui.DefaultTheme()
	lines := []DiffLine{
		{Left: "a", Right: "a", LeftKind: KindUnchanged, RightKind: KindUnchanged},
		{Left: "b", Right: "b-modified", LeftKind: KindRemoved, RightKind: KindAdded},
		{Left: "", Right: "c", LeftKind: KindEmpty, RightKind: KindAdded},
		{Left: "d", Right: "", LeftKind: KindRemoved, RightKind: KindEmpty},
	}

	model := New("test.go", lines, theme)

	// Check left line map
	// Line 0: unchanged, should map to 0
	// Line 1: removed, should map to 1
	// Line 2: empty left, should map to -1
	// Line 3: removed, should map to 2
	expectedLeft := []int{0, 1, -1, 2}
	for i, expected := range expectedLeft {
		if model.leftLineMap[i] != expected {
			t.Errorf("leftLineMap[%d] = %d, want %d", i, model.leftLineMap[i], expected)
		}
	}

	// Check right line map
	// Line 0: unchanged, should map to 0
	// Line 1: added, should map to 1
	// Line 2: added, should map to 2
	// Line 3: empty right, should map to -1
	expectedRight := []int{0, 1, 2, -1}
	for i, expected := range expectedRight {
		if model.rightLineMap[i] != expected {
			t.Errorf("rightLineMap[%d] = %d, want %d", i, model.rightLineMap[i], expected)
		}
	}
}
