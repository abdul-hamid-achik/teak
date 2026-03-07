package editor

import (
	"testing"

	"teak/internal/ui"
)

func BenchmarkRenderGutter24Lines(b *testing.B) {
	theme := ui.NordTheme()
	diagnostics := []Diagnostic{
		{StartLine: 5, EndLine: 5, Severity: 1},
		{StartLine: 10, EndLine: 10, Severity: 2},
		{StartLine: 20, EndLine: 20, Severity: 3},
	}
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{
			3:  BPActive,
			15: BPDisabled,
		},
		ExecLine: 8,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderGutter(theme, 1000, 0, 24, 5, diagnostics, opts)
	}
}

func BenchmarkRenderGutter48Lines(b *testing.B) {
	theme := ui.NordTheme()
	diagnostics := []Diagnostic{
		{StartLine: 10, EndLine: 10, Severity: 1},
		{StartLine: 25, EndLine: 25, Severity: 2},
		{StartLine: 40, EndLine: 40, Severity: 3},
	}
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{
			5:  BPActive,
			30: BPDisabled,
		},
		ExecLine: 15,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderGutter(theme, 1000, 0, 48, 10, diagnostics, opts)
	}
}

func BenchmarkRenderGutterNoOpts(b *testing.B) {
	theme := ui.NordTheme()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderGutter(theme, 1000, 0, 24, 5, nil, nil)
	}
}

func BenchmarkRenderGutterWithFolds24Lines(b *testing.B) {
	theme := ui.NordTheme()
	diagnostics := []Diagnostic{
		{StartLine: 5, EndLine: 5, Severity: 1},
		{StartLine: 10, EndLine: 10, Severity: 2},
	}
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{
			3: BPActive,
		},
		ExecLine: 8,
	}
	folds := &FoldState{
		Regions: []FoldRegion{
			{StartLine: 5, EndLine: 15, Collapsed: false},
			{StartLine: 20, EndLine: 30, Collapsed: true},
		},
	}
	visibleLines := []int{0, 1, 2, 3, 4, 5, 16, 17, 18, 19, 20, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderGutterWithFolds(theme, 1000, 0, 24, 5, diagnostics, opts, folds, visibleLines)
	}
}
