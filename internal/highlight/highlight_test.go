package highlight

import (
	"fmt"
	"testing"

	"teak/internal/text"
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

func TestTokenizeViewport(t *testing.T) {
	theme := ui.DefaultTheme()

	// Create a buffer with 100 lines
	var content string
	for i := 0; i < 100; i++ {
		content += fmt.Sprintf("func test%d() { return %d }\n", i, i)
	}
	buf := text.NewBufferFromBytes([]byte(content))

	h := New("test.go", theme)

	// Tokenize viewport lines 40-60
	viewStart := 40
	viewEnd := 60
	tokens := h.TokenizeViewport(buf, viewStart, viewEnd)

	// Result should have same length as buffer line count
	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Viewport lines should be tokenized
	for i := viewStart; i < viewEnd && i < len(tokens); i++ {
		if len(tokens[i]) == 0 {
			t.Errorf("viewport line %d should have tokens", i)
		}
	}

	// Lines outside viewport (+/- margin) may be nil or empty
	// But at least the basic structure should be there
	if tokens[0] != nil {
		t.Log("Line 0 has tokens (within margin)")
	}
}

func TestTokenizeViewportSmallBuffer(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("package main\n\nfunc main() {}"))

	h := New("test.go", theme)

	// Viewport larger than buffer
	tokens := h.TokenizeViewport(buf, 0, 100)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Lines with content should be tokenized (empty lines may have nil tokens)
	for i := 0; i < buf.LineCount(); i++ {
		line := buf.Line(i)
		if len(line) > 0 && len(tokens[i]) == 0 {
			t.Errorf("line %d (non-empty) should have tokens", i)
		}
	}
}

func TestTokenizeViewportEmptyBuffer(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte(""))

	h := New("test.go", theme)
	tokens := h.TokenizeViewport(buf, 0, 10)

	// Empty buffer has 1 line
	if len(tokens) != 1 {
		t.Errorf("expected 1 line for empty buffer, got %d", len(tokens))
	}
}
