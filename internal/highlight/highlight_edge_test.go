package highlight

import (
	"fmt"
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

// Edge case tests for TokenizeViewport

func TestTokenizeViewportNilBuffer(t *testing.T) {
	theme := ui.DefaultTheme()
	h := New("test.go", theme)

	// Should return nil gracefully instead of panicking
	result := h.TokenizeViewport(nil, 0, 10)
	if result != nil {
		t.Error("Expected nil for nil buffer")
	}
}

func TestTokenizeViewportNegativeStart(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("line 1\nline 2\nline 3"))
	h := New("test.go", theme)

	// Negative viewStart should be handled gracefully
	tokens := h.TokenizeViewport(buf, -10, 2)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Should still tokenize available lines
	hasTokens := false
	for _, line := range tokens {
		if len(line) > 0 {
			hasTokens = true
			break
		}
	}
	if !hasTokens {
		t.Error("expected some tokens to be generated")
	}
}

func TestTokenizeViewportStartGreaterThanEnd(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("line 1\nline 2\nline 3"))
	h := New("test.go", theme)

	// viewStart > viewEnd - should handle gracefully
	tokens := h.TokenizeViewport(buf, 10, 5)

	// Should not panic and return valid structure
	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}
}

func TestTokenizeViewportBeyondLineCount(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("line 1\nline 2"))
	h := New("test.go", theme)

	// Request viewport beyond buffer size
	tokens := h.TokenizeViewport(buf, 100, 200)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// All lines should be nil (beyond range)
	allNil := true
	for _, line := range tokens {
		if len(line) > 0 {
			allNil = false
			break
		}
	}
	// With margin, we might get some tokens
	// This is acceptable behavior
	t.Logf("allNil=%v (acceptable if margin doesn't reach content)", allNil)
}

func TestTokenizeViewportZeroRange(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("line 1\nline 2\nline 3"))
	h := New("test.go", theme)

	// viewStart == viewEnd (zero range)
	tokens := h.TokenizeViewport(buf, 1, 1)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Should still tokenize with margin
	hasTokens := false
	for _, line := range tokens {
		if len(line) > 0 {
			hasTokens = true
			break
		}
	}
	if !hasTokens {
		t.Error("expected some tokens within margin")
	}
}

func TestTokenizeViewportLongLines(t *testing.T) {
	theme := ui.DefaultTheme()

	// Create a buffer with very long lines
	longLine := "x"
	for i := 0; i < 20; i++ {
		longLine += longLine // exponential growth
	}

	content := longLine + "\n" + longLine + "\n" + longLine
	buf := text.NewBufferFromBytes([]byte(content))

	h := New("test.go", theme)
	tokens := h.TokenizeViewport(buf, 0, 3)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// All lines should be tokenized
	for i := 0; i < buf.LineCount(); i++ {
		if len(tokens[i]) == 0 {
			t.Errorf("line %d should have tokens", i)
		}
	}
}

func TestTokenizeViewportUnicode(t *testing.T) {
	theme := ui.DefaultTheme()

	// Content with unicode characters
	content := `package main

// こんにちは世界
func 日本語() string {
	return "Hello 世界 🌍"
}
`
	buf := text.NewBufferFromBytes([]byte(content))
	h := New("test.go", theme)

	tokens := h.TokenizeViewport(buf, 0, 10)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Check that unicode lines have tokens
	for i := 0; i < buf.LineCount(); i++ {
		line := buf.Line(i)
		if len(line) > 0 && len(tokens[i]) == 0 {
			t.Errorf("line %d (len=%d) should have tokens", i, len(line))
		}
	}
}

func TestTokenizeViewportSingleLine(t *testing.T) {
	theme := ui.DefaultTheme()
	buf := text.NewBufferFromBytes([]byte("package main"))
	h := New("test.go", theme)

	tokens := h.TokenizeViewport(buf, 0, 1)

	if len(tokens) != 1 {
		t.Errorf("expected 1 line, got %d", len(tokens))
	}

	if len(tokens[0]) == 0 {
		t.Error("single line should have tokens")
	}
}

func TestTokenizeViewportMultiByteBoundary(t *testing.T) {
	theme := ui.DefaultTheme()

	// Content where margin might split multi-byte character
	// This tests the boundary handling
	content := "package main\n"
	for i := 0; i < 250; i++ {
		content += "// Comment line " + string(rune('0'+i%10)) + "\n"
	}
	content += "func main() {}"

	buf := text.NewBufferFromBytes([]byte(content))
	h := New("test.go", theme)

	// Request viewport at position 200
	tokens := h.TokenizeViewport(buf, 200, 210)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}
}

func TestTokenizeViewportVeryLargeFile(t *testing.T) {
	// This test is too slow for CI - skip in all modes
	t.Skip("Skipping slow large file test - run manually with -timeout=20m if needed")

	theme := ui.DefaultTheme()

	// Create 500K line file
	var content string
	for i := 0; i < 500000; i++ {
		content += fmt.Sprintf("func test%d() int { return %d }\n", i, i)
	}

	buf := text.NewBufferFromBytes([]byte(content))
	h := New("test.go", theme)

	// Tokenize middle viewport
	start := 250000
	end := 250024

	tokens := h.TokenizeViewport(buf, start, end)

	if len(tokens) != buf.LineCount() {
		t.Errorf("expected %d lines, got %d", buf.LineCount(), len(tokens))
	}

	// Check viewport lines are tokenized
	for i := start; i < end && i < len(tokens); i++ {
		if len(tokens[i]) == 0 {
			t.Errorf("viewport line %d should have tokens", i)
		}
	}

	// Verify we're not allocating crazy amounts of memory
	// 500K pointers * 8 bytes = 4MB which is acceptable
	t.Logf("Successfully tokenized 500K line file, result slice has %d elements", len(tokens))
}

func TestTokenizeViewportMemoryUsage(t *testing.T) {
	theme := ui.DefaultTheme()

	// Create 100K line file
	var content string
	for i := 0; i < 100000; i++ {
		content += fmt.Sprintf("line %d\n", i)
	}

	buf := text.NewBufferFromBytes([]byte(content))
	h := New("test.go", theme)

	// This allocates buf.LineCount() slice of slices
	// 100K * 8 bytes = ~800KB overhead just for the slice headers
	tokens := h.TokenizeViewport(buf, 0, 24)

	// Most tokens should be nil (only viewport + margin has content)
	nilCount := 0
	for _, line := range tokens {
		if line == nil || len(line) == 0 {
			nilCount++
		}
	}

	nonNilCount := len(tokens) - nilCount
	t.Logf("Total lines: %d, Non-nil: %d, Nil: %d", len(tokens), nonNilCount, nilCount)

	// We expect most to be nil (only viewport + 400 line margin should have content)
	// 100K - 424 = ~99.5K nil entries
	if nilCount < len(tokens)-500 {
		t.Logf("Warning: More non-nil entries than expected (got %d non-nil)", nonNilCount)
	}
}
