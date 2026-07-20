package editor

import (
	"bytes"
	"testing"

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
