package text

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRopePrefixLinesIsBoundedWithoutBuildingLargeLineIndex(t *testing.T) {
	const lineCount = maxCachedLineIndexEntries + 1
	r := NewFromString(strings.Repeat("x\n", lineCount))

	got := string(r.PrefixLines(3, 16))
	if want := "x\nx\nx\n"; got != want {
		t.Fatalf("PrefixLines() = %q, want %q", got, want)
	}
	if r.lineIndex != nil {
		t.Fatal("bounded prefix unexpectedly built a full-file line index")
	}

	if got := r.LineStart(lineCount - 1); got <= 0 {
		t.Fatalf("LineStart() = %d, want a valid offset", got)
	}
	if r.lineIndex != nil {
		t.Fatal("large Rope.LineStart unexpectedly built a full-file line index")
	}
}

func TestRopeBytesContextHonorsCancellation(t *testing.T) {
	r := NewFromString("content")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got, err := r.BytesContext(ctx); got != nil || err != context.Canceled {
		t.Fatalf("BytesContext(canceled) = (%q, %v), want (nil, context.Canceled)", got, err)
	}
}

func TestRopeEqualBytesComparesLeavesWithoutMaterializing(t *testing.T) {
	content := strings.Repeat("ab😀\n", 300)
	rope := NewFromString(content)

	if !rope.EqualBytes([]byte(content)) {
		t.Fatal("EqualBytes rejected identical content")
	}
	different := []byte(content)
	different[len(different)-2] ^= 1
	if rope.EqualBytes(different) {
		t.Fatal("EqualBytes accepted different content")
	}
	if rope.EqualBytes(different[:len(different)-1]) {
		t.Fatal("EqualBytes accepted a different length")
	}
}

func TestRopeEqualReaderComparesWithoutFlattening(t *testing.T) {
	content := bytes.Repeat([]byte("abc😀\n"), 2_000)
	rope := New(content)
	different := append([]byte(nil), content...)
	different[len(different)/2] ^= 1
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "equal", data: content, want: true},
		{name: "different same length", data: different, want: false},
		{name: "short", data: content[:len(content)-1], want: false},
		{name: "long", data: append(append([]byte(nil), content...), 'x'), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rope.EqualReader(bytes.NewReader(tt.data))
			if err != nil {
				t.Fatalf("EqualReader() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EqualReader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRopeWriteToStreamsLeavesInOrder(t *testing.T) {
	content := strings.Repeat("stream 😀\n", 300)
	rope := NewFromString(content)
	var dst bytes.Buffer

	written, err := rope.WriteTo(&dst)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if written != int64(len(content)) {
		t.Fatalf("WriteTo() wrote %d bytes, want %d", written, len(content))
	}
	if got := dst.String(); got != content {
		t.Fatalf("WriteTo() content mismatch: got %d bytes", len(got))
	}
}
