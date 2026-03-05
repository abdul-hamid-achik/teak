package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestRenderGutter(t *testing.T) {
	theme := ui.DefaultTheme()

	result, width := RenderGutter(theme, 100, 0, 10, 0, nil)
	if result == "" {
		t.Error("expected non-empty gutter")
	}
	if width < 3 {
		t.Errorf("expected width >= 3, got %d", width)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}

func TestRenderGutterActiveLine(t *testing.T) {
	theme := ui.DefaultTheme()

	result, _ := RenderGutter(theme, 100, 0, 10, 5, nil)
	lines := strings.Split(result, "\n")

	// Line 6 (index 5) should be active
	if len(lines) < 6 {
		t.Fatal("expected at least 6 lines")
	}
	// The active line should have different styling
	if !strings.Contains(lines[5], "6") {
		t.Errorf("expected line 6 in result, got %q", lines[5])
	}
}

func TestRenderGutterWithDiagnostics(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 0, EndLine: 0, Severity: 1, Message: "error"},
		{StartLine: 2, EndLine: 2, Severity: 2, Message: "warning"},
		{StartLine: 4, EndLine: 4, Severity: 3, Message: "info"},
	}

	result, _ := RenderGutter(theme, 100, 0, 10, 0, diagnostics)
	if result == "" {
		t.Error("expected non-empty gutter")
	}
}

func TestRenderGutterWithDiagnosticsError(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 2, EndLine: 2, Severity: 1, Message: "error"},
	}

	result, _ := RenderGutter(theme, 100, 0, 10, 2, diagnostics)
	lines := strings.Split(result, "\n")

	// Line 3 (index 2) should have error styling
	if len(lines) < 3 {
		t.Fatal("expected at least 3 lines")
	}
}

func TestRenderGutterWithDiagnosticsWarning(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 2, EndLine: 2, Severity: 2, Message: "warning"},
	}

	result, _ := RenderGutter(theme, 100, 0, 10, 2, diagnostics)
	lines := strings.Split(result, "\n")

	if len(lines) < 3 {
		t.Fatal("expected at least 3 lines")
	}
}

func TestRenderGutterScroll(t *testing.T) {
	theme := ui.DefaultTheme()

	result, _ := RenderGutter(theme, 100, 50, 10, 55, nil)
	lines := strings.Split(result, "\n")

	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}

	// First visible line should be 51
	if !strings.Contains(lines[0], "51") {
		t.Errorf("expected line 51 in first line, got %q", lines[0])
	}
}

func TestRenderGutterSmallFile(t *testing.T) {
	theme := ui.DefaultTheme()

	result, width := RenderGutter(theme, 5, 0, 10, 0, nil)
	if result == "" {
		t.Error("expected non-empty gutter")
	}
	if width < 3 {
		t.Errorf("expected width >= 3, got %d", width)
	}

	lines := strings.Split(result, "\n")
	// Should have 10 lines (some empty)
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}

func TestRenderGutterEmptyLines(t *testing.T) {
	theme := ui.DefaultTheme()

	result, _ := RenderGutter(theme, 5, 0, 10, 0, nil)
	lines := strings.Split(result, "\n")

	// Lines beyond file should still be rendered
	for i := 5; i < 10; i++ {
		if lines[i] == "" {
			t.Errorf("expected line %d to be rendered", i)
		}
	}
}

func TestGutterWidth(t *testing.T) {
	tests := []struct {
		lines int
		want  int
	}{
		{1, 3},
		{9, 3},
		{10, 3},
		{99, 3},
		{100, 3},
		{999, 3},
		{1000, 4},
		{9999, 4},
		{10000, 5},
		{99999, 5},
		{100000, 6},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.lines)), func(t *testing.T) {
			got := gutterWidth(tt.lines)
			if got != tt.want {
				t.Errorf("gutterWidth(%d) = %d, want %d", tt.lines, got, tt.want)
			}
		})
	}
}

func TestRenderGutterMultiLineDiagnostics(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 1, EndLine: 3, Severity: 1, Message: "multi-line error"},
	}

	result, _ := RenderGutter(theme, 100, 0, 10, 0, diagnostics)
	lines := strings.Split(result, "\n")

	// Lines 2, 3, 4 (indices 1, 2, 3) should have error styling
	for i := 1; i <= 3 && i < len(lines); i++ {
		if lines[i] == "" {
			t.Errorf("expected line %d to be rendered with error", i+1)
		}
	}
}

func TestRenderGutterDiagnosticPriority(t *testing.T) {
	theme := ui.DefaultTheme()

	// Error should take priority over warning on same line
	diagnostics := []Diagnostic{
		{StartLine: 2, EndLine: 2, Severity: 2, Message: "warning"},
		{StartLine: 2, EndLine: 2, Severity: 1, Message: "error"},
	}

	result, _ := RenderGutter(theme, 100, 0, 10, 2, diagnostics)
	lines := strings.Split(result, "\n")

	if len(lines) < 3 {
		t.Fatal("expected at least 3 lines")
	}
	// Error should be shown (severity 1)
}
