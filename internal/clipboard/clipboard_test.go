package clipboard

import (
	"strings"
	"testing"
)

func TestCopy_InternalFallback(t *testing.T) {
	// Copy should always succeed (internal fallback)
	err := Copy("hello world")
	// On CI or systems without pbcopy, err may be non-nil but internal should be set
	_ = err

	if internal != "hello world" {
		t.Errorf("internal = %q, want %q", internal, "hello world")
	}
}

func TestPaste_InternalFallback(t *testing.T) {
	// Set internal buffer directly
	internal = "test content"

	// Paste should return the internal buffer if OS clipboard fails
	content, _ := Paste()
	if content == "" {
		t.Error("Paste() returned empty string, expected at least internal fallback")
	}
}

func TestCopy_Paste_Roundtrip(t *testing.T) {
	text := "round trip test 🎉"
	_ = Copy(text)

	content, _ := Paste()
	if content == "" {
		t.Error("Paste() returned empty after Copy()")
	}
	// On systems with clipboard support, content should match
	// On systems without, internal fallback should still work
	if content != text && internal != text {
		t.Errorf("roundtrip failed: got %q, internal=%q, want %q", content, internal, text)
	}
}

func TestCopy_ReturnsError(t *testing.T) {
	// Copy returns error type (not nil type)
	err := Copy("test")
	// We can't predict whether it errors (depends on clipboard availability)
	// but the function should not panic
	_ = err
}

func TestPaste_ReturnsTwoValues(t *testing.T) {
	// Verify Paste returns (string, error) - compile-time check
	var s string
	var err error
	s, err = Paste()
	_, _ = s, err
}

// --- New tests below ---

func TestCopy_EmptyString(t *testing.T) {
	_ = Copy("")
	if internal != "" {
		t.Errorf("internal = %q after Copy(\"\"), want empty string", internal)
	}
}

func TestCopy_Paste_EmptyString(t *testing.T) {
	_ = Copy("")
	content, _ := Paste()
	// Either OS clipboard returns empty or internal fallback does
	if content != "" && internal != "" {
		t.Errorf("expected empty string roundtrip, got content=%q, internal=%q", content, internal)
	}
}

func TestCopy_Unicode(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"emoji", "Hello 🌍🚀✨"},
		{"chinese", "你好世界"},
		{"arabic", "مرحبا بالعالم"},
		{"japanese", "こんにちは世界"},
		{"mixed scripts", "Hello мир 世界 🌍"},
		{"combining characters", "é = e\u0301"},
		{"zero width joiner", "👨‍👩‍👧‍👦"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = Copy(tt.text)
			if internal != tt.text {
				t.Errorf("internal = %q, want %q", internal, tt.text)
			}
		})
	}
}

func TestCopy_Multiline(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"unix newlines", "line1\nline2\nline3"},
		{"windows newlines", "line1\r\nline2\r\nline3"},
		{"mixed newlines", "line1\nline2\r\nline3"},
		{"trailing newline", "content\n"},
		{"leading newline", "\ncontent"},
		{"blank lines", "line1\n\n\nline4"},
		{"tabs and spaces", "  indented\n\ttabbed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = Copy(tt.text)
			if internal != tt.text {
				t.Errorf("internal = %q, want %q", internal, tt.text)
			}
		})
	}
}

func TestCopy_LongString(t *testing.T) {
	// 100KB string
	long := strings.Repeat("abcdefghij", 10000)
	_ = Copy(long)
	if internal != long {
		t.Errorf("internal length = %d, want %d", len(internal), len(long))
	}
}

func TestCopy_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"null bytes", "before\x00after"},
		{"control chars", "bell\x07tab\tesc\x1b"},
		{"backslashes", `path\to\file`},
		{"quotes", `she said "hello" and 'goodbye'`},
		{"angle brackets", "<html>&amp;</html>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = Copy(tt.text)
			if internal != tt.text {
				t.Errorf("internal = %q, want %q", internal, tt.text)
			}
		})
	}
}

func TestCopy_OverwritesPrevious(t *testing.T) {
	_ = Copy("first")
	if internal != "first" {
		t.Fatalf("internal = %q, want 'first'", internal)
	}

	_ = Copy("second")
	if internal != "second" {
		t.Errorf("internal = %q after overwrite, want 'second'", internal)
	}
}

func TestPaste_ReturnsInternalWhenSet(t *testing.T) {
	internal = "fallback value"
	content, _ := Paste()
	// On any system, we should get something back
	if content == "" {
		t.Error("Paste() returned empty string when internal fallback is set")
	}
}

func TestCopy_Paste_Roundtrip_Multiline(t *testing.T) {
	text := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	_ = Copy(text)

	content, _ := Paste()
	if content != text && internal != text {
		t.Errorf("multiline roundtrip failed: got %q, internal=%q, want %q", content, internal, text)
	}
}

func TestCopy_VeryLongString(t *testing.T) {
	// 1MB string
	veryLong := strings.Repeat("x", 1024*1024)
	_ = Copy(veryLong)
	if internal != veryLong {
		t.Errorf("internal length = %d, want %d", len(internal), len(veryLong))
	}
}

func TestCopy_Paste_WhitespaceOnly(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"single space", " "},
		{"multiple spaces", "     "},
		{"single tab", "\t"},
		{"multiple tabs", "\t\t\t"},
		{"newline", "\n"},
		{"multiple newlines", "\n\n\n"},
		{"mixed whitespace", " \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = Copy(tt.text)
			if internal != tt.text {
				t.Errorf("internal = %q, want %q", internal, tt.text)
			}
		})
	}
}

func TestCopy_Paste_RepeatedCalls(t *testing.T) {
	values := []string{"first", "second", "third", "fourth"}
	
	for i, expected := range values {
		_ = Copy(expected)
		if internal != expected {
			t.Errorf("iteration %d: internal = %q, want %q", i, internal, expected)
		}
	}
}

func TestPaste_WithoutPriorCopy(t *testing.T) {
	// Reset internal
	internal = ""
	
	// Paste should still return something (OS clipboard or empty internal)
	content, err := Paste()
	// We don't care about the error, just that it doesn't panic
	_ = err
	_ = content
}

func TestCopy_DoesNotPanicWithNil(t *testing.T) {
	// This test ensures Copy handles edge cases gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Copy panicked with %v", r)
		}
	}()
	
	// Empty string should not panic
	_ = Copy("")
}

func TestPaste_DoesNotPanic(t *testing.T) {
	// This test ensures Paste handles edge cases gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Paste panicked with %v", r)
		}
	}()
	
	// Paste should not panic even if internal is empty
	internal = ""
	_, _ = Paste()
}

func TestCopy_Paste_PreservesExactBytes(t *testing.T) {
	// Test that bytes are preserved exactly through the internal fallback
	original := "test with special chars: \x00\x01\x02"
	_ = Copy(original)
	
	if internal != original {
		t.Errorf("bytes not preserved: got %q, want %q", internal, original)
	}
}

func TestCopy_Paste_EscapeSequences(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"backslash n", `test\nnot a newline`},
		{"backslash t", `test\tnot a tab`},
		{"backslash r", `test\rnot a cr`},
		{"backslash backslash", `test\\backslash`},
		{"backslash quote", `test\"quote`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = Copy(tt.text)
			if internal != tt.text {
				t.Errorf("internal = %q, want %q", internal, tt.text)
			}
		})
	}
}

func TestCopy_Paste_JSONLikeContent(t *testing.T) {
	json := `{"key": "value", "number": 42, "array": [1,2,3]}`
	_ = Copy(json)
	
	content, _ := Paste()
	if content != json && internal != json {
		t.Errorf("JSON roundtrip failed: got %q, internal=%q, want %q", content, internal, json)
	}
}

func TestCopy_Paste_XMLLikeContent(t *testing.T) {
	xml := `<root><element attr="value">content</element></root>`
	_ = Copy(xml)
	
	content, _ := Paste()
	if content != xml && internal != xml {
		t.Errorf("XML roundtrip failed: got %q, internal=%q, want %q", content, internal, xml)
	}
}

func TestCopy_Paste_CodeSnippet(t *testing.T) {
	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, 世界")
}`
	_ = Copy(code)
	
	content, _ := Paste()
	if content != code && internal != code {
		t.Errorf("code roundtrip failed:\ngot %q\ninternal=%q\nwant %q", content, internal, code)
	}
}

func TestInternalVariableIsPackageLevel(t *testing.T) {
	// Verify internal is package-level by checking it persists across calls
	_ = Copy("value1")
	val1 := internal
	
	_ = Copy("value2")
	val2 := internal
	
	if val1 == val2 {
		t.Error("internal variable should change between Copy calls")
	}
	if val1 != "value1" || val2 != "value2" {
		t.Errorf("internal values incorrect: got %q, %q, want value1, value2", val1, val2)
	}
}
