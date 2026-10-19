package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"teak/internal/highlight"
	"teak/internal/ui"
)

// A wide rune cut by the horizontal scroll edge must leave a blank cell in
// its place so the remaining text keeps its display columns.
func TestApplyScrollXWideRuneCutBySliceEdge(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		scrollX int
		want    string
	}{
		{name: "half of leading wide rune", in: "你好x", scrollX: 1, want: " 好x"},
		{name: "scroll ends on wide rune boundary", in: "你好x", scrollX: 2, want: "好x"},
		{name: "ascii only", in: "abc", scrollX: 1, want: "bc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyScrollX(tt.in, tt.scrollX); got != tt.want {
				t.Errorf("applyScrollX(%q, %d) = %q (width %d), want %q", tt.in, tt.scrollX, got, displayWidth(got), tt.want)
			}
			got, remaining := applyScrollXCount(tt.in, tt.scrollX)
			if got != tt.want || remaining != 0 {
				t.Errorf("applyScrollXCount(%q, %d) = (%q, %d), want (%q, 0)", tt.in, tt.scrollX, got, remaining, tt.want)
			}
		})
	}
}

// Highlighted lines take the token renderer; a wide rune straddling ScrollX
// there must also keep the following text in its display column.
func TestTokenRenderWideRuneCutBySliceEdge(t *testing.T) {
	tests := []struct {
		name    string
		tokens  []string
		scrollX int
		want    string
	}{
		{name: "single token half wide rune", tokens: []string{"你好x"}, scrollX: 1, want: " 好x"},
		{name: "wide rune cut across tokens", tokens: []string{"a", "你好x"}, scrollX: 2, want: " 好x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokens []highlight.StyledToken
			for _, s := range tt.tokens {
				tokens = append(tokens, highlight.StyledToken{Text: s})
			}
			v := Viewport{ScrollX: tt.scrollX, TabSize: 4}
			var sb strings.Builder
			v.renderLineWithTokensInto(&sb, tokens, false, 6, ui.DefaultTheme())
			got := strings.TrimRight(ansi.Strip(sb.String()), " ")
			if got != tt.want {
				t.Errorf("rendered %q (width %d), want %q", got, ansi.StringWidth(got), tt.want)
			}
		})
	}
}
