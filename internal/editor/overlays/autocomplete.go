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
	Items    []AutocompleteItem
	allItems []AutocompleteItem
	Cursor   int
	Visible  bool
	theme    ui.Theme
}

const maxVisibleItems = 10

// NewAutocomplete creates a new autocomplete popup.
func NewAutocomplete(theme ui.Theme) Autocomplete {
	return Autocomplete{theme: theme}
}

// Show displays the autocomplete popup with the given items.
func (a *Autocomplete) Show(items []AutocompleteItem) {
	a.allItems = items
	a.Items = items
	a.Cursor = 0
	a.Visible = len(items) > 0
}

// Hide dismisses the autocomplete popup.
func (a *Autocomplete) Hide() {
	a.Visible = false
	a.Items = nil
	a.allItems = nil
	a.Cursor = 0
}

// Filter narrows the popup to items whose label starts with prefix
// (case-insensitive), keeping the original list so later keystrokes can widen
// the match again. An empty prefix restores the full list; a prefix that
// matches nothing hides the popup instead of rendering an empty box.
func (a *Autocomplete) Filter(prefix string) {
	if !a.Visible {
		return
	}
	source := a.allItems
	if source == nil {
		source = a.Items
	}
	if prefix == "" {
		a.Items = source
		a.Cursor = 0
		return
	}
	p := strings.ToLower(prefix)
	filtered := make([]AutocompleteItem, 0, len(source))
	for _, item := range source {
		if strings.HasPrefix(strings.ToLower(item.Label), p) {
			filtered = append(filtered, item)
		}
	}
	a.Items = filtered
	a.Cursor = 0
	if len(filtered) == 0 {
		a.Hide()
	}
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

func (a Autocomplete) visibleStart() int {
	if len(a.Items) <= maxVisibleItems {
		return 0
	}
	return min(max(0, a.Cursor-(maxVisibleItems-1)), len(a.Items)-maxVisibleItems)
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
	start := a.visibleStart()
	absolute := start + idx
	if !a.Visible || idx < 0 || idx >= maxVisibleItems || absolute >= len(a.Items) {
		return nil
	}
	a.Cursor = absolute
	return &a.Items[absolute]
}

// View renders the autocomplete popup as a string.
func (a Autocomplete) View() string {
	if !a.Visible || len(a.Items) == 0 {
		return ""
	}

	start := a.visibleStart()
	end := min(start+maxVisibleItems, len(a.Items))
	maxWidth := 0
	for i := start; i < end; i++ {
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
	for i := start; i < end; i++ {
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
		if i < end-1 {
			sb.WriteByte('\n')
		}
	}

	return a.theme.AutocompleteBox.Render(sb.String())
}
