package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor/overlays"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestAutocompleteFilterNarrowsByPrefix(t *testing.T) {
	a := overlays.NewAutocomplete(ui.DefaultTheme())
	a.Show([]overlays.AutocompleteItem{
		{Label: "apple"}, {Label: "Apply"}, {Label: "banana"},
	})

	a.Filter("app")
	if len(a.Items) != 2 {
		t.Fatalf("Items after Filter(app) = %d, want 2 (case-insensitive prefix)", len(a.Items))
	}

	a.Filter("appz")
	if a.Visible {
		t.Fatal("popup still visible after the prefix stopped matching anything")
	}
}

func TestAutocompleteFilterEmptyPrefixRestoresAll(t *testing.T) {
	a := overlays.NewAutocomplete(ui.DefaultTheme())
	a.Show([]overlays.AutocompleteItem{{Label: "apple"}, {Label: "banana"}})

	a.Filter("app")
	if len(a.Items) != 1 {
		t.Fatalf("Items after Filter(app) = %d, want 1", len(a.Items))
	}
	a.Filter("")
	if len(a.Items) != 2 {
		t.Fatalf("Items after clearing the prefix = %d, want the full list back", len(a.Items))
	}
}

func autocompleteTestEditor(t *testing.T) Editor {
	t.Helper()
	buf := text.NewBufferFromBytes([]byte("ap\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 2})
	ed.ShowAutocomplete([]overlays.AutocompleteItem{
		{Label: "apple", InsertText: "apple"},
		{Label: "banana", InsertText: "banana"},
	})
	return ed
}

func TestAutocompleteRefiltersWhileTyping(t *testing.T) {
	ed := autocompleteTestEditor(t)

	// Type "p": prefix becomes "app" — banana no longer matches.
	ed, _ = ed.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !ed.autocomplete.Visible {
		t.Fatal("popup hidden even though the typed prefix still matches")
	}
	if len(ed.autocomplete.Items) != 1 || ed.autocomplete.Items[0].Label != "apple" {
		t.Fatalf("items after typing p = %+v, want only apple", ed.autocomplete.Items)
	}

	// Type "z": prefix becomes "appz" — nothing matches, popup must close.
	ed, _ = ed.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if ed.autocomplete.Visible {
		t.Fatal("popup still visible after the prefix stopped matching")
	}
}

func TestAutocompleteHidesOnCursorNavigation(t *testing.T) {
	ed := autocompleteTestEditor(t)

	ed, _ = ed.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if ed.autocomplete.Visible {
		t.Fatal("popup still visible after moving the cursor left")
	}
	if ed.Buffer.Cursor.Col != 1 {
		t.Fatalf("cursor col = %d, want 1 (navigation must still move the cursor)", ed.Buffer.Cursor.Col)
	}
}
