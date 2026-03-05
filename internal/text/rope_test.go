package text

import (
	"strings"
	"testing"
)

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
