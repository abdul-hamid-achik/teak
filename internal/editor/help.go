package editor

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

type keybinding struct {
	key  string
	desc string
}

type bindingGroup struct {
	title    string
	bindings []keybinding
}

var helpGroups = []bindingGroup{
	{
		title: "General",
		bindings: []keybinding{
			{"Ctrl+Q", "Quit"},
			{"Ctrl+S", "Save file"},
			{"F1", "Toggle help"},
		},
	},
	{
		title: "Navigation",
		bindings: []keybinding{
			{"Arrows", "Move cursor"},
			{"Ctrl+Left/Right", "Word jump"},
			{"Home/End", "Line start/end"},
			{"Ctrl+Home/End", "Doc start/end"},
			{"PgUp/PgDn", "Page up/down"},
			{"Ctrl+G", "Go to line"},
		},
	},
	{
		title: "Selection",
		bindings: []keybinding{
			{"Shift+Arrows", "Select characters"},
			{"Ctrl+Shift+L/R", "Select words"},
			{"Shift+Home/End", "Select to line edge"},
			{"Ctrl+A", "Select all"},
			{"Ctrl+D", "Select next occurrence"},
			{"Double-click", "Select word"},
			{"Click+Drag", "Select with mouse"},
			{"Shift+Click", "Extend selection"},
		},
	},
	{
		title: "Clipboard",
		bindings: []keybinding{
			{"Ctrl+C", "Copy"},
			{"Ctrl+X", "Cut"},
			{"Ctrl+V", "Paste"},
		},
	},
	{
		title: "Editing",
		bindings: []keybinding{
			{"Tab", "Indent"},
			{"Shift+Tab", "Dedent"},
			{"Ctrl+]", "Indent block"},
			{"Ctrl+/", "Toggle comment"},
			{"Alt+Up/Down", "Move line"},
			{"Alt+Shift+U/D", "Duplicate line"},
			{"Ctrl+Shift+K", "Delete line"},
			{"Ctrl+Bksp/Del", "Delete word"},
			{"Enter", "New line (auto-indent)"},
			{"Ctrl+Z", "Undo"},
			{"Ctrl+Y", "Redo"},
		},
	},
	{
		title: "Search",
		bindings: []keybinding{
			{"Ctrl+F", "Text search"},
			{"Ctrl+Shift+F", "Semantic search"},
		},
	},
	{
		title: "LSP",
		bindings: []keybinding{
			{"Ctrl+Space", "Autocomplete"},
			{"F12", "Go to definition"},
		},
	},
	{
		title: "Panels",
		bindings: []keybinding{
			{"Ctrl+B", "Toggle file tree"},
			{"Ctrl+Shift+G", "Toggle git panel"},
			{"Ctrl+Tab", "Next tab"},
			{"Ctrl+Shift+Tab", "Previous tab"},
		},
	},
}

// HelpModel is the interactive help overlay with search and scroll.
type HelpModel struct {
	input   textinput.Model
	scrollY int
	height  int
	width   int
	theme   ui.Theme
	lines   []helpLine // all rendered lines
	filtered []helpLine // filtered by search
}

type helpLine struct {
	rendered string // pre-rendered line for display
	text     string // plain text for search matching
	isTitle  bool
}

// NewHelpModel creates a new help overlay model.
func NewHelpModel(theme ui.Theme) HelpModel {
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.CharLimit = 64
	ti.SetWidth(36)

	m := HelpModel{
		input: ti,
		theme: theme,
	}
	m.lines = m.buildLines()
	m.filtered = m.lines
	return m
}

// Focus focuses the search input.
func (m *HelpModel) Focus() tea.Cmd {
	return m.input.Focus()
}

// SetSize sets the overlay dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.input.SetWidth(min(w-12, 36))
}

// Update handles input for the help overlay.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape", "f1":
			return m, nil // caller checks for close
		case "up":
			if m.scrollY > 0 {
				m.scrollY--
			}
			return m, nil
		case "down":
			maxScroll := m.maxScroll()
			if m.scrollY < maxScroll {
				m.scrollY++
			}
			return m, nil
		case "pgup":
			visible := m.visibleLines()
			m.scrollY -= visible
			if m.scrollY < 0 {
				m.scrollY = 0
			}
			return m, nil
		case "pgdown":
			visible := m.visibleLines()
			m.scrollY += visible
			if m.scrollY > m.maxScroll() {
				m.scrollY = m.maxScroll()
			}
			return m, nil
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
		} else if mouse.Button == tea.MouseWheelDown {
			m.scrollY += 3
			if m.scrollY > m.maxScroll() {
				m.scrollY = m.maxScroll()
			}
		}
		return m, nil
	}

	// Forward to text input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Re-filter on query change
	query := strings.ToLower(m.input.Value())
	if query == "" {
		m.filtered = m.lines
	} else {
		m.filtered = m.filterLines(query)
	}
	// Reset scroll when filter changes
	if m.scrollY > m.maxScroll() {
		m.scrollY = m.maxScroll()
	}

	return m, cmd
}

// View renders the help overlay.
func (m HelpModel) View() string {
	boxWidth := 48
	if boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}

	var sb strings.Builder

	// Title
	sb.WriteString(m.theme.HelpTitle.Render("Keyboard Shortcuts"))
	sb.WriteString("\n\n")

	// Search input
	sb.WriteString(m.input.View())
	sb.WriteString("\n\n")

	// Scrollable content
	visible := m.visibleLines()
	endIdx := m.scrollY + visible
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
	}

	for i := m.scrollY; i < endIdx; i++ {
		sb.WriteString(m.filtered[i].rendered)
		if i < endIdx-1 {
			sb.WriteByte('\n')
		}
	}

	// Scroll indicator
	if len(m.filtered) > visible {
		sb.WriteByte('\n')
		pct := 0
		if m.maxScroll() > 0 {
			pct = m.scrollY * 100 / m.maxScroll()
		}
		indicator := lipgloss.NewStyle().Foreground(ui.Nord3).Render(
			padRight(
				"  "+strings.Repeat("^", min(1, m.scrollY))+" Scroll "+
					strings.Repeat("v", min(1, m.maxScroll()-m.scrollY)),
				20,
			) + padRight("", 10) + padRight(string(rune('0'+pct/100%10))+string(rune('0'+pct/10%10))+string(rune('0'+pct%10))+"%", 5),
		)
		_ = indicator
		scrollHint := lipgloss.NewStyle().Foreground(ui.Nord3).Render("  Use arrows or scroll to navigate")
		sb.WriteString(scrollHint)
	}

	content := sb.String()

	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2)

	return helpStyle.Width(boxWidth).Render(content)
}

func (m HelpModel) buildLines() []helpLine {
	var lines []helpLine
	for i, group := range helpGroups {
		if i > 0 {
			lines = append(lines, helpLine{rendered: "", text: ""})
		}
		lines = append(lines, helpLine{
			rendered: m.theme.HelpTitle.Render(group.title),
			text:     strings.ToLower(group.title),
			isTitle:  true,
		})
		for _, b := range group.bindings {
			keyStr := m.theme.HelpKey.Render(padRight(b.key, 16))
			rendered := "  " + keyStr + " " + b.desc
			text := strings.ToLower(b.key + " " + b.desc)
			lines = append(lines, helpLine{rendered: rendered, text: text})
		}
	}
	return lines
}

func (m HelpModel) filterLines(query string) []helpLine {
	// Find which groups have matching bindings
	var result []helpLine
	for _, group := range helpGroups {
		var matching []helpLine
		groupMatches := strings.Contains(strings.ToLower(group.title), query)
		for _, b := range group.bindings {
			text := strings.ToLower(b.key + " " + b.desc)
			if groupMatches || strings.Contains(text, query) {
				keyStr := m.theme.HelpKey.Render(padRight(b.key, 16))
				matching = append(matching, helpLine{
					rendered: "  " + keyStr + " " + b.desc,
					text:     text,
				})
			}
		}
		if len(matching) > 0 {
			if len(result) > 0 {
				result = append(result, helpLine{rendered: "", text: ""})
			}
			result = append(result, helpLine{
				rendered: m.theme.HelpTitle.Render(group.title),
				text:     strings.ToLower(group.title),
				isTitle:  true,
			})
			result = append(result, matching...)
		}
	}
	return result
}

func (m HelpModel) visibleLines() int {
	// Account for title (1) + blank (1) + input (1) + blank (1) + scroll hint (1) + border/padding (~4)
	v := m.height - 10
	if v < 5 {
		v = 5
	}
	return v
}

func (m HelpModel) maxScroll() int {
	ms := len(m.filtered) - m.visibleLines()
	if ms < 0 {
		return 0
	}
	return ms
}

// RenderHelp is kept for backward compatibility but now unused.
func RenderHelp(theme ui.Theme, width, height int) string {
	h := NewHelpModel(theme)
	h.SetSize(width, height)
	return h.View()
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
