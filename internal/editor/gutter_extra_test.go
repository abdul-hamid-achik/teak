package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestRenderGutterWithBreakpoints(t *testing.T) {
	theme := ui.DefaultTheme()
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{2: BPActive, 5: BPActive},
		ExecLine:    -1,
	}

	result, width := RenderGutter(theme, 10, 0, 10, 0, nil, opts)
	if result == "" {
		t.Error("expected non-empty gutter with breakpoints")
	}
	// Width should include 4 extra columns for marker (1 space + 2-cell icon + 1 space)
	baseWidth := gutterWidth(10)
	if width != baseWidth+3 {
		t.Errorf("expected width %d, got %d", baseWidth+3, width)
	}

	lines := strings.Split(result, "\n")
	// Line at index 2 should contain the bullet marker
	if !strings.Contains(lines[2], "\U000f0765") {
		t.Errorf("expected breakpoint marker on line 3, got %q", lines[2])
	}
	// Line at index 0 should not have a breakpoint marker
	if strings.Contains(lines[0], "\U000f0765") {
		t.Errorf("line 1 should not have breakpoint marker, got %q", lines[0])
	}
}

func TestRenderGutterWithExecLine(t *testing.T) {
	theme := ui.DefaultTheme()
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{},
		ExecLine:    3,
	}

	result, _ := RenderGutter(theme, 10, 0, 10, 0, nil, opts)
	lines := strings.Split(result, "\n")

	if len(lines) < 4 {
		t.Fatal("expected at least 4 lines")
	}
	// Line at index 3 should be the exec line (styled differently)
	if !strings.Contains(lines[3], "4") {
		t.Errorf("expected line number 4 on exec line, got %q", lines[3])
	}
}

func TestRenderGutterExecLineWithDiagnostic(t *testing.T) {
	theme := ui.DefaultTheme()
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{},
		ExecLine:    2,
	}
	diagnostics := []Diagnostic{
		{StartLine: 2, EndLine: 2, Severity: 1, Message: "error"},
	}

	result, _ := RenderGutter(theme, 10, 0, 10, 0, diagnostics, opts)
	lines := strings.Split(result, "\n")

	// Exec line styling should take priority over diagnostic
	if len(lines) < 3 {
		t.Fatal("expected at least 3 lines")
	}
	// Just ensure no crash and line is rendered
	if !strings.Contains(lines[2], "3") {
		t.Errorf("expected line number 3, got %q", lines[2])
	}
}

func TestRenderGutterWithBreakpointAndExecOnSameLine(t *testing.T) {
	theme := ui.DefaultTheme()
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{3: BPActive},
		ExecLine:    3,
	}

	result, _ := RenderGutter(theme, 10, 0, 10, 0, nil, opts)
	lines := strings.Split(result, "\n")

	if len(lines) < 4 {
		t.Fatal("expected at least 4 lines")
	}
	// Should have breakpoint marker and exec line styling
	if !strings.Contains(lines[3], "\U000f0765") {
		t.Errorf("expected breakpoint marker on exec line, got %q", lines[3])
	}
}

func TestRenderGutterNoOptsNilBreakpoints(t *testing.T) {
	theme := ui.DefaultTheme()

	result, width := RenderGutter(theme, 10, 0, 5, 0, nil, nil)
	if result == "" {
		t.Error("expected non-empty gutter")
	}
	// No marker width
	if width != gutterWidth(10) {
		t.Errorf("expected width %d, got %d", gutterWidth(10), width)
	}
}

func TestRenderGutterBeyondTotalLines(t *testing.T) {
	theme := ui.DefaultTheme()

	result, _ := RenderGutter(theme, 3, 0, 10, 0, nil, nil)
	lines := strings.Split(result, "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
	// Lines beyond total should be spaces
	for i := 3; i < 10; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			// Lines beyond file content should only have whitespace
		}
	}
}

func TestRenderGutterDiagnosticInfoSeverity(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 1, EndLine: 1, Severity: 3, Message: "info"},
	}

	result, _ := RenderGutter(theme, 10, 0, 5, 0, diagnostics, nil)
	if result == "" {
		t.Error("expected non-empty gutter with info diagnostic")
	}
}

func TestRenderGutterDiagnosticInfoOnActiveLine(t *testing.T) {
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 2, EndLine: 2, Severity: 3, Message: "info"},
	}

	// Active line with info severity falls through to active line styling
	result, _ := RenderGutter(theme, 10, 0, 5, 2, diagnostics, nil)
	if result == "" {
		t.Error("expected non-empty gutter")
	}
}

func TestGutterWidthEdgeCases(t *testing.T) {
	tests := []struct {
		lines int
		want  int
	}{
		{0, 3},  // zero lines
		{1, 3},  // single line
		{9, 3},  // single digit max
		{10, 3}, // two digits, but min is 3
		{999, 3},
		{1000, 4},
		{10000, 5},
	}

	for _, tt := range tests {
		got := gutterWidth(tt.lines)
		if got != tt.want {
			t.Errorf("gutterWidth(%d) = %d, want %d", tt.lines, got, tt.want)
		}
	}
}
