package editor

import (
	"strings"

	"teak/internal/ui"
)

// AutocompleteItem represents a single completion suggestion.
type AutocompleteItem struct {
	Label      string
	Detail     string
	InsertText string
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

// View renders the autocomplete popup as a string.
func (a Autocomplete) View() string {
	if !a.Visible || len(a.Items) == 0 {
		return ""
	}

	maxItems := min(10, len(a.Items))
	maxWidth := 0
	for i := range maxItems {
		w := len(a.Items[i].Label)
		if a.Items[i].Detail != "" {
			w += len(a.Items[i].Detail) + 2
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
		line := item.Label
		if item.Detail != "" {
			remaining := maxWidth - len(line) - 2
			if remaining > 0 {
				detail := item.Detail
				if len(detail) > remaining {
					detail = detail[:remaining]
				}
				line += strings.Repeat(" ", max(1, maxWidth-len(line)-len(detail))) + detail
			}
		}
		// Pad to width
		if len(line) < maxWidth {
			line += strings.Repeat(" ", maxWidth-len(line))
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
