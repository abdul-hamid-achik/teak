package overlay

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// PickerItem is a single selectable entry in the picker.
type PickerItem struct {
	Label       string // display text
	Description string // secondary text (shown dimmed)
	Value       any    // opaque payload returned on selection
	Search      string // optional extra haystack for fuzzy match (e.g. full path)
	Recency     int    // higher ranks more recently used files first
}

// PickerSelectMsg is emitted when the user selects an item.
type PickerSelectMsg struct {
	Item PickerItem
}

// PickerCloseMsg is emitted when the user dismisses the picker.
type PickerCloseMsg struct{}

// PickerFilterReadyMsg contains a projection built from an immutable picker
// item slice. ZoneID, query and generation prevent a slower result from
// replacing a newer query or a different picker after the overlay changed.
type PickerFilterReadyMsg struct {
	InstanceID uint64
	ZoneID     string
	Generation uint64
	Query      string
	Matches    []PickerMatch
	Err        error
}

// PickerItemsReadyMsg carries an item list prepared away from the Bubble Tea
// update loop. The picker will schedule its query projection after installing
// the immutable slice.
type PickerItemsReadyMsg struct {
	InstanceID uint64
	ZoneID     string
	Generation uint64
	Items      []PickerItem
	Err        error
}

// PickerMatch is one item that matched the current picker query.
type PickerMatch struct {
	Item  PickerItem
	Score int
}

// Picker is a fuzzy-filterable list overlay with a text input, scrollable
// results, and keyboard/mouse navigation. It implements the Overlay interface.
type Picker struct {
	input            textinput.Model
	items            []PickerItem
	filtered         []scoredItem
	cursor           int
	scrollY          int
	theme            ui.Theme
	width            int
	maxHeight        int
	dismissed        bool
	title            string
	zoneID           string // unique prefix for mouse zones
	filterPending    bool
	filterGeneration uint64
	filterCancel     context.CancelFunc
	instanceID       uint64
	itemsPending     bool
	itemsGeneration  uint64
	itemsCancel      context.CancelFunc
	pendingSelect    bool
	dismissAction    tea.Msg
}

type scoredItem = PickerMatch

var pickerInstanceSequence uint64

// NewPicker creates a picker overlay.
// zoneID should be unique per picker instance to avoid zone collisions.
func NewPicker(title string, items []PickerItem, theme ui.Theme, zoneID string) *Picker {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.CharLimit = 128

	p := &Picker{
		input:      ti,
		items:      items,
		theme:      theme,
		width:      60,
		maxHeight:  20,
		title:      title,
		zoneID:     zoneID,
		instanceID: atomic.AddUint64(&pickerInstanceSequence, 1),
	}
	p.refilter()
	return p
}

// NewPendingPicker creates a picker whose items will be prepared by a command.
// Until the matching PickerItemsReadyMsg arrives, selection and rendering use
// an explicit loading state instead of briefly claiming there are no matches.
func NewPendingPicker(title string, theme ui.Theme, zoneID string) *Picker {
	p := NewPicker(title, nil, theme, zoneID)
	p.itemsPending = true
	return p
}

// Focus gives keyboard focus to the text input.
func (p *Picker) Focus() tea.Cmd {
	return p.input.Focus()
}

// SetSize updates the available dimensions.
func (p *Picker) SetSize(w, h int) {
	p.width = max(1, w)
	p.maxHeight = max(1, h)
	p.input.SetWidth(max(1, min(p.width-8, 50)))
}

// SetItems replaces the item list and refilters.
func (p *Picker) SetItems(items []PickerItem) {
	p.cancelItemsPreparation()
	p.cancelFilter()
	p.pendingSelect = false
	p.items = items
	p.refilter()
}

// SetItemsAsync replaces the item list and builds the current query
// projection in a cancellable command. It is intended for large result sets
// arriving from filesystem scans or other background operations.
func (p *Picker) SetItemsAsync(items []PickerItem) tea.Cmd {
	p.cancelItemsPreparation()
	return p.installItemsAsync(items)
}

func (p *Picker) installItemsAsync(items []PickerItem) tea.Cmd {
	p.cancelFilter()
	p.itemsPending = false
	p.items = items
	return p.scheduleFilter()
}

// PrepareItemsCmd runs an item projection outside Bubble Tea's Update loop.
// A newer preparation or picker cancellation invalidates and cancels the old
// one; instance, zone, and generation keep late results harmless.
func (p *Picker) PrepareItemsCmd(prepare func(context.Context) ([]PickerItem, error)) tea.Cmd {
	if prepare == nil {
		return nil
	}
	p.cancelItemsPreparation()
	p.cancelFilter()
	p.itemsPending = true
	p.pendingSelect = false
	p.items = nil
	p.filtered = nil
	p.cursor = 0
	p.scrollY = 0
	generation := p.itemsGeneration
	instanceID := p.instanceID
	zoneID := p.zoneID
	ctx, cancel := context.WithCancel(context.Background())
	p.itemsCancel = cancel
	return func() tea.Msg {
		items, err := prepare(ctx)
		return PickerItemsReadyMsg{
			InstanceID: instanceID,
			ZoneID:     zoneID,
			Generation: generation,
			Items:      items,
			Err:        err,
		}
	}
}

// ZoneID identifies the picker instance for callers that need to route an
// asynchronous result without relying on its user-visible title.
func (p *Picker) ZoneID() string {
	return p.zoneID
}

// InstanceID identifies this concrete picker instance so a result from a
// dismissed picker cannot populate a newer picker with the same zone.
func (p *Picker) InstanceID() uint64 { return p.instanceID }

// SetDismissAction supplies an optional message for Escape dismissal. It is
// useful for asynchronous owners such as plugin selectors that must resume a
// callback with an explicit cancellation result.
func (p *Picker) SetDismissAction(action tea.Msg) { p.dismissAction = action }

// DismissAction returns the message associated with Escape, if any.
func (p *Picker) DismissAction() tea.Msg { return p.dismissAction }

// Update implements Overlay.
func (p *Picker) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return p, p.dismiss()
		case "enter":
			if p.pending() {
				p.pendingSelect = true
				return p, nil
			}
			if len(p.filtered) > 0 && p.cursor < len(p.filtered) {
				item := p.filtered[p.cursor].Item
				p.dismissed = true
				return p, func() tea.Msg { return PickerSelectMsg{Item: item} }
			}
			return p, nil
		case "up":
			if p.cursor > 0 {
				p.cursor--
				p.ensureVisible()
			}
			return p, nil
		case "down":
			if p.cursor < len(p.filtered)-1 {
				p.cursor++
				p.ensureVisible()
			}
			return p, nil
		case "pgup":
			p.cursor -= p.visibleCount()
			if p.cursor < 0 {
				p.cursor = 0
			}
			p.ensureVisible()
			return p, nil
		case "pgdown":
			p.cursor += p.visibleCount()
			if p.cursor >= len(p.filtered) {
				p.cursor = len(p.filtered) - 1
			}
			if p.cursor < 0 {
				p.cursor = 0
			}
			p.ensureVisible()
			return p, nil
		}

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if boxZone := zone.Get(p.boxZoneID()); boxZone != nil && mouse.Button == tea.MouseLeft && !boxZone.InBounds(msg) {
			// A left click outside the rendered box dismisses the picker,
			// exactly like Escape. The click stays consumed by the overlay
			// stack's modal routing, so it never reaches the editor or the
			// tree underneath. Zone bounds come from the last scanned frame,
			// which is how item hit-testing already resolves positions. An
			// unpublished zone is treated as inside: zone bounds arrive from
			// an async scan worker, and dismissing before the first frame is
			// published would eat an in-box click under scripted input.
			return p, p.dismiss()
		}
		if p.pending() {
			return p, nil
		}
		if mouse.Button == tea.MouseLeft {
			start, end := p.visibleRange()
			for i := start; i < end; i++ {
				if zone.Get(p.itemZoneID(i)).InBounds(msg) {
					item := p.filtered[i].Item
					p.dismissed = true
					return p, func() tea.Msg { return PickerSelectMsg{Item: item} }
				}
			}
		}
		return p, nil

	case tea.MouseWheelMsg:
		if p.pending() {
			return p, nil
		}
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			p.scrollY -= 3
			if p.scrollY < 0 {
				p.scrollY = 0
			}
		case tea.MouseWheelDown:
			maxScroll := len(p.filtered) - p.visibleCount()
			if maxScroll < 0 {
				maxScroll = 0
			}
			p.scrollY += 3
			if p.scrollY > maxScroll {
				p.scrollY = maxScroll
			}
		}
		return p, nil
	case PickerFilterReadyMsg:
		return p, p.handleFilterReady(msg)
	case PickerItemsReadyMsg:
		if msg.InstanceID != p.instanceID || msg.ZoneID != p.zoneID ||
			(msg.Generation != 0 && msg.Generation != p.itemsGeneration) {
			return p, nil
		}
		if p.itemsCancel != nil {
			p.itemsCancel()
			p.itemsCancel = nil
		}
		p.itemsPending = false
		p.itemsGeneration++
		if msg.Err != nil {
			p.filtered = nil
			p.pendingSelect = false
			return p, nil
		}
		return p, p.installItemsAsync(msg.Items)
	}

	// Forward to text input
	prevVal := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prevVal {
		p.pendingSelect = false
		if p.itemsPending {
			return p, cmd
		}
		return p, tea.Batch(cmd, p.scheduleFilter())
	}
	return p, cmd
}

// View implements Overlay.
func (p *Picker) View() string {
	boxWidth := p.width
	if boxWidth < 30 && p.width >= 30 {
		boxWidth = 30
	}
	if boxWidth > 80 {
		boxWidth = 80
	}
	contentWidth := boxWidth - 6 // border + padding
	if contentWidth < 1 {
		contentWidth = 1
	}

	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	sb.WriteString(titleStyle.Render(p.title))
	sb.WriteString("\n\n")

	// Input
	sb.WriteString(p.input.View())
	sb.WriteByte('\n')

	// Results
	startIdx, endIdx := p.visibleRange()

	itemStyle := lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	cursorStyle := lipgloss.NewStyle().
		Background(ui.Nord2).
		Foreground(ui.Nord6)
	descStyle := lipgloss.NewStyle().
		Foreground(ui.Nord3)

	for i := startIdx; i < endIdx; i++ {
		si := p.filtered[i]
		label := truncStr(si.Item.Label, contentWidth)
		if si.Item.Description != "" {
			descWidth := contentWidth - lipgloss.Width(label) - 2
			if descWidth > 4 {
				label += "  " + descStyle.Render(truncStr(si.Item.Description, descWidth))
			}
		}

		style := itemStyle
		if i == p.cursor {
			style = cursorStyle
		}
		rendered := zone.Mark(p.itemZoneID(i), style.Width(contentWidth).Render(label))
		sb.WriteByte('\n')
		sb.WriteString(rendered)
	}

	if p.itemsPending {
		sb.WriteByte('\n')
		status := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(status.Render("  Loading..."))
	} else if p.filterPending {
		sb.WriteByte('\n')
		status := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(status.Render("  Filtering..."))
	} else if len(p.filtered) == 0 {
		sb.WriteByte('\n')
		noMatch := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(noMatch.Render("  No matches"))
	}

	// Scroll hint
	if len(p.filtered) > p.visibleCount() {
		sb.WriteByte('\n')
		hint := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(hint.Render(strings.Repeat(" ", max(0, contentWidth-10)) + countStr(p.cursor+1, len(p.filtered))))
	}

	content := sb.String()
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2).
		Width(boxWidth)

	// The box zone spans the entire rendered dialog (border and padding
	// included) so Update can distinguish "inside the picker, missed every
	// item" from "outside the picker" on mouse clicks.
	return zone.Mark(p.boxZoneID(), boxStyle.Render(content))
}

// IsDismissed implements Overlay.
func (p *Picker) IsDismissed() bool {
	return p.dismissed
}

// CapturesInput implements Overlay.
func (p *Picker) CapturesInput() bool {
	return true
}

// FilteredCount returns the number of items after filtering.
func (p *Picker) FilteredCount() int {
	if p.pending() {
		return 0
	}
	return len(p.filtered)
}

// FilterPending reports whether items or the current query are waiting for an
// asynchronous projection.
func (p *Picker) FilterPending() bool { return p.pending() }

// Cursor returns the current cursor position.
func (p *Picker) Cursor() int {
	return p.cursor
}

// Query returns the current input value.
func (p *Picker) Query() string {
	return p.input.Value()
}

func (p *Picker) refilter() {
	p.filterPending = false
	query := p.input.Value()
	// Keep the backing array between keystrokes. A project picker commonly
	// contains tens of thousands of files, and dropping this slice used to
	// allocate several megabytes for every character typed. Clear first so
	// items removed by a narrower result do not retain their values.
	clear(p.filtered)
	p.filtered = p.filtered[:0]

	if query == "" {
		for _, item := range p.items {
			p.filtered = append(p.filtered, scoredItem{Item: item, Score: 0})
		}
	} else {
		for _, item := range p.items {
			score, matched := FuzzyMatch(query, item.Label)
			if matched {
				p.filtered = append(p.filtered, scoredItem{Item: item, Score: score})
			}
		}
		sort.Slice(p.filtered, func(i, j int) bool {
			return p.filtered[i].Score > p.filtered[j].Score
		})
	}

	p.cursor = 0
	p.scrollY = 0
}

func (p *Picker) scheduleFilter() tea.Cmd {
	p.cancelFilter()
	p.filterGeneration++
	generation := p.filterGeneration
	query := p.input.Value()
	items := p.items
	ctx, cancel := context.WithCancel(context.Background())
	p.filterCancel = cancel
	p.filterPending = true
	p.cursor = 0
	p.scrollY = 0
	return func() tea.Msg {
		matches, err := filterItemsContext(ctx, items, query)
		return PickerFilterReadyMsg{
			InstanceID: p.instanceID,
			ZoneID:     p.zoneID,
			Generation: generation,
			Query:      query,
			Matches:    matches,
			Err:        err,
		}
	}
}

func (p *Picker) handleFilterReady(msg PickerFilterReadyMsg) tea.Cmd {
	if !p.filterPending || msg.InstanceID != p.instanceID || msg.ZoneID != p.zoneID || msg.Generation != p.filterGeneration || msg.Query != p.input.Value() {
		return nil
	}
	p.filterCancel = nil
	p.filterPending = false
	if msg.Err != nil {
		if errors.Is(msg.Err, context.Canceled) {
			return nil
		}
		p.filtered = nil
		p.pendingSelect = false
		return nil
	}
	p.filtered = msg.Matches
	p.cursor = 0
	p.scrollY = 0
	if p.pendingSelect {
		p.pendingSelect = false
		if len(p.filtered) > 0 {
			item := p.filtered[0].Item
			p.dismissed = true
			return func() tea.Msg { return PickerSelectMsg{Item: item} }
		}
	}
	return nil
}

func (p *Picker) cancelFilter() {
	if p.filterCancel != nil {
		p.filterCancel()
		p.filterCancel = nil
	}
	p.filterPending = false
	p.filterGeneration++
}

func (p *Picker) cancelItemsPreparation() {
	if p.itemsCancel != nil {
		p.itemsCancel()
		p.itemsCancel = nil
	}
	p.itemsPending = false
	p.itemsGeneration++
}

// Cancel releases all cancellable picker work. Owners that clear an overlay
// stack programmatically should call it before dropping the picker.
func (p *Picker) Cancel() {
	p.cancelItemsPreparation()
	p.cancelFilter()
	p.pendingSelect = false
}

func (p *Picker) pending() bool { return p.itemsPending || p.filterPending }

func filterItemsContext(ctx context.Context, items []PickerItem, query string) ([]PickerMatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	matches := make([]PickerMatch, 0, len(items))
	if query == "" {
		for i, item := range items {
			if i%256 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			matches = append(matches, PickerMatch{Item: item})
		}
		return matches, ctx.Err()
	}
	for i, item := range items {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		haystack := item.Label
		if item.Search != "" {
			haystack = item.Search
		}
		score, matched := FuzzyMatch(query, haystack)
		if !matched && item.Description != "" && item.Description != haystack {
			score, matched = FuzzyMatch(query, item.Description)
		}
		if !matched && item.Label != haystack {
			score, matched = FuzzyMatch(query, item.Label)
		}
		if matched {
			score += item.Recency * 20
			matches = append(matches, PickerMatch{Item: item, Score: score})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Item.Recency > matches[j].Item.Recency
	})
	return matches, nil
}

func (p *Picker) visibleCount() int {
	// title + blank + input + blank = 4 lines; border/padding ~4
	v := p.maxHeight - 8
	if v < 3 {
		v = 3
	}
	if v > 20 {
		v = 20
	}
	return v
}

func (p *Picker) visibleRange() (start, end int) {
	if p.pending() {
		return 0, 0
	}
	start = max(0, min(p.scrollY, len(p.filtered)))
	end = min(len(p.filtered), start+p.visibleCount())
	return start, end
}

func (p *Picker) ensureVisible() {
	visible := p.visibleCount()
	if p.cursor < p.scrollY {
		p.scrollY = p.cursor
	}
	if p.cursor >= p.scrollY+visible {
		p.scrollY = p.cursor - visible + 1
	}
}

func (p *Picker) itemZoneID(idx int) string {
	return p.zoneID + "-item-" + itoa(idx)
}

// boxZoneID identifies the zone covering the whole rendered picker box.
func (p *Picker) boxZoneID() string {
	return p.zoneID + "-box"
}

// dismiss cancels in-flight work and closes the picker, emitting the same
// message Escape produces. Escape and the outside-box click path must stay
// indistinguishable to owners.
func (p *Picker) dismiss() tea.Cmd {
	p.Cancel()
	p.pendingSelect = false
	p.dismissed = true
	if p.dismissAction != nil {
		action := p.dismissAction
		return func() tea.Msg { return action }
	}
	return func() tea.Msg { return PickerCloseMsg{} }
}

func countStr(cur, total int) string {
	return itoa(cur) + "/" + itoa(total)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func truncStr(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	return ansi.Truncate(s, maxLen, "...")
}
