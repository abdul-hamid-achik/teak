package overlays

import (
	"testing"

	"teak/internal/ui"
)

func TestSetThemePreservesOverlayState(t *testing.T) {
	next := ui.DraculaTheme()

	autocomplete := NewAutocomplete(ui.NordTheme())
	autocomplete.Show([]AutocompleteItem{{Label: "item"}})
	autocomplete.SetTheme(next)
	if autocomplete.theme != next || !autocomplete.Visible || len(autocomplete.Items) != 1 {
		t.Fatal("Autocomplete SetTheme changed state or left the old theme")
	}

	hover := NewHover(ui.NordTheme())
	hover.Show("documentation")
	hover.SetTheme(next)
	if hover.theme != next || !hover.Visible || hover.Content != "documentation" {
		t.Fatal("Hover SetTheme changed state or left the old theme")
	}

	signature := NewSignatureHelp(ui.NordTheme())
	signature.Show(&SignatureData{Signatures: []SignatureInfo{{Label: "fn()"}}})
	signature.SetTheme(next)
	if signature.theme != next || !signature.Visible || signature.Help == nil {
		t.Fatal("SignatureHelp SetTheme changed state or left the old theme")
	}
}
