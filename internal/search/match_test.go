package search

import (
	"strings"
	"testing"
	"unicode/utf8"

	"teak/internal/ui"
)

func TestCompilePatternLiteral(t *testing.T) {
	re, err := CompilePattern("foo.bar", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("foo.bar") {
		t.Error("literal pattern should match exact text")
	}
	if re.MatchString("fooXbar") {
		t.Error("literal pattern should not match regex metacharacters")
	}
}

func TestCompilePatternRegex(t *testing.T) {
	re, err := CompilePattern("foo.bar", SearchOpts{Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("fooXbar") {
		t.Error("regex pattern should match with metacharacters")
	}
	if !re.MatchString("foo.bar") {
		t.Error("regex pattern should also match literal dot")
	}
}

func TestCompilePatternCaseInsensitive(t *testing.T) {
	re, err := CompilePattern("hello", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("HELLO") {
		t.Error("default should be case-insensitive")
	}
}

func TestCompilePatternCaseSensitive(t *testing.T) {
	re, err := CompilePattern("hello", SearchOpts{CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchString("HELLO") {
		t.Error("case-sensitive should not match different case")
	}
	if !re.MatchString("hello") {
		t.Error("case-sensitive should match exact case")
	}
}

func TestCompilePatternWholeWord(t *testing.T) {
	re, err := CompilePattern("cat", SearchOpts{WholeWord: true})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("a cat sat") {
		t.Error("whole-word should match a standalone word")
	}
	if re.MatchString("category") {
		t.Error("whole-word should not match a longer token")
	}
}

func TestCompilePatternInvalidRegex(t *testing.T) {
	_, err := CompilePattern("[invalid", SearchOpts{Regex: true})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestCompilePatternRegexGroups(t *testing.T) {
	re, err := CompilePattern(`(\w+)@(\w+)`, SearchOpts{Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	match := re.FindStringSubmatch("user@host")
	if len(match) != 3 || match[1] != "user" || match[2] != "host" {
		t.Errorf("expected groups [user host], got %v", match)
	}
}

// TestSearchOverlayViewHasNoRawEscapeFromPreview renders the overlay with a
// preview that contains a clear-screen sequence and asserts the sequence never
// reaches the rendered output.
func TestSearchOverlayViewHasNoRawEscapeFromPreview(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.SetSize(80, 18)
	m.results = []Result{{FilePath: "a.txt", Line: 0, Col: 0, Preview: previewAround("before\x1b[2Jafter", 0)}}
	if view := m.View(); strings.Contains(view, "\x1b[2J") {
		t.Errorf("overlay view contains a raw clear-screen escape from file content")
	}
}

func TestPreviewAroundKeepsMatchAndRuneBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		start    int
		wantFrag string
	}{
		{"short line trimmed", "  hello  \r\n", 2, "hello"},
		{"match in middle", strings.Repeat("x", 5000) + "TARGET" + strings.Repeat("y", 5000), 5000, "TARGET"},
		{"match at end", strings.Repeat("x", 5000) + "TARGET", 5000, "TARGET"},
		// Multibyte runes everywhere: the window must not split a rune, or the
		// preview would contain invalid UTF-8.
		{"multibyte around match", strings.Repeat("é", 3000) + "TARGET" + strings.Repeat("日", 3000), 6000, "TARGET"},
		{"start past end", strings.Repeat("a", 3000), 99999, "aaa"},
		// Raw control bytes from file content must not reach the terminal: an
		// ESC sequence in a preview would be interpreted by the renderer.
		{"escape sequence stripped", "before\x1b[2Jafter TARGET", 0, "before [2Jafter TARGET"},
		{"tab replaced", "a\tTARGET", 2, "a TARGET"},
		{"negative start", strings.Repeat("a", 3000), -1, "aaa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := previewAround(tt.line, tt.start)
			if !utf8.ValidString(got) {
				t.Fatalf("preview is not valid UTF-8: %q", got)
			}
			if !strings.Contains(got, tt.wantFrag) {
				t.Errorf("preview %q does not contain %q", got, tt.wantFrag)
			}
			if len(got) > maxPreviewBytes+2*len("…") {
				t.Errorf("preview is %d bytes, want at most %d", len(got), maxPreviewBytes)
			}
		})
	}
}
