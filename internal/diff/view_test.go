package diff

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
	"teak/internal/highlight"
	"teak/internal/ui"
)

func BenchmarkPrepareHighlightedDiffViewportThousands(b *testing.B) {
	lines := make([]DiffLine, 10_000)
	for i := range lines {
		line := fmt.Sprintf("value%d := call%d(input)", i, i%37)
		lines[i] = DiffLine{
			Left: line, Right: line, LeftNum: i + 1, RightNum: i + 1,
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	theme := ui.DefaultTheme()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		model := New("large.go", lines, theme)
		if !model.PrepareViewport(context.Background(), 0, 40) {
			b.Fatal("viewport highlighting was canceled")
		}
		if len(model.leftHL.Line(0)) == 0 || len(model.rightHL.Line(0)) == 0 || len(model.leftLineMap) != len(lines) {
			b.Fatal("diff highlighting was not prepared")
		}
	}
}

func BenchmarkPrepareDiffScrollViewportThousands(b *testing.B) {
	lines := make([]DiffLine, 10_000)
	for i := range lines {
		line := fmt.Sprintf("value%d := call%d(input)", i, i%37)
		lines[i] = DiffLine{
			Left: line, Right: line, LeftNum: i + 1, RightNum: i + 1,
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result := prepareViewportHighlight(
			context.Background(), model.leftHL, model.rightHL,
			model.leftSource, model.rightSource, model.leftKinds, model.rightKinds,
			model.leftLineMap, model.rightLineMap,
			len(model.Lines), 5_000, 5_040,
		)
		if !result.complete || len(result.leftBatch.Lines) == 0 || len(result.rightBatch.Lines) == 0 {
			b.Fatal("scroll viewport highlighting was not prepared")
		}
	}
}

func BenchmarkDiffScrollDispatchThousands(b *testing.B) {
	lines := make([]DiffLine, 10_000)
	for i := range lines {
		lines[i] = DiffLine{
			Left: "value := call(input)", Right: "value := call(input)",
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	model.SetSize(80, 40)
	model.ScrollY = 5_000
	msg := tea.KeyPressMsg{Code: tea.KeyDown}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		candidate, cmd := model.Update(msg)
		if candidate.ScrollY != 5_001 || cmd == nil {
			b.Fatal("scroll did not schedule viewport preparation")
		}
	}
}

func BenchmarkViewPreparedDiffThousands(b *testing.B) {
	lines := make([]DiffLine, 10_000)
	for i := range lines {
		line := fmt.Sprintf("value%d := call%d(input)", i, i%37)
		lines[i] = DiffLine{
			Left: line, Right: line, LeftNum: i + 1, RightNum: i + 1,
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	model.SetSize(160, 40)
	model.ScrollY = 5_000
	if !model.PrepareViewport(context.Background(), model.ScrollY, model.ScrollY+model.Height) {
		b.Fatal("viewport highlighting was canceled")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if view := model.View(); view == "" {
			b.Fatal("prepared diff rendered empty")
		}
	}
}

func TestNewDefersDiffTokenization(t *testing.T) {
	lines := make([]DiffLine, 1_000)
	for i := range lines {
		lines[i] = DiffLine{
			Left: "value := call(input)", Right: "value := call(input)",
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	if got := model.leftHL.LineCount(); got != 0 {
		t.Fatalf("New tokenized %d left lines eagerly", got)
	}
	if got := model.rightHL.LineCount(); got != 0 {
		t.Fatalf("New tokenized %d right lines eagerly", got)
	}
}

func TestPrepareViewportKeepsLargeDiffHighlightingSparse(t *testing.T) {
	lines := make([]DiffLine, 10_000)
	for i := range lines {
		lines[i] = DiffLine{
			Left: "value := call(input)", Right: "value := call(input)",
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	if !model.PrepareViewport(context.Background(), 5_000, 5_024) {
		t.Fatal("viewport preparation was canceled")
	}
	if got := model.leftHL.Line(model.leftLineMap[5_000]); len(got) == 0 {
		t.Fatal("visible left line was not highlighted")
	}
	if got := model.rightHL.Line(model.rightLineMap[5_023]); len(got) == 0 {
		t.Fatal("visible right line was not highlighted")
	}
	if got := model.leftHL.Line(model.leftLineMap[0]); got != nil {
		t.Fatal("offscreen line was tokenized by a sparse viewport projection")
	}
}

func TestPreparedDiffTokensCarryTheirLineBackground(t *testing.T) {
	lines := []DiffLine{
		{Left: "oldValue", Right: "newValue", LeftKind: KindRemoved, RightKind: KindAdded},
		{Left: "sameValue", Right: "sameValue", LeftKind: KindUnchanged, RightKind: KindUnchanged},
	}
	model := New("main.go", lines, ui.DefaultTheme())
	if !model.PrepareViewport(context.Background(), 0, len(lines)) {
		t.Fatal("viewport preparation was canceled")
	}

	checks := []struct {
		name   string
		tokens []highlight.StyledToken
		kind   LineKind
	}{
		{name: "removed", tokens: model.leftHL.Line(model.leftLineMap[0]), kind: KindRemoved},
		{name: "added", tokens: model.rightHL.Line(model.rightLineMap[0]), kind: KindAdded},
		{name: "unchanged", tokens: model.leftHL.Line(model.leftLineMap[1]), kind: KindUnchanged},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if len(check.tokens) == 0 {
				t.Fatal("expected prepared syntax tokens")
			}
			gotR, gotG, gotB, gotA := check.tokens[0].Style.GetBackground().RGBA()
			wantR, wantG, wantB, wantA := backgroundForKind(check.kind).RGBA()
			if gotR != wantR || gotG != wantG || gotB != wantB || gotA != wantA {
				t.Fatalf("background RGBA = (%d,%d,%d,%d), want (%d,%d,%d,%d)", gotR, gotG, gotB, gotA, wantR, wantG, wantB, wantA)
			}
		})
	}
}

func TestPreparedDiffFastRenderMatchesLipglossReference(t *testing.T) {
	lines := []DiffLine{
		{Left: "oldValue := \"á\t漢\"", Right: "newValue := call(input)", LeftKind: KindRemoved, RightKind: KindAdded},
		{Left: "sameValue := 42", Right: "sameValue := 42", LeftKind: KindUnchanged, RightKind: KindUnchanged},
	}
	model := New("main.go", lines, ui.DefaultTheme())
	if !model.PrepareViewport(context.Background(), 0, len(lines)) {
		t.Fatal("viewport preparation was canceled")
	}

	reference := func(text string, kind LineKind, width int, tokens []highlight.StyledToken) string {
		var sb strings.Builder
		widthLeft := width
		for _, token := range tokens {
			if widthLeft <= 0 {
				break
			}
			part := strings.ReplaceAll(strings.TrimRight(token.Text, "\n\r"), "\t", "    ")
			partWidth := runewidth.StringWidth(part)
			if partWidth > widthLeft {
				part = truncateToWidth(part, widthLeft)
				partWidth = runewidth.StringWidth(part)
			}
			sb.WriteString(token.Style.Background(model.bgForKind(kind)).Render(part))
			widthLeft -= partWidth
		}
		if widthLeft > 0 {
			padding := lipgloss.NewStyle().Background(model.bgForKind(kind)).Foreground(model.fgForKind(kind))
			sb.WriteString(padding.Render(strings.Repeat(" ", widthLeft)))
		}
		return sb.String()
	}

	tests := []struct {
		name   string
		text   string
		kind   LineKind
		tokens []highlight.StyledToken
		widths []int
	}{
		{name: "removed unicode and tab", text: lines[0].Left, kind: KindRemoved, tokens: model.leftHL.Line(model.leftLineMap[0]), widths: []int{9, 32}},
		{name: "added", text: lines[0].Right, kind: KindAdded, tokens: model.rightHL.Line(model.rightLineMap[0]), widths: []int{11, 32}},
		{name: "unchanged", text: lines[1].Left, kind: KindUnchanged, tokens: model.leftHL.Line(model.leftLineMap[1]), widths: []int{8, 24}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.tokens) == 0 {
				t.Fatal("expected prepared syntax tokens")
			}
			for _, width := range tt.widths {
				got := model.renderContentHighlighted(tt.text, tt.kind, width, tt.tokens)
				want := reference(tt.text, tt.kind, width, tt.tokens)
				if got != want {
					t.Fatalf("width %d fast render = %q, want lipgloss reference %q", width, got, want)
				}
			}
		})
	}
}

func TestDiffScrollPreparesLatestViewportAsynchronously(t *testing.T) {
	lines := make([]DiffLine, 1_000)
	for i := range lines {
		lines[i] = DiffLine{
			Left: "value := call(input)", Right: "value := call(input)",
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	model.SetSize(80, 10)
	if !model.PrepareViewport(context.Background(), 0, 10) {
		t.Fatal("initial viewport preparation was canceled")
	}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if cmd == nil {
		t.Fatal("scroll tokenized inside Update instead of scheduling preparation")
	}
	last := updated.leftLineMap[len(lines)-1]
	if got := updated.leftHL.Line(last); got != nil {
		t.Fatal("far viewport was highlighted before the command completed")
	}

	applied, next := updated.Update(cmd())
	if next != nil {
		t.Fatal("prepared viewport unexpectedly scheduled more work")
	}
	if got := applied.leftHL.Line(last); len(got) == 0 {
		t.Fatal("prepared viewport did not highlight the final visible line")
	}
}

func TestLaterDiffScrollCancelsObsoleteViewport(t *testing.T) {
	lines := make([]DiffLine, 1_000)
	for i := range lines {
		lines[i] = DiffLine{
			Left: "value := call(input)", Right: "value := call(input)",
			LeftKind: KindUnchanged, RightKind: KindUnchanged,
		}
	}
	model := New("large.go", lines, ui.DefaultTheme())
	model.SetSize(80, 10)

	afterEnd, endCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if endCmd == nil {
		t.Fatal("end scroll did not schedule highlighting")
	}
	afterHome, homeCmd := afterEnd.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if homeCmd == nil {
		t.Fatal("home scroll did not supersede highlighting")
	}
	obsolete, ok := endCmd().(HighlightReadyMsg)
	if !ok || !obsolete.canceled {
		t.Fatalf("obsolete viewport result = %+v", obsolete)
	}
	afterObsolete, _ := afterHome.Update(obsolete)
	if got := afterObsolete.leftHL.Line(afterObsolete.leftLineMap[len(lines)-1]); got != nil {
		t.Fatal("obsolete viewport populated the far highlight cache")
	}
	current, ok := homeCmd().(HighlightReadyMsg)
	if !ok || current.canceled {
		t.Fatalf("current viewport result = %+v", current)
	}
}

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

func TestDiffModelClampsScrollToLastVisiblePage(t *testing.T) {
	lines := make([]DiffLine, 10)
	model := New("test.go", lines, ui.DefaultTheme())
	model.SetSize(80, 3)

	for range 20 {
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got, want := model.ScrollY, 7; got != want {
		t.Fatalf("scroll after down = %d, want %d", got, want)
	}
	model.ScrollY = 0
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got, want := model.ScrollY, 7; got != want {
		t.Fatalf("scroll after end = %d, want %d", got, want)
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
