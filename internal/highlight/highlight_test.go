package highlight

import (
	"testing"

	"teak/internal/ui"
)

func TestNew(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)
	if h == nil {
		t.Fatal("expected non-nil Highlighter")
	}
	if !h.IsDirty() {
		t.Error("new highlighter should be dirty")
	}
}

func TestTokenize(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)

	src := []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	h.Tokenize(src)

	if h.IsDirty() {
		t.Error("should not be dirty after tokenize")
	}
	if h.LineCount() == 0 {
		t.Error("expected non-zero line count")
	}

	// First line should have tokens
	line0 := h.Line(0)
	if len(line0) == 0 {
		t.Error("expected tokens on line 0")
	}

	// Check that the tokens reconstruct the line text
	var text string
	for _, tok := range line0 {
		text += tok.Text
	}
	if text != "package main" {
		t.Errorf("expected 'package main', got %q", text)
	}
}

func TestTokenizeEmpty(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)
	h.Tokenize([]byte(""))

	if h.IsDirty() {
		t.Error("should not be dirty after tokenize")
	}
}

func TestInvalidate(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)
	h.Tokenize([]byte("x := 1"))
	if h.IsDirty() {
		t.Error("should not be dirty")
	}
	h.Invalidate()
	if !h.IsDirty() {
		t.Error("should be dirty after invalidate")
	}
}

func TestLineOutOfBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)
	h.Tokenize([]byte("line1\nline2"))

	if tokens := h.Line(-1); tokens != nil {
		t.Error("expected nil for negative line")
	}
	if tokens := h.Line(100); tokens != nil {
		t.Error("expected nil for out-of-bounds line")
	}
}

func TestUnknownLanguage(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("unknown.xyz123", theme)
	h.Tokenize([]byte("some random text"))

	if h.LineCount() == 0 {
		t.Error("expected at least one line")
	}
}

func TestMultipleLanguages(t *testing.T) {
	theme := ui.DefaultTheme()
	tests := []struct {
		filename string
		content  string
	}{
		{"test.py", "def hello():\n    print('hi')\n"},
		{"test.js", "function hello() {\n  console.log('hi');\n}\n"},
		{"test.rs", "fn main() {\n    println!(\"hello\");\n}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			h := New(tt.filename, theme)
			h.Tokenize([]byte(tt.content))
			if h.LineCount() == 0 {
				t.Errorf("expected tokens for %s", tt.filename)
			}
		})
	}
}
