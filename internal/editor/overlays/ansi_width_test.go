package overlays

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

func TestOverlayViewsTruncateUnicodeAtCellBoundaries(t *testing.T) {
	long := strings.Repeat("界", 80) + "🎉"
	tests := []struct {
		name     string
		view     string
		maxWidth int // content plus the component's box chrome
	}{
		{
			name: "autocomplete",
			view: func() string {
				a := NewAutocomplete(ui.DefaultTheme())
				a.Show([]AutocompleteItem{{Label: long, Detail: long, InsertText: "x"}})
				return a.View()
			}(),
			maxWidth: 62,
		},
		{
			name: "hover",
			view: func() string {
				h := NewHover(ui.DefaultTheme())
				h.Show(long)
				return h.View()
			}(),
			maxWidth: 64,
		},
		{
			name: "signature",
			view: func() string {
				s := NewSignatureHelp(ui.DefaultTheme())
				s.Show(&SignatureData{Signatures: []SignatureInfo{{Label: long, Documentation: long}}})
				return s.View()
			}(),
			maxWidth: 74,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, line := range strings.Split(tt.view, "\n") {
				plain := ansi.Strip(line)
				if !utf8.ValidString(plain) {
					t.Fatalf("truncation split UTF-8: %q", plain)
				}
				if got := ansi.StringWidth(line); got > tt.maxWidth {
					t.Fatalf("line width = %d, want <= %d: %q", got, tt.maxWidth, plain)
				}
			}
		})
	}
}
