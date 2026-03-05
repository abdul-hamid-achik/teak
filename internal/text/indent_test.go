package text

import (
	"bytes"
	"testing"
)

func TestLeadingWhitespace(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		want []byte
	}{
		{"no whitespace", []byte("hello"), []byte{}},
		{"spaces", []byte("    hello"), []byte("    ")},
		{"tabs", []byte("\t\thello"), []byte("\t\t")},
		{"mixed", []byte("  \thello"), []byte("  \t")},
		{"all whitespace", []byte("    "), []byte("    ")},
		{"empty", []byte(""), []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LeadingWhitespace(tt.line)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("LeadingWhitespace(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestIndentString(t *testing.T) {
	tests := []struct {
		tabSize int
		want    []byte
	}{
		{4, []byte("    ")},
		{2, []byte("  ")},
		{0, []byte{}},
		{8, []byte("        ")},
	}
	for _, tt := range tests {
		got := IndentString(tt.tabSize)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("IndentString(%d) = %q, want %q", tt.tabSize, got, tt.want)
		}
	}
}

func TestDedent(t *testing.T) {
	tests := []struct {
		name    string
		line    []byte
		tabSize int
		want    int
	}{
		{"full indent", []byte("    hello"), 4, 4},
		{"partial indent", []byte("  hello"), 4, 2},
		{"no indent", []byte("hello"), 4, 0},
		{"tab char", []byte("\thello"), 4, 0},
		{"more than tabSize", []byte("      hello"), 4, 4},
		{"empty", []byte(""), 4, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Dedent(tt.line, tt.tabSize)
			if got != tt.want {
				t.Errorf("Dedent(%q, %d) = %d, want %d", tt.line, tt.tabSize, got, tt.want)
			}
		})
	}
}
