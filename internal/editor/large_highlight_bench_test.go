package editor

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

func BenchmarkLargeInitialTokenizationSparse(b *testing.B) {
	buf := text.NewBufferFromBytes(bytes.Repeat([]byte("x\n"), maxFullHighlightLines+1))
	buf.FilePath = "large.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := ed.ScheduleInitialTokenize()()
		complete, ok := msg.(TokenizeCompleteMsg)
		if !ok || !complete.Partial || len(complete.Batch.Lines) == 0 {
			b.Fatal("expected sparse initial tokenization")
		}
	}
}

func BenchmarkLargeFile10KEditTokenize(b *testing.B) {
	buf := createLargeGoBuffer(10_000)
	buf.FilePath = "large.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cmd tea.Cmd
		ed, cmd = ed.Update(RetokenizeMsg{EditorID: ed.ID(), Version: buf.Version()})
		msg, ok := cmd().(TokenizeCompleteMsg)
		if !ok || !msg.Partial {
			b.Fatalf("edit tokenization result = %#v, want partial result", msg)
		}
		ed, _ = ed.Update(msg)
	}
}
