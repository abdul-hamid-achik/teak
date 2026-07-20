package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"teak/internal/text"
)

func BenchmarkSavePreflightUnchanged8MiB(b *testing.B) {
	data := bytes.Repeat([]byte("0123456789abcdef"), (8<<20)/16)
	path := filepath.Join(b.TempDir(), "large.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	expected := text.New(data)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		observed, equal, err := compareSaveDestination(context.Background(), path, expected)
		if err != nil {
			b.Fatal(err)
		}
		if !equal || observed != nil {
			b.Fatal("unchanged destination failed preflight")
		}
	}
}
