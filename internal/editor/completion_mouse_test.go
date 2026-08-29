package editor

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/editor/overlays"
)

// newEditorWithVisibleCompletion returns an editor whose cursor sits at a
// completion point with the popup showing one unmistakable item.
func newEditorWithVisibleCompletion(t *testing.T) Editor {
	t.Helper()
	e := newEditor("fm\nsecond\nthird", 0, 2)
	e.ShowAutocomplete([]overlays.AutocompleteItem{{
		Label:      "fmt",
		InsertText: "INSERTED_FMT",
	}})
	return e
}

func TestBufferLeftClickDismissesAutocomplete(t *testing.T) {
	e := newEditorWithVisibleCompletion(t)

	// Click line 2 inside the text area, one column past the gutter.
	clickX := e.effectiveGutterWidth() + 1
	e, _ = e.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: 2})

	if e.autocomplete.Visible {
		t.Fatal("left click away from the completion point left the autocomplete popup visible")
	}

	// With the popup dismissed, Enter must insert a newline at the clicked
	// position rather than the stale completion's InsertText.
	linesBefore := e.Buffer.LineCount()
	e, _ = e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	content := string(e.Buffer.Bytes())
	if strings.Contains(content, "INSERTED_FMT") {
		t.Fatalf("enter after a dismissing click inserted the stale completion: %q", content)
	}
	if got := e.Buffer.LineCount(); got != linesBefore+1 {
		t.Errorf("line count = %d, want %d (enter should insert a newline)", got, linesBefore+1)
	}
}

func TestBufferRightClickDismissesAutocomplete(t *testing.T) {
	e := newEditorWithVisibleCompletion(t)

	clickX := e.effectiveGutterWidth() + 1
	e, _ = e.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: clickX, Y: 2})

	if !e.contextMenu.Visible {
		t.Fatal("right click did not open the context menu")
	}
	if e.autocomplete.Visible {
		t.Fatal("right click away from the completion point left the autocomplete popup visible")
	}
}

func TestBufferShiftClickDismissesAutocomplete(t *testing.T) {
	e := newEditorWithVisibleCompletion(t)

	// Shift+click extends the selection from the completion point to the
	// clicked position; the selection head moves away, so Enter must not be
	// able to replace that selection with the stale completion.
	clickX := e.effectiveGutterWidth() + 1
	e, _ = e.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: 2, Mod: tea.ModShift})

	if e.autocomplete.Visible {
		t.Fatal("shift click away from the completion point left the autocomplete popup visible")
	}

	e, _ = e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	content := string(e.Buffer.Bytes())
	if strings.Contains(content, "INSERTED_FMT") {
		t.Fatalf("enter after a dismissing shift click inserted the stale completion: %q", content)
	}
}

func TestAutocompleteScrollMovesSelectionAndWindow(t *testing.T) {
	e := newEditor("fm\n", 0, 2)
	items := make([]overlays.AutocompleteItem, 15)
	for i := range items {
		label := fmt.Sprintf("item%02d", i)
		items[i] = overlays.AutocompleteItem{Label: label, InsertText: label}
	}
	e.ShowAutocomplete(items)

	// One wheel notch moves the selection three items, matching the tab strip.
	e.AutocompleteScroll(3)
	if got := e.autocomplete.Cursor; got != 3 {
		t.Fatalf("selection = %d, want 3 after one wheel notch", got)
	}

	// The visible window derives from the selection, so scrolling deep enough
	// advances it: item00 scrolls out while the selected item12 scrolls in.
	e.AutocompleteScroll(9)
	if got := e.autocomplete.Cursor; got != 12 {
		t.Fatalf("selection = %d, want 12 after scrolling further", got)
	}
	view := e.AutocompleteView()
	if strings.Contains(view, "item00") {
		t.Error("popup still shows item00 after the window scrolled past it")
	}
	if !strings.Contains(view, "item12") {
		t.Error("popup does not show the selected item12 after scrolling")
	}
}
