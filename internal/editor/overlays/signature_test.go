package overlays

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// TestSignatureHelpViewSkipsEmptyParameterHighlight pins that a parameter
// without a label does not get brackets inserted at the start of the signature.
// strings.Replace with an empty old string matches at index 0.
func TestSignatureHelpViewSkipsEmptyParameterHighlight(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{{
			Label:      "f(a, b)",
			Parameters: []ParameterInfo{{Label: ""}},
		}},
		ActiveParameter: 0,
	})

	plain := ansi.Strip(s.View())
	if strings.Contains(plain, "[]") {
		t.Fatalf("empty parameter label inserted brackets into signature:\n%s", plain)
	}
	if !strings.Contains(plain, "f(a, b)") {
		t.Fatalf("signature label missing from view:\n%s", plain)
	}
}
