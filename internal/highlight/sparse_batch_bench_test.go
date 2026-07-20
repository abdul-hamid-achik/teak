package highlight

import (
	"context"
	"testing"

	"teak/internal/ui"
)

func BenchmarkTokenizeViewportSnapshotBatch64MLineMetadata(b *testing.B) {
	snapshot := ViewportSnapshot{
		Content:   []byte("package main\nfunc main() {}\n"),
		LineCount: 64_000_000,
		StartLine: 32_000_000,
		ViewStart: 32_000_000,
		ViewEnd:   32_000_002,
	}
	h := New("test.go", ui.DefaultTheme())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), snapshot)
		if !complete || len(batch.Lines) == 0 {
			b.Fatal("expected sparse viewport tokens")
		}
	}
}
