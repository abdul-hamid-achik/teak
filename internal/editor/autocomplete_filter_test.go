package editor

import (
	"strings"
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
	var cmd tea.Cmd
	ed, cmd = ed.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !ed.autocomplete.Visible {
		t.Fatal("popup hidden even though the typed prefix still matches")
	}
	if view := ed.AutocompleteView(); !strings.Contains(view, "Filtering") {
		t.Fatalf("view immediately after typing = %q, want asynchronous filtering state", view)
	}
	ed = drainAutocompleteTestCommand(t, ed, cmd)
	if len(ed.autocomplete.Items) != 1 || ed.autocomplete.Items[0].Label != "apple" {
		t.Fatalf("items after typing p = %+v, want only apple", ed.autocomplete.Items)
	}

	// Type "z": prefix becomes "appz" — nothing matches, popup must close.
	ed, cmd = ed.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if !ed.autocomplete.Visible {
		t.Fatal("popup closed synchronously before the current filter completed")
	}
	ed = drainAutocompleteTestCommand(t, ed, cmd)
	if ed.autocomplete.Visible {
		t.Fatal("popup still visible after the prefix stopped matching")
	}
}

func TestAutocompleteEnterWaitsForCurrentFilter(t *testing.T) {
	ed := autocompleteTestEditor(t)
	ed, filterCmd := ed.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	ed, enterCmd := ed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if enterCmd != nil {
		t.Fatal("Enter while filtering scheduled unrelated work")
	}
	if got := string(ed.Buffer.Bytes()); got != "app\n" {
		t.Fatalf("buffer before filter completion = %q, want typed prefix only", got)
	}

	ed = drainAutocompleteTestCommand(t, ed, filterCmd)
	if got := string(ed.Buffer.Bytes()); got != "appapple\n" {
		t.Fatalf("buffer after pending completion = %q, want selected current match", got)
	}
	if ed.autocomplete.Visible {
		t.Fatal("autocomplete remained visible after applying pending selection")
	}
}

func TestAutocompleteTypingInvalidatesPendingItemPreparation(t *testing.T) {
	ed := autocompleteTestEditor(t)
	generation := ed.BeginAutocompleteLoading()
	ed, _ = ed.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if ed.AcceptsAutocompleteItems(generation) {
		t.Fatal("typing accepted the obsolete item-preparation generation")
	}
	if ed.autocomplete.Visible {
		t.Fatal("obsolete loading shell remained visible after typing")
	}
}

func drainAutocompleteTestCommand(t *testing.T, ed Editor, cmd tea.Cmd) Editor {
	t.Helper()
	if cmd == nil {
		t.Fatal("editor returned no autocomplete filtering command")
	}
	var apply func(Editor, tea.Msg) Editor
	apply = func(current Editor, msg tea.Msg) Editor {
		if msg == nil {
			return current
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				if child != nil {
					current = apply(current, child())
				}
			}
			return current
		}
		updated, next := current.Update(msg)
		if next != nil {
			updated = apply(updated, next())
		}
		return updated
	}
	return apply(ed, cmd())
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
