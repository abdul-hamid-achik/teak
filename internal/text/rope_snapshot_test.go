package text

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestRopePrefixLinesAndLineStartDoNotAllocatePerDocument(t *testing.T) {
	const lineCount = 250_001
	r := NewFromString(strings.Repeat("x\n", lineCount))

	got := string(r.PrefixLines(3, 16))
	if want := "x\nx\nx\n"; got != want {
		t.Fatalf("PrefixLines() = %q, want %q", got, want)
	}

	if got := r.LineStart(lineCount - 1); got <= 0 {
		t.Fatalf("LineStart() = %d, want a valid offset", got)
	}

	// LineStart descends the tree instead of materializing a whole-document
	// offset table, so its cost must not scale with the document. Allocating
	// here at all would mean the table is back.
	if allocs := testing.AllocsPerRun(50, func() {
		r.LineStart(lineCount / 2)
	}); allocs != 0 {
		t.Errorf("LineStart allocated %v times per call, want 0", allocs)
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

func TestRopeReadAtStreamsAcrossLeaves(t *testing.T) {
	rope := join(
		newLeafOwned([]byte("abc")),
		join(newLeafOwned([]byte("def")), newLeafOwned([]byte("ghi"))),
	)
	tests := []struct {
		name    string
		offset  int64
		size    int
		want    string
		wantEOF bool
		wantErr bool
	}{
		{name: "within leaf", offset: 0, size: 2, want: "ab"},
		{name: "across leaves", offset: 2, size: 5, want: "cdefg"},
		{name: "partial at end", offset: 7, size: 4, want: "hi", wantEOF: true},
		{name: "at end", offset: 9, size: 1, wantEOF: true},
		{name: "past end", offset: 20, size: 1, wantEOF: true},
		{name: "empty destination", offset: 9, size: 0},
		{name: "negative offset", offset: -1, size: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, tt.size)
			n, err := rope.ReadAt(dst, tt.offset)
			if tt.wantEOF {
				if err != io.EOF {
					t.Fatalf("ReadAt() error = %v, want io.EOF", err)
				}
			} else if tt.wantErr {
				if err == nil {
					t.Fatal("ReadAt() error = nil, want an error")
				}
			} else if err != nil {
				t.Fatalf("ReadAt() error = %v", err)
			}
			if got := string(dst[:n]); got != tt.want {
				t.Fatalf("ReadAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRopeReadAtDoesNotAllocate(t *testing.T) {
	rope := NewFromString(strings.Repeat("0123456789", 1_000))
	dst := make([]byte, 4<<10)
	if n, err := rope.ReadAt(dst, 333); n != len(dst) || err != nil {
		t.Fatalf("ReadAt() = (%d, %v), want (%d, nil)", n, err, len(dst))
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_, _ = rope.ReadAt(dst, 333)
	}); allocs != 0 {
		t.Fatalf("ReadAt() allocated %.0f times per call, want 0", allocs)
	}
}

func TestNilRopeReadAtReturnsEOF(t *testing.T) {
	var rope *Rope
	dst := make([]byte, 1)
	if n, err := rope.ReadAt(dst, 0); n != 0 || err != io.EOF {
		t.Fatalf("ReadAt() = (%d, %v), want (0, io.EOF)", n, err)
	}
}
