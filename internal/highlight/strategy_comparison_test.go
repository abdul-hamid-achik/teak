package highlight

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

const (
	comparisonCorpusFunctions = 2_000 // five physical lines each: 10,000 lines
	comparisonViewStart       = 5_000
	comparisonViewEnd         = 5_024
)

// TestViewportStrategyMatchesFullStrategy verifies the contract that permits
// large interactive edits to use the sparse path: the materialized visible
// window must have the same styled token stream as a full Chroma pass. The
// test deliberately uses ordinary line-local Go, which is the safe baseline;
// long multiline constructs remain documented as a reason to prefer a real
// incremental parser before changing the strategy.
func TestViewportStrategyMatchesFullStrategy(t *testing.T) {
	content := comparisonCorpus()
	buf := text.NewBufferFromBytes(content)
	theme := ui.DefaultTheme()

	full := New("representative.go", theme)
	full.Tokenize(content)
	sparse := New("representative.go", theme)
	batch, complete := sparse.TokenizeViewportSnapshotBatch(
		context.Background(),
		CaptureViewport(buf.Rope(), comparisonViewStart, comparisonViewEnd),
	)
	if !complete {
		t.Fatal("viewport tokenization did not complete")
	}
	sparse.MergeBatch(batch)

	for line := batch.StartLine; line < batch.StartLine+len(batch.Lines); line++ {
		if got, want := lineSignature(sparse.Line(line)), lineSignature(full.Line(line)); got != want {
			t.Fatalf("line %d sparse signature differs from full pass:\n got  %q\n want %q", line, got, want)
		}
	}
}

// BenchmarkHighlightStrategyComparison is the reproducible baseline for a
// future parser comparison. It keeps construction and the immutable source
// snapshot outside the timed loops; the incremental case measures invalidation,
// window tokenization, and cache merge rather than accidentally benchmarking
// setup work.
func BenchmarkHighlightStrategyComparison(b *testing.B) {
	content := comparisonCorpus()
	buf := text.NewBufferFromBytes(content)
	snapshot := CaptureViewport(buf.Rope(), comparisonViewStart, comparisonViewEnd)
	theme := ui.DefaultTheme()

	b.Run("full-file", func(b *testing.B) {
		h := New("representative.go", theme)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			h.TokenizeToLines(content)
		}
	})

	b.Run("viewport-window", func(b *testing.B) {
		h := New("representative.go", theme)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if batch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), snapshot); !complete || len(batch.Lines) == 0 {
				b.Fatal("viewport strategy returned no batch")
			}
		}
	})

	b.Run("incremental-cache-refresh", func(b *testing.B) {
		h := New("representative.go", theme)
		h.Tokenize(content)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			h.InvalidateEdited(comparisonViewStart, comparisonViewStart, 0)
			batch, complete := h.TokenizeViewportSnapshotBatch(context.Background(), snapshot)
			if !complete || len(batch.Lines) == 0 {
				b.Fatal("incremental strategy returned no batch")
			}
			h.MergeBatch(batch)
		}
	})
}

func comparisonCorpus() []byte {
	var content bytes.Buffer
	content.Grow(comparisonCorpusFunctions * 72)
	for line := range comparisonCorpusFunctions {
		fmt.Fprintf(&content, "func handler%04d(value int) (int, error) {\n", line)
		fmt.Fprintf(&content, "\tmessage := \"value=%d\"\n", line)
		content.WriteString("\tif value < 0 { return 0, errors.New(message) }\n")
		content.WriteString("\treturn value + 1, nil\n}\n")
	}
	return content.Bytes()
}

func lineSignature(tokens []StyledToken) string {
	var signature strings.Builder
	for _, token := range tokens {
		signature.WriteString(token.Text)
		signature.WriteByte(0)
		signature.WriteString(token.Style.Render(token.Text))
		signature.WriteByte(1)
	}
	return signature.String()
}
