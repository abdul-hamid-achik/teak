package editor

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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
	hl.Tokenize(buf.Bytes())
	diagnostics := []Diagnostic{
		{StartLine: 5, EndLine: 5, Severity: 1},
		{StartLine: 15, EndLine: 15, Severity: 2},
	}

	b.ReportAllocs()
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
	hl.Tokenize(buf.Bytes())
	diagnostics := []Diagnostic{
		{StartLine: 10, EndLine: 10, Severity: 1},
		{StartLine: 30, EndLine: 30, Severity: 2},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, diagnostics, nil)
	}
}

// BenchmarkViewportRenderPlainText48Lines covers the untokenized branch
// deliberately: no lexer has run, so Viewport.Render falls back to
// unstyled text. This is a real state (e.g. the first frame before
// tokenization completes), kept as its own honestly-named benchmark rather
// than an accidental default.
func BenchmarkViewportRenderPlainText48Lines(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(200)
	v := Viewport{Width: 120, Height: 48, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	diagnostics := []Diagnostic{
		{StartLine: 10, EndLine: 10, Severity: 1},
		{StartLine: 30, EndLine: 30, Severity: 2},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, diagnostics, nil)
	}
}

func BenchmarkViewportRenderWithSelection(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	// Set a selection
	buf.Selections = text.NewSelections(text.Position{Line: 5, Col: 10})
	buf.Selections.Add(text.Selection{
		Anchor: text.Position{Line: 5, Col: 10},
		Head:   text.Position{Line: 10, Col: 20},
	})
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	hl.Tokenize(buf.Bytes())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, nil, nil)
	}
}

func BenchmarkViewportRender1000Selections60Lines(b *testing.B) {
	theme := ui.NordTheme()
	buf := text.NewBufferFromBytes(makeSelectionTestDocument(1_000, 80))
	selections := text.NewSelections(text.Position{Line: 0, Col: 1})
	for line := 1; line < text.MaxSelections; line++ {
		selections.Add(text.Selection{
			Anchor: text.Position{Line: line, Col: 1},
			Head:   text.Position{Line: line, Col: 12},
		})
	}
	buf.Selections = selections
	buf.Cursor = selections.PrimaryCursor()
	v := Viewport{Width: 100, Height: 60, ScrollY: 470}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, nil, nil, nil)
	}
}

func BenchmarkViewportRenderWithGutterOpts(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(100)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	hl.Tokenize(buf.Bytes())
	opts := &GutterOpts{
		Breakpoints: map[int]BreakpointState{
			3:  BPActive,
			15: BPDisabled,
		},
		ExecLine: 8,
	}

	b.ReportAllocs()
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
	hl.Tokenize(buf.Bytes())
	folds := &FoldState{
		Regions: []FoldRegion{
			{StartLine: 5, EndLine: 15, Collapsed: false},
			{StartLine: 20, EndLine: 30, Collapsed: true},
		},
	}

	b.ReportAllocs()
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
	hl.Tokenize(buf.Bytes())
	wrap := NewWrapLayout(func(i int) []byte { return buf.Line(i) }, buf.LineCount(), 70)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.RenderWithWrap(buf, theme, hl, nil, nil, wrap)
	}
}

func BenchmarkWrapLayoutRebuild(b *testing.B) {
	line := []byte("package teak // representative source line with enough text to wrap\n")
	buf := text.NewBufferFromBytes(bytes.Repeat(line, 20_000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 80, 4)
	}
}

func BenchmarkEditorSetSizeSameWrap(b *testing.B) {
	buf := text.NewBufferFromBytes(bytes.Repeat([]byte("some ordinary source text that wraps\n"), 2_000))
	cfg := DefaultConfig()
	cfg.WordWrap = true
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(80, 24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ed.SetSize(80, 24)
	}
}

func BenchmarkWrapDeepSegmentLookup(b *testing.B) {
	line := strings.Repeat("identifier ", 25_000) + "\t你"
	buf := text.NewBufferFromBytes([]byte(line))
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 80, 4)
	segment := wrap.LineRows(0) - 1

	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _, _, _ = wrap.SegmentBounds(0, segment, len(line))
		}
	})
	b.Run("stateless_rescan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _, _, _ = wrappedLineSegmentWithTabs(line, segment, 80, 4)
		}
	})
}

func BenchmarkViewportRenderWithWrapDeepLongLine(b *testing.B) {
	theme := ui.NordTheme()
	line := strings.Repeat("identifier ", 25_000) + "\t你"
	buf := text.NewBufferFromBytes([]byte(line))
	v := Viewport{Width: 90, Height: 24}
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 80, 4)
	v.WrapScrollY = wrap.LineRows(0) - v.Height

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.RenderWithWrap(buf, theme, nil, nil, nil, wrap)
	}
}

func BenchmarkViewportRenderWithWrapDeepLongLineTokens(b *testing.B) {
	theme := ui.NordTheme()
	line := strings.Repeat("identifier ", 25_000) + "\t你"
	buf := text.NewBufferFromBytes([]byte(line))
	v := Viewport{Width: 90, Height: 24}
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 80, 4)
	v.WrapScrollY = wrap.LineRows(0) - v.Height
	hl := highlight.New("test.go", theme)
	hl.Tokenize(buf.Bytes())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.RenderWithWrap(buf, theme, hl, nil, nil, wrap)
	}
}

// Large file benchmarks

func createLargeGoBuffer(lineCount int) *text.Buffer {
	var content strings.Builder
	content.Grow(lineCount * 96)
	for i := 0; i < lineCount; i++ {
		_, _ = fmt.Fprintf(&content, "func testFunction%d() string {\n\treturn \"this is a test string for line %d with some content\"\n}\n\n", i, i)
	}
	return text.NewBufferFromBytes([]byte(content.String()))
}

func BenchmarkLargeFile10KTokenizeFull(b *testing.B) {
	theme := ui.NordTheme()
	buf := createLargeGoBuffer(10000) // ~10K lines
	hl := highlight.New("test.go", theme)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hl.Tokenize(buf.Bytes())
	}
}

func BenchmarkLargeFile10KTokenizeViewport(b *testing.B) {
	theme := ui.NordTheme()
	buf := createLargeGoBuffer(10000)
	hl := highlight.New("test.go", theme)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		viewStart := (i * 100) % (buf.LineCount() - 100)
		snapshot := highlight.CaptureViewport(buf.Rope(), viewStart, viewStart+24)
		batch, complete := hl.TokenizeViewportSnapshotBatch(ctx, snapshot)
		if !complete || len(batch.Lines) == 0 {
			b.Fatal("expected a non-empty viewport token batch")
		}
	}
}

func BenchmarkLargeFile10KScroll(b *testing.B) {
	theme := ui.NordTheme()
	buf := createLargeGoBuffer(10000)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)
	ctx := context.Background()

	// Pre-tokenize and install the first viewport through the same sparse batch
	// path used by Editor's asynchronous tokenization command.
	initial := highlight.CaptureViewport(buf.Rope(), 0, 24)
	batch, complete := hl.TokenizeViewportSnapshotBatch(ctx, initial)
	if !complete {
		b.Fatal("initial viewport tokenization was canceled")
	}
	hl.MergeBatch(batch)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := (i * 10) % (buf.LineCount() - 24)
		v.ScrollY = start
		snapshot := highlight.CaptureViewport(buf.Rope(), start, start+24)
		batch, complete := hl.TokenizeViewportSnapshotBatch(ctx, snapshot)
		if !complete {
			b.Fatal("viewport tokenization was canceled")
		}
		hl.MergeBatch(batch)
		_ = v.Render(buf, theme, hl, nil, nil)
	}
}

func BenchmarkLargeFile100KTokenizeViewport(b *testing.B) {
	theme := ui.NordTheme()
	buf := createLargeGoBuffer(100000) // ~100K lines, ~10MB
	hl := highlight.New("test.go", theme)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		viewStart := (i * 1000) % (buf.LineCount() - 100)
		snapshot := highlight.CaptureViewport(buf.Rope(), viewStart, viewStart+24)
		batch, complete := hl.TokenizeViewportSnapshotBatch(ctx, snapshot)
		if !complete || len(batch.Lines) == 0 {
			b.Fatal("expected a non-empty viewport token batch")
		}
	}
}

func BenchmarkViewportCacheHit(b *testing.B) {
	theme := ui.NordTheme()
	buf := createTestBuffer(1000)
	v := Viewport{Width: 80, Height: 24, ScrollY: 0}
	hl := highlight.New("test.go", theme)

	// Viewport.Render never writes tokens back into the Highlighter, so
	// there is no viewport-side cache to warm by rendering once. The "cache
	// hit" this measures is the Highlighter's own token cache: tokenize once
	// up front, then render the same viewport repeatedly against already
	// tokenized content, which is the common case while scrolling within a
	// range that has already been highlighted.
	hl.Tokenize(buf.Bytes())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Render(buf, theme, hl, nil, nil)
	}
}
