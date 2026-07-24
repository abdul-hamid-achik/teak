package overlays

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// AutocompleteItem represents a single completion suggestion.
type AutocompleteItem struct {
	Label      string
	Detail     string
	InsertText string

	// Edit, when HasEdit is set, is the buffer range the server wants replaced
	// (0-based line, UTF-8 byte column). Without it the typed prefix is left in
	// place and the completion is appended to it.
	Edit    AutocompleteEdit
	HasEdit bool
}

// AutocompleteEdit is a completion's replacement range in buffer coordinates.
type AutocompleteEdit struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// Autocomplete manages the autocomplete popup state.
type Autocomplete struct {
	Items   []AutocompleteItem
	Cursor  int
	Visible bool
	theme   ui.Theme
}

// NewAutocomplete creates a new autocomplete popup.
func NewAutocomplete(theme ui.Theme) Autocomplete {
	return Autocomplete{theme: theme}
}

// Show displays the autocomplete popup with the given items.
func (a *Autocomplete) Show(items []AutocompleteItem) {
	a.Items = items
	a.Cursor = 0
	a.Visible = len(items) > 0
}

// Hide dismisses the autocomplete popup.
func (a *Autocomplete) Hide() {
	a.Visible = false
	a.Items = nil
	a.Cursor = 0
}

// MoveUp moves the cursor up.
func (a *Autocomplete) MoveUp() {
	if a.Cursor > 0 {
		a.Cursor--
	}
}

// MoveDown moves the cursor down.
func (a *Autocomplete) MoveDown() {
	if a.Cursor < len(a.Items)-1 {
		a.Cursor++
	}
}

// Selected returns the currently selected item, or nil.
func (a *Autocomplete) Selected() *AutocompleteItem {
	if !a.Visible || a.Cursor >= len(a.Items) {
		return nil
	}
	return &a.Items[a.Cursor]
}

// SelectAt selects the item at the given index (relative to popup top).
// Returns the selected item if valid, or nil.
func (a *Autocomplete) SelectAt(idx int) *AutocompleteItem {
	if !a.Visible || idx < 0 || idx >= len(a.Items) || idx >= 10 {
		return nil
	}
	a.Cursor = idx
	return &a.Items[idx]
}

// View renders the autocomplete popup as a string.
func (a Autocomplete) View() string {
	if !a.Visible || len(a.Items) == 0 {
		return ""
	}

	maxItems := min(10, len(a.Items))
	maxWidth := 0
	for i := range maxItems {
		w := ansi.StringWidth(a.Items[i].Label)
		if a.Items[i].Detail != "" {
			w += ansi.StringWidth(a.Items[i].Detail) + 2
		}
		if w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth < 20 {
		maxWidth = 20
	}
	if maxWidth > 60 {
		maxWidth = 60
	}

	var sb strings.Builder
	for i := range maxItems {
		item := a.Items[i]
		line := ansi.Truncate(item.Label, maxWidth, "")
		if item.Detail != "" {
			remaining := maxWidth - ansi.StringWidth(line) - 2
			if remaining > 0 {
				detail := ansi.Truncate(item.Detail, remaining, "")
				line += strings.Repeat(" ", max(1, maxWidth-ansi.StringWidth(line)-ansi.StringWidth(detail))) + detail
			}
		}
		// Pad to width
		if ansi.StringWidth(line) < maxWidth {
			line += strings.Repeat(" ", maxWidth-ansi.StringWidth(line))
		}
		if i == a.Cursor {
			sb.WriteString(a.theme.AutocompleteCursor.Render(line))
		} else {
			sb.WriteString(a.theme.AutocompleteItem.Render(line))
		}
		if i < maxItems-1 {
			sb.WriteByte('\n')
		}
	}

	return a.theme.AutocompleteBox.Render(sb.String())
}
