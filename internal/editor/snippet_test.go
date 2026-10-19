package editor

import "testing"

func TestExpandSnippetLSPEscapesAndNesting(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantText  string
		wantCaret int
	}{
		{"escaped dollar is literal", `cost \$1`, "cost $1", -1},
		{"escaped dollar zero is literal", `\$0`, "$0", -1},
		{"escaped brace inside placeholder", `${1:a\}b}`, "a}b", -1},
		{"escaped backslash", `C:\\dir$0`, `C:\dir`, 6},
		{"plain text without tabstops keeps backslashes", `C:\\dir`, `C:\\dir`, -1},
		{"other backslash sequences are kept", `a\nb`, `a\nb`, -1},
		{"nested placeholder default", `${1:${2:x}}$0`, "x", 1},
		{"unterminated placeholder stays literal", "${1:foo", "${1:foo", -1},
		{"bare tabstop without default", "a${1}b$0", "ab", 2},
		{"unmatched inner brace in text", "f(}$0", "f(}", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, caret := expandSnippet(tt.in)
			if got != tt.wantText || caret != tt.wantCaret {
				t.Fatalf("expandSnippet(%q) = (%q, %d), want (%q, %d)", tt.in, got, caret, tt.wantText, tt.wantCaret)
			}
		})
	}
}
