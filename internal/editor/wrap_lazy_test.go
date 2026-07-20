package editor

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"teak/internal/text"
	"teak/internal/ui"
)

const (
	largeWrapTestBytes    = 2 << 20
	largeWrapTestLines    = 200_000
	largeWrapTestSegments = 250_000
)

func TestWrapLayoutWindowsSegmentsForPathologicalLine(t *testing.T) {
	const cacheBudget = 512
	line := bytes.Repeat([]byte("x"), largeWrapTestSegments+1)
	wrap := NewWrapLayout(func(int) []byte { return line }, 1, 1)
	if wrap.Degraded() {
		t.Fatal("a long logical line must remain word-wrapped")
	}
	if got, want := wrap.LineRows(0), len(line); got != want {
		t.Fatalf("LineRows(0) = %d, want %d", got, want)
	}

	start, end, _, ok := wrap.SegmentBounds(0, len(line)-2, len(line))
	if !ok || start != len(line)-2 || end != len(line)-1 {
		t.Fatalf("deep SegmentBounds() = (%d, %d, ok=%t), want (%d, %d, true)", start, end, ok, len(line)-2, len(line)-1)
	}
	if got := len(wrap.segmentStarts); got > cacheBudget+1 {
		t.Fatalf("cached segment starts = %d, want at most %d", got, cacheBudget+1)
	}
}

func TestLargeWrapDeepScrollAndHitTestingRemainCorrect(t *testing.T) {
	line := bytes.Repeat([]byte("x"), largeWrapTestBytes+128)
	buf := text.NewBufferFromBytes(line)
	wrap := NewWrapLayout(buf.Line, buf.LineCount(), 4)
	if wrap.Degraded() {
		t.Fatal("multi-megabyte document must not degrade word wrap")
	}

	viewport := Viewport{Width: 8, Height: 2, WrapScrollY: 100}
	rendered := ansi.Strip(viewport.RenderWithWrap(buf, ui.DefaultTheme(), nil, nil, nil, wrap))
	if rendered == "" {
		t.Fatal("deep wrapped viewport rendered no content")
	}
	if got, want := viewport.ScreenToBufferPositionWrap(0, 0, buf, 0, wrap), (text.Position{Line: 0, Col: 400}); got != want {
		t.Fatalf("deep wrapped hit test = %#v, want %#v", got, want)
	}
}

func TestEditorAppliesSingleLineWrapEditWithoutFullRebuild(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("abcd"))
	cfg := DefaultConfig()
	cfg.WordWrap = true
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(8, 10)
	if ed.Wrap == nil {
		t.Fatal("test setup requires word wrap")
	}
	builds := ed.Wrap.BuildCount()

	updated, _ := ed.Update(tea.KeyPressMsg{Text: "e"})
	if got := updated.Wrap.BuildCount(); got != builds {
		t.Fatalf("single-line edit rebuilt complete wrap layout: got %d builds, want %d", got, builds)
	}
	if got, want := updated.Wrap.LineRows(0), wrappedLineRowsWithTabs(updated.Buffer.Line(0), updated.Wrap.Width(), updated.Config.TabSize); got != want {
		t.Fatalf("LineRows(0) after edit = %d, want %d", got, want)
	}
}

func TestEditorAppliesNewlineWrapEditWithExactDeepMapping(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("first second"))
	cfg := DefaultConfig()
	cfg.WordWrap = true
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(20, 4)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})

	updated, _ := ed.Update(tea.KeyPressMsg{Text: "enter"})
	if updated.Wrap == nil || updated.Wrap.Degraded() {
		t.Fatal("newline edit disabled word wrap")
	}
	if got, want := updated.Buffer.LineCount(), 2; got != want {
		t.Fatalf("LineCount() = %d, want %d", got, want)
	}
	firstRows := updated.Wrap.LineRows(0)
	if line, segment := updated.Wrap.BufferLine(firstRows); line != 1 || segment != 0 {
		t.Fatalf("BufferLine(first row of second line) = (%d, %d), want (1, 0)", line, segment)
	}
}

func TestWrapLayoutPaginatesMillionLinesWithoutEagerReads(t *testing.T) {
	const lineCount = 1_000_000
	reads := 0
	wrap := NewWrapLayout(func(int) []byte {
		reads++
		return []byte("short source line")
	}, lineCount, 80)

	if got := reads; got != 0 {
		t.Fatalf("construction read %d lines, want 0", got)
	}
	if got, want := len(wrap.blocks), (lineCount+wrapBlockLines-1)/wrapBlockLines; got != want {
		t.Fatalf("sparse block count = %d, want %d", got, want)
	}
	if got := wrap.TotalRows(); got != lineCount {
		t.Fatalf("initial row estimate = %d, want %d", got, lineCount)
	}
	if wrap.TotalRowsKnown() {
		t.Fatal("fresh million-line layout must remain unmeasured")
	}

	if got := wrap.LineRows(0); got != 1 {
		t.Fatalf("LineRows(0) = %d, want 1", got)
	}
	if got := reads; got != wrapBlockLines {
		t.Fatalf("first viewport page read %d lines, want %d", got, wrapBlockLines)
	}
	if got := wrap.LineRows(wrapBlockLines + 1); got != 1 {
		t.Fatalf("LineRows(next page) = %d, want 1", got)
	}
	if got := reads; got != 2*wrapBlockLines {
		t.Fatalf("two pages read %d lines, want %d", got, 2*wrapBlockLines)
	}
}

func TestWrapLayoutEditInvalidatesOnlyTouchedPage(t *testing.T) {
	const lineCount = 200_000
	reads := 0
	getter := func(int) []byte {
		reads++
		return []byte("unchanged")
	}
	wrap := NewWrapLayout(getter, lineCount, 80)
	if got := wrap.LineRows(100); got != 1 {
		t.Fatalf("LineRows() = %d, want 1", got)
	}
	reads = 0
	blocks := len(wrap.blocks)
	builds := wrap.BuildCount()

	if !wrap.ApplyEdit(getter, lineCount, 100, 100, "x") {
		t.Fatal("single-line edit rejected by sparse layout")
	}
	if got := reads; got != 0 {
		t.Fatalf("ApplyEdit read %d lines, want 0", got)
	}
	if got := len(wrap.blocks); got != blocks {
		t.Fatalf("ApplyEdit rebuilt %d blocks, want %d", got, blocks)
	}
	if got := wrap.BuildCount(); got != builds {
		t.Fatalf("ApplyEdit build count = %d, want %d", got, builds)
	}
	if got := wrap.LineRows(100); got != 1 {
		t.Fatalf("LineRows() after edit = %d, want 1", got)
	}
	if got := reads; got != wrapBlockLines {
		t.Fatalf("edited page reread %d lines, want %d", got, wrapBlockLines)
	}
}

func TestWrapLayoutEvictsHydratedPagesButKeepsExactTotals(t *testing.T) {
	lineCount := wrapBlockLines * (maxWrapResidentBlocks + 12)
	wrap := NewWrapLayout(func(int) []byte { return []byte("short") }, lineCount, 80)
	for line := 0; line < lineCount; line += wrapBlockLines {
		if got := wrap.LineRows(line); got != 1 {
			t.Fatalf("LineRows(%d) = %d, want 1", line, got)
		}
	}

	resident := 0
	for _, block := range wrap.blocks {
		if block.rows != nil {
			resident++
		}
	}
	if resident > maxWrapResidentBlocks {
		t.Fatalf("resident row pages = %d, want at most %d", resident, maxWrapResidentBlocks)
	}
	if !wrap.TotalRowsKnown() {
		t.Fatal("visiting every page must retain exact block totals")
	}
	if got := wrap.TotalRows(); got != lineCount {
		t.Fatalf("TotalRows() = %d, want %d", got, lineCount)
	}
	if wrap.blocks[0].rows != nil {
		t.Fatal("least-recently-used first page should have been evicted")
	}
}

func TestLazyWrapResolvesDeepRowsAndClampsOverscrollAtEOF(t *testing.T) {
	lineCount := wrapBlockLines*3 + 7
	wrap := NewWrapLayout(func(int) []byte { return []byte("abcdef") }, lineCount, 4)
	targetLine := wrapBlockLines + 5
	targetRow := targetLine*2 + 1
	if line, segment := wrap.BufferLine(targetRow); line != targetLine || segment != 1 {
		t.Fatalf("BufferLine(%d) = (%d, %d), want (%d, 1)", targetRow, line, segment, targetLine)
	}
	if got, want := wrap.VisualRow(targetLine), targetLine*2; got != want {
		t.Fatalf("VisualRow(%d) = %d, want %d", targetLine, got, want)
	}

	buf := text.NewBufferFromBytes([]byte("abcdefgh"))
	single := NewWrapLayout(buf.Line, buf.LineCount(), 4)
	viewport := Viewport{Width: 8, Height: 2, WrapScrollY: 999}
	_ = viewport.RenderWithWrap(buf, ui.DefaultTheme(), nil, nil, nil, single)
	if got := viewport.WrapScrollY; got != 0 {
		t.Fatalf("overscroll after EOF resolution = %d, want 0", got)
	}
}

func BenchmarkWrapLayoutLargeLogicalLine(b *testing.B) {
	line := bytes.Repeat([]byte("wrapped source text "), 250_000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		wrap := NewWrapLayout(func(int) []byte { return line }, 1, 80)
		_, _, _, _ = wrap.SegmentBounds(0, wrap.LineRows(0)-1, len(line))
	}
}

func BenchmarkWrapLayoutInitializeMillionLines(b *testing.B) {
	const lineCount = 1_000_000
	line := []byte("short source line")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = NewWrapLayout(func(int) []byte { return line }, lineCount, 80)
	}
}

func BenchmarkWrapLayoutTypingInTwoHundredThousandLines(b *testing.B) {
	const lineCount = 200_000
	line := []byte("short source line")
	getter := func(int) []byte { return line }
	wrap := NewWrapLayout(getter, lineCount, 80)
	_ = wrap.LineRows(100) // establish one measured viewport page
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !wrap.ApplyEdit(getter, lineCount, 100, 100, "x") {
			b.Fatal("ApplyEdit rejected stable single-line mutation")
		}
		_ = wrap.LineRows(100)
	}
}
