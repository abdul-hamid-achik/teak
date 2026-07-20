package highlight

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestTokenizeViewportSnapshotBatchStaysSparse(t *testing.T) {
	const totalLines = 64_000_000
	const startLine = totalLines - 3
	snapshot := ViewportSnapshot{
		Content:   []byte("first\nsecond\nthird\n"),
		LineCount: totalLines,
		StartLine: startLine,
		ViewStart: startLine,
		ViewEnd:   totalLines,
	}
	h := New("test.go", ui.DefaultTheme())

	batch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), snapshot)
	if !complete {
		t.Fatal("expected completed tokenization")
	}
	if batch.TotalLines != totalLines {
		t.Fatalf("TotalLines = %d, want %d", batch.TotalLines, totalLines)
	}
	if batch.StartLine != startLine {
		t.Fatalf("StartLine = %d, want %d", batch.StartLine, startLine)
	}
	if got := len(batch.Lines); got > 4 {
		t.Fatalf("sparse batch has %d entries, want only captured lines", got)
	}

	h.MergeBatch(batch)
	if got := tokenText(h.Line(startLine)); got != "first" {
		t.Fatalf("cached sparse line = %q, want %q", got, "first")
	}
	if !h.CoversRange(startLine, startLine+3) {
		t.Fatal("captured sparse range is not marked covered")
	}
	if h.CoversRange(startLine, startLine+4) {
		t.Fatal("line outside the captured snapshot was incorrectly marked covered")
	}
	if h.CoversRange(startLine-1, startLine) {
		t.Fatal("uncaptured line was incorrectly marked covered")
	}
}

func TestViewportBatchCoversOnlyMaterializedContext(t *testing.T) {
	const (
		viewStart = 500
		viewEnd   = 510
	)
	buf := text.NewBufferFromBytes(makeNumberedLines(1_000, "before"))
	h := New("test.go", ui.DefaultTheme())

	batch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), CaptureViewport(buf.Rope(), viewStart, viewEnd))
	if !complete {
		t.Fatal("expected completed tokenization")
	}
	if got, want := batch.StartLine, viewStart-50; got != want {
		t.Fatalf("batch start = %d, want materialized start %d", got, want)
	}
	if got, want := len(batch.Lines), viewEnd-viewStart+100; got != want {
		t.Fatalf("batch line count = %d, want materialized context %d", got, want)
	}

	h.MergeBatch(batch)
	if !h.CoversRange(viewStart-50, viewEnd+50) {
		t.Fatal("materialized viewport context is not covered")
	}
	if h.CoversRange(viewStart-200, viewStart-50) {
		t.Fatal("captured but unmaterialized prefix was incorrectly marked covered")
	}
	if h.CoversRange(viewEnd+50, viewEnd+51) {
		t.Fatal("uncaptured suffix was incorrectly marked covered")
	}
}

func TestInvalidateDropsSparseCoverageAndStaleTokens(t *testing.T) {
	const (
		viewStart = 500
		viewEnd   = 510
	)
	h := New("test.go", ui.DefaultTheme())
	oldBuf := text.NewBufferFromBytes(makeNumberedLines(1_000, "old"))
	oldBatch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), CaptureViewport(oldBuf.Rope(), viewStart, viewEnd))
	if !complete {
		t.Fatal("expected old tokenization to complete")
	}
	h.MergeBatch(oldBatch)
	if got := tokenText(h.Line(viewStart)); got == "" {
		t.Fatal("expected cached viewport token before invalidation")
	}

	h.Invalidate()
	if !h.IsDirty() {
		t.Fatal("highlighter should be dirty after invalidation")
	}
	if got := h.Line(viewStart); got != nil {
		t.Fatalf("stale tokens survived invalidation: %q", tokenText(got))
	}
	if h.CoversRange(viewStart, viewEnd) {
		t.Fatal("stale viewport coverage survived invalidation")
	}
	if start, end := h.TokenizedRange(); start != -1 || end != -1 {
		t.Fatalf("tokenized range = (%d, %d), want (-1, -1)", start, end)
	}

	newBuf := text.NewBufferFromBytes(makeNumberedLines(1_000, "fresh"))
	newBatch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), CaptureViewport(newBuf.Rope(), viewStart, viewEnd))
	if !complete {
		t.Fatal("expected fresh tokenization to complete")
	}
	h.MergeBatch(newBatch)
	if got := tokenText(h.Line(viewStart)); got != "fresh-0500" {
		t.Fatalf("returning viewport cached %q, want fresh content", got)
	}
}

func makeNumberedLines(count int, prefix string) []byte {
	var content bytes.Buffer
	for line := 0; line < count; line++ {
		fmt.Fprintf(&content, "%s-%04d\n", prefix, line)
	}
	return content.Bytes()
}
