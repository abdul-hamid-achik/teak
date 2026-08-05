package overlays

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
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

// AutocompleteFilterReadyMsg returns a client-side completion projection to
// the editor that started it. EditorID and Generation keep late results from
// a closed tab or superseded query harmless.
type AutocompleteFilterReadyMsg struct {
	EditorID   uint64
	Generation uint64
	Items      []AutocompleteItem
	Err        error
}

// Autocomplete manages the autocomplete popup state.
type Autocomplete struct {
	Items            []AutocompleteItem
	allItems         []AutocompleteItem
	Cursor           int
	Visible          bool
	theme            ui.Theme
	itemsLoading     bool
	itemsGeneration  uint64
	filterPending    bool
	filterGeneration uint64
	filterCancel     context.CancelFunc
	pendingSelection bool
}

const maxVisibleItems = 10

// NewAutocomplete creates a new autocomplete popup.
func NewAutocomplete(theme ui.Theme) Autocomplete {
	return Autocomplete{theme: theme}
}

// Show displays the autocomplete popup with the given items.
func (a *Autocomplete) Show(items []AutocompleteItem) {
	a.cancelFilter()
	a.itemsGeneration++
	a.itemsLoading = false
	a.pendingSelection = false
	a.allItems = items
	a.Items = items
	a.Cursor = 0
	a.Visible = len(items) > 0
}

// Hide dismisses the autocomplete popup.
func (a *Autocomplete) Hide() {
	a.cancelFilter()
	a.itemsGeneration++
	a.itemsLoading = false
	a.pendingSelection = false
	a.Visible = false
	a.Items = nil
	a.allItems = nil
	a.Cursor = 0
}

// BeginLoading installs a constant-size loading shell while completion items
// are converted outside Update. The returned generation must accompany the
// prepared result before it can be installed.
func (a *Autocomplete) BeginLoading() uint64 {
	a.cancelFilter()
	a.itemsGeneration++
	a.itemsLoading = true
	a.pendingSelection = false
	a.Visible = true
	a.Items = nil
	a.allItems = nil
	a.Cursor = 0
	return a.itemsGeneration
}

// AcceptsItems reports whether a prepared completion collection still belongs
// to the visible loading request.
func (a *Autocomplete) AcceptsItems(generation uint64) bool {
	return a.Visible && a.itemsLoading && generation == a.itemsGeneration
}

// InstallItems accepts a prepared collection and schedules its initial query
// projection. No work proportional to item count runs in the caller.
func (a *Autocomplete) InstallItems(editorID, generation uint64, items []AutocompleteItem, prefix string) tea.Cmd {
	if !a.AcceptsItems(generation) {
		return nil
	}
	a.itemsLoading = false
	a.allItems = items
	a.Items = nil
	a.Cursor = 0
	if len(items) == 0 {
		a.Hide()
		return nil
	}
	return a.ScheduleFilter(editorID, prefix)
}

// Filter narrows the popup to items whose label starts with prefix
// (case-insensitive), keeping the original list so later keystrokes can widen
// the match again. An empty prefix restores the full list; a prefix that
// matches nothing hides the popup instead of rendering an empty box.
func (a *Autocomplete) Filter(prefix string) {
	if !a.Visible {
		return
	}
	a.cancelFilter()
	a.itemsLoading = false
	a.pendingSelection = false
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

// ScheduleFilter projects the current source collection outside Update. A
// newer query cancels this command and advances the generation before its
// result can mutate the popup.
func (a *Autocomplete) ScheduleFilter(editorID uint64, prefix string) tea.Cmd {
	if !a.Visible || a.itemsLoading {
		return nil
	}
	a.cancelFilter()
	source := a.allItems
	if source == nil {
		source = a.Items
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.filterCancel = cancel
	a.filterPending = true
	a.Items = nil
	a.Cursor = 0
	generation := a.filterGeneration
	return func() tea.Msg {
		items, err := filterAutocompleteItemsContext(ctx, source, prefix)
		return AutocompleteFilterReadyMsg{
			EditorID:   editorID,
			Generation: generation,
			Items:      items,
			Err:        err,
		}
	}
}

// ApplyFilter installs only the newest query result. A pending Enter or Tab is
// returned to the editor for application after the current projection exists.
func (a *Autocomplete) ApplyFilter(msg AutocompleteFilterReadyMsg) *AutocompleteItem {
	if msg.Generation != a.filterGeneration || !a.filterPending {
		return nil
	}
	if a.filterCancel != nil {
		a.filterCancel()
		a.filterCancel = nil
	}
	a.filterPending = false
	if msg.Err != nil || len(msg.Items) == 0 {
		a.Hide()
		return nil
	}
	a.Items = msg.Items
	a.Cursor = 0
	a.Visible = true
	if !a.pendingSelection {
		return nil
	}
	item := a.Items[0]
	a.Hide()
	return &item
}

// RequestSelection remembers Enter or Tab while the current projection is
// pending so the key cannot select a stale completion or insert unrelated text.
func (a *Autocomplete) RequestSelection() {
	if a.Pending() {
		a.pendingSelection = true
	}
}

// Pending reports whether item conversion or query projection is unfinished.
func (a *Autocomplete) Pending() bool { return a.itemsLoading || a.filterPending }

// ItemsLoading distinguishes server-item preparation from client filtering.
func (a *Autocomplete) ItemsLoading() bool { return a.itemsLoading }

// MoveUp moves the cursor up.
func (a *Autocomplete) MoveUp() {
	if a.Pending() {
		return
	}
	if a.Cursor > 0 {
		a.Cursor--
	}
}

// MoveDown moves the cursor down.
func (a *Autocomplete) MoveDown() {
	if a.Pending() {
		return
	}
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
	if !a.Visible || a.Pending() || a.Cursor >= len(a.Items) {
		return nil
	}
	return &a.Items[a.Cursor]
}

// SelectAt selects the item at the given index (relative to popup top).
// Returns the selected item if valid, or nil.
func (a *Autocomplete) SelectAt(idx int) *AutocompleteItem {
	start := a.visibleStart()
	absolute := start + idx
	if !a.Visible || a.Pending() || idx < 0 || idx >= maxVisibleItems || absolute >= len(a.Items) {
		return nil
	}
	a.Cursor = absolute
	return &a.Items[absolute]
}

// View renders the autocomplete popup as a string.
func (a Autocomplete) View() string {
	if !a.Visible {
		return ""
	}
	if a.itemsLoading {
		return a.pendingView("Loading...")
	}
	if a.filterPending {
		return a.pendingView("Filtering...")
	}
	if len(a.Items) == 0 {
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

func (a Autocomplete) pendingView(label string) string {
	if width := ansi.StringWidth(label); width < 20 {
		label += strings.Repeat(" ", 20-width)
	}
	return a.theme.AutocompleteBox.Render(a.theme.AutocompleteItem.Render(label))
}

func (a *Autocomplete) cancelFilter() {
	if a.filterCancel != nil {
		a.filterCancel()
		a.filterCancel = nil
	}
	a.filterPending = false
	a.filterGeneration++
}

func filterAutocompleteItemsContext(ctx context.Context, items []AutocompleteItem, prefix string) ([]AutocompleteItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if prefix == "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return items, nil
	}
	lowerPrefix := strings.ToLower(prefix)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filtered := make([]AutocompleteItem, 0, min(len(items), 256))
	for i, item := range items {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(strings.ToLower(item.Label), lowerPrefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered, ctx.Err()
}
