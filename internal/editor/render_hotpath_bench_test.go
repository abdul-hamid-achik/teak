package editor

import (
	"strings"
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func BenchmarkRenderGutterLargeDiagnosticRange(b *testing.B) {
	theme := ui.NordTheme()
	diagnostics := []Diagnostic{{StartLine: 0, EndLine: 5_000_000, Severity: 1}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderGutter(theme, 5_000_001, 2_000_000, 24, 2_000_010, diagnostics, nil)
	}
}

func BenchmarkFindMatchingBracketWithinBudget(b *testing.B) {
	buf := text.NewBufferFromBytes([]byte("(" + strings.Repeat("x", 2*MaxBracketScanBytes) + ")"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FindMatchingBracketWithinBudget(buf, text.Position{}, MaxBracketScanBytes)
	}
}

func BenchmarkFoldStateLargeSparseRegions(b *testing.B) {
	regions := make([]FoldRegion, 0, 10_000)
	for i := 0; i < 10_000; i++ {
		start := i * 100
		regions = append(regions, FoldRegion{StartLine: start, EndLine: start + 50, Collapsed: true})
	}
	var fs FoldState
	fs.SetRegions(regions)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fs.TotalVisibleLines(1_000_000)
		_ = fs.VisualLineToBuffer(500_000, 1_000_000)
		_ = fs.VisibleLines(500_000, 24, 1_000_000)
	}
}
