package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/editor/overlays"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestLSPOverlayViewPrecedence(t *testing.T) {
	ed := New(text.NewBufferFromBytes([]byte("call")), ui.DefaultTheme(), DefaultConfig())
	ed.ShowHover("hover wins only when alone")
	ed.ShowSignatureHelp(&overlays.SignatureData{Signatures: []overlays.SignatureInfo{{Label: "signature(a int)"}}})

	if got := ansi.Strip(ed.LSPOverlayView()); !strings.Contains(got, "signature(a int)") {
		t.Fatalf("signature overlay = %q, want signature help before hover", got)
	}

	ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "completion", InsertText: "completion"}})
	if got := ansi.Strip(ed.LSPOverlayView()); !strings.Contains(got, "completion") {
		t.Fatalf("autocomplete overlay = %q, want autocomplete before signature/hover", got)
	}

	ed.HideAutocomplete()
	ed.HideSignatureHelp()
	if got := ansi.Strip(ed.LSPOverlayView()); !strings.Contains(got, "hover wins only when alone") {
		t.Fatalf("hover overlay = %q, want hover after higher-priority overlays close", got)
	}
}
