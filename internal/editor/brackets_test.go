package editor

import (
	"testing"

	"teak/internal/text"
)

func TestIsOpenBracket(t *testing.T) {
	tests := []struct {
		b    byte
		want bool
	}{
		{'(', true},
		{'[', true},
		{'{', true},
		{')', false},
		{']', false},
		{'}', false},
		{'a', false},
	}
	for _, tt := range tests {
		if got := IsOpenBracket(tt.b); got != tt.want {
			t.Errorf("IsOpenBracket(%q) = %v, want %v", tt.b, got, tt.want)
		}
	}
}

func TestIsCloseBracket(t *testing.T) {
	tests := []struct {
		b    byte
		want bool
	}{
		{')', true},
		{']', true},
		{'}', true},
		{'(', false},
		{'[', false},
		{'{', false},
		{'a', false},
	}
	for _, tt := range tests {
		if got := IsCloseBracket(tt.b); got != tt.want {
			t.Errorf("IsCloseBracket(%q) = %v, want %v", tt.b, got, tt.want)
		}
	}
}

func TestMatchingClose(t *testing.T) {
	tests := []struct {
		b    byte
		want byte
	}{
		{'(', ')'},
		{'[', ']'},
		{'{', '}'},
		{'a', 0},
	}
	for _, tt := range tests {
		if got := MatchingClose(tt.b); got != tt.want {
			t.Errorf("MatchingClose(%q) = %q, want %q", tt.b, got, tt.want)
		}
	}
}

func TestAutoClosePair(t *testing.T) {
	tests := []struct {
		ch   byte
		want byte
	}{
		{'(', ')'},
		{'{', '}'},
		{'[', ']'},
		{'a', 0},
		{')', 0},
	}
	for _, tt := range tests {
		if got := AutoClosePair(tt.ch); got != tt.want {
			t.Errorf("AutoClosePair(%q) = %q, want %q", tt.ch, got, tt.want)
		}
	}
}

func TestFindMatchingBracket(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pos     text.Position
		want    text.Position
		found   bool
	}{
		{
			name:    "simple parens forward",
			content: "(hello)",
			pos:     text.Position{Line: 0, Col: 0},
			want:    text.Position{Line: 0, Col: 6},
			found:   true,
		},
		{
			name:    "simple parens backward",
			content: "(hello)",
			pos:     text.Position{Line: 0, Col: 6},
			want:    text.Position{Line: 0, Col: 0},
			found:   true,
		},
		{
			name:    "nested brackets",
			content: "((inner))",
			pos:     text.Position{Line: 0, Col: 0},
			want:    text.Position{Line: 0, Col: 8},
			found:   true,
		},
		{
			name:    "inner nested brackets",
			content: "((inner))",
			pos:     text.Position{Line: 0, Col: 1},
			want:    text.Position{Line: 0, Col: 7},
			found:   true,
		},
		{
			name:    "multiline forward",
			content: "func() {\n  return\n}",
			pos:     text.Position{Line: 0, Col: 7},
			want:    text.Position{Line: 2, Col: 0},
			found:   true,
		},
		{
			name:    "multiline backward",
			content: "func() {\n  return\n}",
			pos:     text.Position{Line: 2, Col: 0},
			want:    text.Position{Line: 0, Col: 7},
			found:   true,
		},
		{
			name:    "unmatched open",
			content: "(hello",
			pos:     text.Position{Line: 0, Col: 0},
			want:    text.Position{},
			found:   false,
		},
		{
			name:    "unmatched close",
			content: "hello)",
			pos:     text.Position{Line: 0, Col: 5},
			want:    text.Position{},
			found:   false,
		},
		{
			name:    "not a bracket",
			content: "hello",
			pos:     text.Position{Line: 0, Col: 0},
			want:    text.Position{},
			found:   false,
		},
		{
			name:    "square brackets",
			content: "[1, [2, 3]]",
			pos:     text.Position{Line: 0, Col: 0},
			want:    text.Position{Line: 0, Col: 10},
			found:   true,
		},
		{
			name:    "curly braces",
			content: "{a: {b: c}}",
			pos:     text.Position{Line: 0, Col: 4},
			want:    text.Position{Line: 0, Col: 9},
			found:   true,
		},
		{
			name:    "out of bounds line",
			content: "hello",
			pos:     text.Position{Line: 5, Col: 0},
			want:    text.Position{},
			found:   false,
		},
		{
			name:    "out of bounds col",
			content: "hello",
			pos:     text.Position{Line: 0, Col: 10},
			want:    text.Position{},
			found:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := text.NewBufferFromBytes([]byte(tt.content))
			got, found := FindMatchingBracket(buf, tt.pos)
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if got != tt.want {
				t.Errorf("pos = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBetweenBrackets(t *testing.T) {
	tests := []struct {
		name    string
		content string
		cursor  text.Position
		want    bool
	}{
		{
			name:    "between parens",
			content: "()",
			cursor:  text.Position{Line: 0, Col: 1},
			want:    true,
		},
		{
			name:    "between square brackets",
			content: "[]",
			cursor:  text.Position{Line: 0, Col: 1},
			want:    true,
		},
		{
			name:    "between curly braces",
			content: "{}",
			cursor:  text.Position{Line: 0, Col: 1},
			want:    true,
		},
		{
			name:    "not between brackets",
			content: "ab",
			cursor:  text.Position{Line: 0, Col: 1},
			want:    false,
		},
		{
			name:    "mismatched brackets",
			content: "(]",
			cursor:  text.Position{Line: 0, Col: 1},
			want:    false,
		},
		{
			name:    "at start of line",
			content: "()",
			cursor:  text.Position{Line: 0, Col: 0},
			want:    false,
		},
		{
			name:    "at end of line",
			content: "()",
			cursor:  text.Position{Line: 0, Col: 2},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := text.NewBufferFromBytes([]byte(tt.content))
			if got := IsBetweenBrackets(buf, tt.cursor); got != tt.want {
				t.Errorf("IsBetweenBrackets() = %v, want %v", got, tt.want)
			}
		})
	}
}
