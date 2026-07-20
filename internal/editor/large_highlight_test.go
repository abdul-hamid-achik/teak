package editor

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestLargeInitialTokenizationUsesSparseViewportBatch(t *testing.T) {
	buf := text.NewBufferFromBytes(bytes.Repeat([]byte("x\n"), maxFullHighlightLines+1))
	buf.FilePath = "large.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := ed.ScheduleInitialTokenize()
	if cmd == nil {
		t.Fatal("expected initial tokenization command")
	}
	msg, ok := cmd().(TokenizeCompleteMsg)
	if !ok {
		t.Fatalf("command result = %T, want TokenizeCompleteMsg", msg)
	}
	if !msg.Partial {
		t.Fatal("large buffer scheduled a full tokenization")
	}
	if got := len(msg.Batch.Lines); got > 500 {
		t.Fatalf("batch contains %d lines, want only the viewport and margin", got)
	}
	if got := msg.Batch.TotalLines; got != buf.LineCount() {
		t.Fatalf("batch total lines = %d, want %d", got, buf.LineCount())
	}
}

func TestViewportCoverageDoesNotHideUnmaterializedCaptureGap(t *testing.T) {
	buf := text.NewBufferFromBytes(bytes.Repeat([]byte("var value = 42\n"), 1_000))
	buf.FilePath = "large.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.Viewport.ScrollY = 500
	ed.Viewport.Height = 10

	var cmd tea.Cmd
	ed, cmd = ed.Update(RetokenizeMsg{
		EditorID:     ed.ID(),
		Version:      buf.Version(),
		ViewportOnly: true,
	})
	if cmd == nil {
		t.Fatal("expected viewport tokenization command")
	}
	msg, ok := cmd().(TokenizeCompleteMsg)
	if !ok {
		t.Fatalf("command result = %T, want TokenizeCompleteMsg", msg)
	}
	ed, _ = ed.Update(msg)

	// CaptureViewport retains 200 leading lines for lexer state, while the
	// sparse batch styles only 50 lines of that context. Scrolling into the
	// remaining captured-but-unmaterialized gap must schedule fresh work.
	ed.Viewport.ScrollY = 350
	if !ed.needsRetokenize() {
		t.Fatal("captured but unmaterialized viewport gap was treated as covered")
	}
}
