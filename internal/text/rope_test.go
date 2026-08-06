package text

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewCopiesCallerBytes(t *testing.T) {
	data := []byte("caller-owned bytes")
	rope := New(data)

	if len(rope.value) == 0 {
		t.Fatal("New() produced an empty leaf")
	}
	if &rope.value[0] == &data[0] {
		t.Fatal("New() retained caller-owned backing storage")
	}
	data[0] = 'C'
	if got, want := rope.String(), "caller-owned bytes"; got != want {
		t.Fatalf("New() reflected a caller mutation: got %q, want %q", got, want)
	}
}

func TestNewOwnedTransfersExclusiveBackingStorageWithoutSplittingUTF8(t *testing.T) {
	// Make several leaves and put a multi-byte rune on both sides of a leaf
	// boundary. NewOwned must retain this exact allocation without allowing a
	// leaf to start with a UTF-8 continuation byte.
	data := bytes.Repeat([]byte("ab😀"), 300)
	rope := NewOwned(data)

	if got, want := rope.String(), string(data); got != want {
		t.Fatalf("NewOwned() content = %q, want %q", got, want)
	}
	var leaves []*Rope
	var collect func(*Rope)
	collect = func(node *Rope) {
		if node.isLeaf() {
			if len(node.value) != 0 {
				leaves = append(leaves, node)
			}
			return
		}
		collect(node.left)
		collect(node.right)
	}
	collect(rope)
	if len(leaves) < 2 {
		t.Fatalf("NewOwned() made %d leaves, want multiple leaves", len(leaves))
	}
	if &leaves[0].value[0] != &data[0] {
		t.Fatal("NewOwned() copied the exclusively transferred allocation")
	}
	for index, leaf := range leaves {
		if !utf8.Valid(leaf.value) {
			t.Fatalf("leaf %d split a UTF-8 sequence: %q", index, leaf.value)
		}
	}
}

func TestNewAndString(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"short", "hello"},
		{"with newlines", "hello\nworld\n"},
		{"multi-byte utf8", "héllo wörld 日本語"},
		{"large", strings.Repeat("abcdefghij\n", 100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFromString(tt.input)
			if got := r.String(); got != tt.input {
				t.Errorf("String() = %q, want %q", got, tt.input)
			}
			if got := r.Len(); got != len(tt.input) {
				t.Errorf("Len() = %d, want %d", got, len(tt.input))
			}
		})
	}
}

func TestLineCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 1},
		{"hello", 1},
		{"hello\n", 2},
		{"hello\nworld", 2},
		{"a\nb\nc\n", 4},
	}
	for _, tt := range tests {
		r := NewFromString(tt.input)
		if got := r.LineCount(); got != tt.want {
			t.Errorf("LineCount(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestInsert(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		offset int
		insert string
		want   string
	}{
		{"at beginning", "hello", 0, "X", "Xhello"},
		{"at end", "hello", 5, "X", "helloX"},
		{"in middle", "hello", 2, "X", "heXllo"},
		{"into empty", "", 0, "hello", "hello"},
		{"multi-byte insert", "hello", 5, " 日本", "hello 日本"},
		{"newline insert", "helloworld", 5, "\n", "hello\nworld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFromString(tt.base)
			r2 := r.Insert(tt.offset, []byte(tt.insert))
			if got := r2.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		offset int
		n      int
		want   string
	}{
		{"from beginning", "hello", 0, 1, "ello"},
		{"from end", "hello", 4, 1, "hell"},
		{"from middle", "hello", 2, 1, "helo"},
		{"all", "hello", 0, 5, ""},
		{"delete newline", "hello\nworld", 5, 1, "helloworld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFromString(tt.base)
			r2 := r.Delete(tt.offset, tt.n)
			if got := r2.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestImmutability(t *testing.T) {
	r1 := NewFromString("hello world")
	r2 := r1.Insert(5, []byte("X"))
	r3 := r1.Delete(0, 5)

	if r1.String() != "hello world" {
		t.Error("original rope was mutated after Insert")
	}
	if r2.String() != "helloX world" {
		t.Errorf("Insert result wrong: %q", r2.String())
	}
	if r3.String() != " world" {
		t.Errorf("Delete result wrong: %q", r3.String())
	}
}

func TestLineOperations(t *testing.T) {
	text := "first line\nsecond line\nthird line"
	r := NewFromString(text)

	tests := []struct {
		line    int
		start   int
		content string
		lineLen int
	}{
		{0, 0, "first line", 10},
		{1, 11, "second line", 11},
		{2, 23, "third line", 10},
	}
	for _, tt := range tests {
		if got := r.LineStart(tt.line); got != tt.start {
			t.Errorf("LineStart(%d) = %d, want %d", tt.line, got, tt.start)
		}
		if got := string(r.Line(tt.line)); got != tt.content {
			t.Errorf("Line(%d) = %q, want %q", tt.line, got, tt.content)
		}
		if got := r.LineLen(tt.line); got != tt.lineLen {
			t.Errorf("LineLen(%d) = %d, want %d", tt.line, got, tt.lineLen)
		}
	}
}

func TestPositionToOffset(t *testing.T) {
	text := "abc\ndef\nghi"
	r := NewFromString(text)

	tests := []struct {
		pos    Position
		offset int
	}{
		{Position{0, 0}, 0},
		{Position{0, 3}, 3},
		{Position{1, 0}, 4},
		{Position{1, 2}, 6},
		{Position{2, 0}, 8},
		{Position{2, 3}, 11},
	}
	for _, tt := range tests {
		if got := r.PositionToOffset(tt.pos); got != tt.offset {
			t.Errorf("PositionToOffset(%v) = %d, want %d", tt.pos, got, tt.offset)
		}
	}
}

func TestPositionToOffsetUncachedAllowsLineEnd(t *testing.T) {
	r := NewFromString("abc\ndef\nghi")
	tests := []struct {
		pos  Position
		want int
	}{
		{Position{0, 3}, 3},
		{Position{1, 3}, 7},
		{Position{2, 3}, 11},
	}
	for _, tt := range tests {
		got, ok := r.PositionToOffsetUncached(tt.pos)
		if !ok || got != tt.want {
			t.Errorf("PositionToOffsetUncached(%v) = (%d, %v), want (%d, true)", tt.pos, got, ok, tt.want)
		}
	}
}

func TestPositionToOffsetUncachedDoesNotScanWholeDocument(t *testing.T) {
	r := NewFromString(strings.Repeat("x", 2<<20))
	if got, ok := r.PositionToOffsetUncached(Position{Line: 0, Col: r.Len()}); !ok || got != r.Len() {
		t.Fatalf("PositionToOffsetUncached(line end) = (%d, %v)", got, ok)
	}
	// Converting a position must stay proportional to tree depth, not to
	// document size; a whole-document scan or offset table would allocate.
	if allocs := testing.AllocsPerRun(50, func() {
		r.PositionToOffsetUncached(Position{Line: 0, Col: r.Len()})
	}); allocs != 0 {
		t.Errorf("PositionToOffsetUncached allocated %v times per call, want 0", allocs)
	}
}

func TestValidUTF8RangeStreamsAcrossLeaves(t *testing.T) {
	splitRune := join(
		newLeafOwned([]byte{0xf0, 0x9f}),
		newLeafOwned([]byte{0x99, 0x82}),
	)
	invalid := join(
		newLeafOwned([]byte{0xc0}),
		newLeafOwned([]byte{0xaf}),
	)
	tests := []struct {
		name       string
		rope       *Rope
		start, end int
		want       bool
	}{
		{name: "valid rune split across leaves", rope: splitRune, start: 0, end: 4, want: true},
		{name: "incomplete prefix", rope: splitRune, start: 0, end: 2, want: false},
		{name: "continuation suffix", rope: splitRune, start: 2, end: 4, want: false},
		{name: "invalid overlong sequence", rope: invalid, start: 0, end: 2, want: false},
		{name: "empty range", rope: splitRune, start: 2, end: 2, want: true},
		{name: "invalid negative bound", rope: splitRune, start: -1, end: 2, want: false},
		{name: "invalid reversed bound", rope: splitRune, start: 3, end: 2, want: false},
		{name: "invalid high bound", rope: splitRune, start: 0, end: 5, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rope.ValidUTF8Range(tt.start, tt.end); got != tt.want {
				t.Fatalf("ValidUTF8Range(%d, %d) = %v, want %v", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestValidUTF8RangeMatchesStandardLibrary(t *testing.T) {
	parts := [][]byte{
		{'a', 0xf0},
		{0x9f},
		{0x99, 0x82, 'b', 0xc0},
		{0xaf, 0xe2},
		{0x82, 0xac, 'c'},
	}
	data := make([]byte, 0)
	var rope *Rope
	for _, part := range parts {
		data = append(data, part...)
		leaf := newLeafOwned(part)
		if rope == nil {
			rope = leaf
		} else {
			rope = join(rope, leaf)
		}
	}
	for start := 0; start <= len(data); start++ {
		for end := start; end <= len(data); end++ {
			want := utf8.Valid(data[start:end])
			if got := rope.ValidUTF8Range(start, end); got != want {
				t.Fatalf("ValidUTF8Range(%d, %d) = %v, want %v for %x", start, end, got, want, data[start:end])
			}
		}
	}
}

func TestValidUTF8RangeDoesNotAllocate(t *testing.T) {
	rope := NewFromString(strings.Repeat("ab🙂cd", 1_000))
	if !rope.ValidUTF8Range(0, rope.Len()) {
		t.Fatal("ValidUTF8Range rejected valid content")
	}
	if allocs := testing.AllocsPerRun(50, func() {
		rope.ValidUTF8Range(0, rope.Len())
	}); allocs != 0 {
		t.Fatalf("ValidUTF8Range allocated %.0f times per call, want 0", allocs)
	}
}

func TestOffsetToPosition(t *testing.T) {
	text := "abc\ndef\nghi"
	r := NewFromString(text)

	tests := []struct {
		offset int
		pos    Position
	}{
		{0, Position{0, 0}},
		{3, Position{0, 3}},
		{4, Position{1, 0}},
		{6, Position{1, 2}},
		{8, Position{2, 0}},
		{11, Position{2, 3}},
	}
	for _, tt := range tests {
		if got := r.OffsetToPosition(tt.offset); got != tt.pos {
			t.Errorf("OffsetToPosition(%d) = %v, want %v", tt.offset, got, tt.pos)
		}
	}
}

func TestMultiByteUTF8(t *testing.T) {
	text := "héllo"
	r := NewFromString(text)
	if r.Len() != len(text) {
		t.Errorf("Len() = %d, want %d", r.Len(), len(text))
	}
	r2 := r.Insert(len("hé"), []byte("X"))
	want := "héXllo"
	if r2.String() != want {
		t.Errorf("got %q, want %q", r2.String(), want)
	}
}

func TestLargeDocument(t *testing.T) {
	// 1MB document
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 13107) // ~1MB
	r := New([]byte(doc))

	if r.String() != doc {
		t.Error("large document roundtrip failed")
	}

	// Insert in middle
	mid := r.Len() / 2
	r2 := r.Insert(mid, []byte("INSERTED"))
	if r2.Len() != r.Len()+8 {
		t.Errorf("length after insert: %d, want %d", r2.Len(), r.Len()+8)
	}

	// Delete from middle
	r3 := r2.Delete(mid, 8)
	if r3.String() != doc {
		t.Error("delete did not restore original")
	}
}

func TestSlice(t *testing.T) {
	r := NewFromString("hello world")
	s := r.Slice(0, 5)
	if s.String() != "hello" {
		t.Errorf("Slice(0,5) = %q, want %q", s.String(), "hello")
	}
	s2 := r.Slice(6, 11)
	if s2.String() != "world" {
		t.Errorf("Slice(6,11) = %q, want %q", s2.String(), "world")
	}
}

func BenchmarkInsert(b *testing.B) {
	doc := strings.Repeat("abcdefghij\n", 10000)
	r := New([]byte(doc))
	mid := r.Len() / 2
	data := []byte("X")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Insert(mid, data)
	}
}

func BenchmarkDelete(b *testing.B) {
	doc := strings.Repeat("abcdefghij\n", 10000)
	r := New([]byte(doc))
	mid := r.Len() / 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Delete(mid, 1)
	}
}

func BenchmarkLineStart(b *testing.B) {
	doc := strings.Repeat("abcdefghij\n", 10000)
	r := New([]byte(doc))
	_ = r.LineStart(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.LineStart(5000)
	}
}

func TestByteAtBounds(t *testing.T) {
	r := NewFromString("hello")

	tests := []struct {
		name     string
		offset   int
		wantByte byte
		wantOK   bool
	}{
		{"first byte", 0, 'h', true},
		{"last byte", 4, 'o', true},
		{"negative offset", -1, 0, false},
		{"offset at length", 5, 0, false},
		{"offset beyond length", 10, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.ByteAtSafe(tt.offset)
			if ok != tt.wantOK {
				t.Errorf("ByteAtSafe(%d) ok=%v, want %v", tt.offset, ok, tt.wantOK)
			}
			if ok && got != tt.wantByte {
				t.Errorf("ByteAtSafe(%d) = %q, want %q", tt.offset, got, tt.wantByte)
			}
		})
	}
}

func TestByteAtNilRope(t *testing.T) {
	var r *Rope
	got, ok := r.ByteAtSafe(0)
	if ok {
		t.Errorf("ByteAtSafe on nil rope should return !ok, got byte %q", got)
	}
}

func TestRopeRuneAccessAvoidsLineMaterialization(t *testing.T) {
	rope := NewFromString(strings.Repeat("x", maxLeaf-1) + "é😀z")
	start := maxLeaf - 1
	if got, size, ok := rope.RuneAt(start); !ok || got != 'é' || size != len("é") {
		t.Fatalf("RuneAt(%d) = %q, %d, %v; want é, 2, true", start, got, size, ok)
	}
	if got, size, ok := rope.RuneBefore(start + len("é")); !ok || got != 'é' || size != len("é") {
		t.Fatalf("RuneBefore(%d) = %q, %d, %v; want é, 2, true", start+len("é"), got, size, ok)
	}
	if got, size, ok := rope.RuneAt(start + len("é")); !ok || got != '😀' || size != len("😀") {
		t.Fatalf("RuneAt after boundary = %q, %d, %v; want 😀, 4, true", got, size, ok)
	}
}

func TestByteAtLargeRope(t *testing.T) {
	// Test with a multi-node rope
	large := strings.Repeat("abcdefghij", 100)
	r := NewFromString(large)

	// Test at various positions
	positions := []int{0, 50, 99, 500, 999}
	for _, pos := range positions {
		got, ok := r.ByteAtSafe(pos)
		if !ok {
			t.Errorf("ByteAtSafe(%d) failed unexpectedly", pos)
			continue
		}
		want := large[pos]
		if got != want {
			t.Errorf("ByteAtSafe(%d) = %q, want %q", pos, got, want)
		}
	}
}
