package overlay

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// PickerItem is a single selectable entry in the picker.
type PickerItem struct {
	Label       string // display text
	Description string // secondary text (shown dimmed)
	Value       any    // opaque payload returned on selection
}

// PickerSelectMsg is emitted when the user selects an item.
type PickerSelectMsg struct {
	Item PickerItem
}

// PickerCloseMsg is emitted when the user dismisses the picker.
type PickerCloseMsg struct{}

// Picker is a fuzzy-filterable list overlay with a text input, scrollable
// results, and keyboard/mouse navigation. It implements the Overlay interface.
type Picker struct {
	input     textinput.Model
	items     []PickerItem
	filtered  []scoredItem
	cursor    int
	scrollY   int
	theme     ui.Theme
	width     int
	maxHeight int
	dismissed bool
	title     string
	zoneID    string // unique prefix for mouse zones
}

type scoredItem struct {
	item  PickerItem
	score int
}

// NewPicker creates a picker overlay.
// zoneID should be unique per picker instance to avoid zone collisions.
func NewPicker(title string, items []PickerItem, theme ui.Theme, zoneID string) *Picker {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.CharLimit = 128

	p := &Picker{
		input:     ti,
		items:     items,
		theme:     theme,
		width:     60,
		maxHeight: 20,
		title:     title,
		zoneID:    zoneID,
	}
	p.refilter()
	return p
}

// Focus gives keyboard focus to the text input.
func (p *Picker) Focus() tea.Cmd {
	return p.input.Focus()
}

// SetSize updates the available dimensions.
func (p *Picker) SetSize(w, h int) {
	p.width = w
	p.maxHeight = h
	p.input.SetWidth(min(w-8, 50))
}

// SetItems replaces the item list and refilters.
func (p *Picker) SetItems(items []PickerItem) {
	p.items = items
	p.refilter()
}

// Update implements Overlay.
func (p *Picker) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			p.dismissed = true
			return p, func() tea.Msg { return PickerCloseMsg{} }
		case "enter":
			if len(p.filtered) > 0 && p.cursor < len(p.filtered) {
				item := p.filtered[p.cursor].item
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
		if mouse.Button == tea.MouseLeft {
			for i := range p.filtered {
				if zone.Get(p.itemZoneID(i)).InBounds(msg) {
					item := p.filtered[i].item
					p.dismissed = true
					return p, func() tea.Msg { return PickerSelectMsg{Item: item} }
				}
			}
		}
		return p, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			p.scrollY -= 3
			if p.scrollY < 0 {
				p.scrollY = 0
			}
		} else if mouse.Button == tea.MouseWheelDown {
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
	}

	// Forward to text input
	prevVal := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prevVal {
		p.refilter()
	}
	return p, cmd
}

// View implements Overlay.
func (p *Picker) View() string {
	boxWidth := p.width
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 80 {
		boxWidth = 80
	}
	contentWidth := boxWidth - 6 // border + padding

	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	sb.WriteString(titleStyle.Render(p.title))
	sb.WriteString("\n\n")

	// Input
	sb.WriteString(p.input.View())
	sb.WriteByte('\n')

	// Results
	visible := p.visibleCount()
	endIdx := p.scrollY + visible
	if endIdx > len(p.filtered) {
		endIdx = len(p.filtered)
	}

	itemStyle := lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	cursorStyle := lipgloss.NewStyle().
		Background(ui.Nord2).
		Foreground(ui.Nord6)
	descStyle := lipgloss.NewStyle().
		Foreground(ui.Nord3)

	for i := p.scrollY; i < endIdx; i++ {
		si := p.filtered[i]
		label := truncStr(si.item.Label, contentWidth)
		if si.item.Description != "" {
			descWidth := contentWidth - len(label) - 2
			if descWidth > 4 {
				label += "  " + descStyle.Render(truncStr(si.item.Description, descWidth))
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

	if len(p.filtered) == 0 {
		sb.WriteByte('\n')
		noMatch := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(noMatch.Render("  No matches"))
	}

	// Scroll hint
	if len(p.filtered) > visible {
		sb.WriteByte('\n')
		hint := lipgloss.NewStyle().Foreground(ui.Nord3)
		sb.WriteString(hint.Render(strings.Repeat(" ", contentWidth-10) + countStr(p.cursor+1, len(p.filtered))))
	}

	content := sb.String()
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2).
		Width(boxWidth)

	return boxStyle.Render(content)
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
	return len(p.filtered)
}

// Cursor returns the current cursor position.
func (p *Picker) Cursor() int {
	return p.cursor
}

// Query returns the current input value.
func (p *Picker) Query() string {
	return p.input.Value()
}

func (p *Picker) refilter() {
	query := p.input.Value()
	p.filtered = nil

	if query == "" {
		for _, item := range p.items {
			p.filtered = append(p.filtered, scoredItem{item: item, score: 0})
		}
	} else {
		for _, item := range p.items {
			score, matched := FuzzyMatch(query, item.Label)
			if matched {
				p.filtered = append(p.filtered, scoredItem{item: item, score: score})
			}
		}
		sort.Slice(p.filtered, func(i, j int) bool {
			return p.filtered[i].score > p.filtered[j].score
		})
	}

	p.cursor = 0
	p.scrollY = 0
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
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
