package editor

import "testing"

func TestDetectIndentRegionsIgnoresWhitespaceOnlyBlankLines(t *testing.T) {
	// Whitespace-only lines have no content, so they must not open or extend a
	// fold. Tab-only lines score a large indent, and CRLF blank lines end in \r
	// and must not end the enclosing block early.
	tests := []struct {
		name  string
		lines []string
		want  []FoldRegion
	}{
		{"tab-only line is not content", []string{"a", "\t\t", "b"}, nil},
		{"CRLF blank line keeps block open", []string{"f {", "\tx", "\r", "\ty", "}"}, []FoldRegion{{StartLine: 0, EndLine: 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIndentRegions(func(i int) []byte { return []byte(tt.lines[i]) }, len(tt.lines))
			if len(got) != len(tt.want) {
				t.Fatalf("DetectIndentRegions(%q) = %+v, want %+v", tt.lines, got, tt.want)
			}
			for i := range got {
				if got[i].StartLine != tt.want[i].StartLine || got[i].EndLine != tt.want[i].EndLine {
					t.Fatalf("DetectIndentRegions(%q) = %+v, want %+v", tt.lines, got, tt.want)
				}
			}
		})
	}
}
