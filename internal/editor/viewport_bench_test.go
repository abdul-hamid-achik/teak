package editor

import (
	"testing"

	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

func createTestBuffer(lineCount int) *text.Buffer {
	var content string
	for i := 0; i < lineCount; i++ {
		content += "This is line number " + string(rune('0'+i%10)) + " with some content to make it realistic\n"
	}
	return text.NewBufferFromBytes([]byte(content))
}

func BenchmarkViewportRender24Lines(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	diagnostics := []Diagnostic{
		{StartLine: 5, EndLine: 5, Severity: 1},
		{StartLine: 15, EndLine: 15, Severity: 2},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, diagnostics, nil)
	}
}

func BenchmarkViewportRender48Lines(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(200)
	v := Viewport{Width: 120, Height: 48, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	diagnostics := []Diagnostic{
		{StartLine: 10, EndLine: 10, Severity: 1},
		{StartLine: 30, EndLine: 30, Severity: 2},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, diagnostics, nil)
	}
}

func BenchmarkViewportRenderWithSelection(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	// Set a selection
	buf.Selection = &text.Selection{
		Anchor: text.Position{Line: 5, Col: 10},
		Head:   text.Position{Line: 10, Col: 20},
	}
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, nil, nil)
	}
}

func BenchmarkViewportRenderWithGutterOpts(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{
			3:  BPActive,
			15: BPDisabled,
		},
		ExecLine: 8,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, nil, opts)
	}
}

func BenchmarkViewportRenderWithFolds(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	folds := &FoldState{
		Regions: []FoldRegion{
			{StartLine: 5, EndLine: 15, Collapsed: false},
			{StartLine: 20, EndLine: 30, Collapsed: true},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.RenderWithFolds(buf, theme, hl, nil, nil, folds)
	}
}

func BenchmarkViewportRenderWithWrap(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	wrap := NewWrapLayout(func(i int) []byte { return buf.Line(i) }, buf.LineCount(), 70)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.RenderWithWrap(buf, theme, hl, nil, nil, wrap)
	}
}
