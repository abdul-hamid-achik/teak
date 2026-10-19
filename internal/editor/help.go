package editor

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
			{"Ctrl+Shift+S", "Save as"},
			{"Ctrl+N", "New file"},
			{"Ctrl+P", "Quick open"},
			{"Ctrl+Shift+P", "Command palette"},
			{"F1", "Toggle help"},
			{"Ctrl+,", "Open settings"},
		},
	},
	{
		title: "Navigation",
		bindings: []keybinding{
			{"Arrows", "Move cursor"},
			{"Ctrl+Left/Right", "Word jump"},
			{"Home/End", "Smart line start/end"},
			{"Ctrl+Home/End", "Doc start/end"},
			{"PgUp/PgDn", "Page up/down"},
			{"Alt+Z", "Toggle word wrap"},
			{"Ctrl+G", "Go to line (or line:col)"},
		},
	},
	{
		title: "Selection",
		bindings: []keybinding{
			{"Shift+Arrows", "Select characters"},
			{"Ctrl+Shift+Left/Right", "Select words"},
			{"Shift+Home/End", "Select to line edge"},
			{"Ctrl+Shift+Home/End", "Select to document start/end"},
			{"Ctrl+Alt+Up/Down", "Add cursor above / below"},
			{"Ctrl+A", "Select all"},
			{"Ctrl+D", "Select next occurrence"},
			{"Ctrl+U", "Undo last cursor"},
			{"Ctrl+Shift+L", "Select all occurrences"},
			{"Ctrl+L", "Select current line"},
			{"Shift+Alt+I", "Split selection into lines"},
			{"Double-click", "Select word"},
			{"Click+Drag", "Select with mouse"},
			{"Shift+Click", "Extend selection"},
		},
	},
	{
		title: "Clipboard",
		bindings: []keybinding{
			{"Ctrl+C", "Copy (line if no selection)"},
			{"Ctrl+X", "Cut (line if no selection)"},
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
			{"Alt+Shift+Up/Down", "Duplicate line"},
			{"Ctrl+Shift+K", "Delete line"},
			{"Ctrl+Bksp/Del", "Delete word"},
			{"Enter", "New line (auto-indent)"},
			{"Ctrl+Z", "Undo"},
			{"Ctrl+Y / Ctrl+Shift+Z", "Redo"},
		},
	},
	{
		title: "Search",
		bindings: []keybinding{
			{"Ctrl+F", "Find in file"},
			{"Ctrl+H", "Replace in project"},
			{"Ctrl+Shift+F", "Find in project"},
			{"Tab in search", "Toggle text / semantic search"},
			{"F3 / Shift+F3", "Next / previous result"},
			{"Ctrl+R (find widget)", "Toggle regex search"},
			{"Alt+C (find widget)", "Toggle case-sensitive find"},
			{"Alt+W (find widget)", "Toggle whole-word find"},
		},
	},
	{
		title: "LSP",
		bindings: []keybinding{
			{"Ctrl+Space", "Autocomplete"},
			{"Alt+K", "Show hover"},
			{"Ctrl+.", "Code actions"},
			{"F12", "Go to definition"},
			{"Shift+F12", "Find references"},
			{"Alt+Left / Ctrl+-", "Go back"},
			{"Alt+Right", "Go forward"},
			{"F2", "Rename symbol"},
			{"Ctrl+Shift+O", "Document symbols"},
			{"Shift+Alt+F / Ctrl+Alt+F", "Format document"},
		},
	},
	{
		title: "Code Folding",
		bindings: []keybinding{
			{"Ctrl+Shift+[", "Fold current region"},
			{"Ctrl+Shift+]", "Unfold current region"},
			{"Ctrl+Shift+0", "Fold all regions"},
			{"Ctrl+Shift+J", "Unfold all regions"},
		},
	},
	{
		title: "Panels",
		bindings: []keybinding{
			{"Ctrl+B", "Toggle file tree"},
			{"/ (file tree)", "Filter project files"},
			{"Ctrl+. / Alt+H (file tree)", "Toggle hidden files"},
			{"Ctrl+Shift+. / Alt+I (file tree)", "Toggle ignored files"},
			{"Ctrl+\\", "Toggle editor split"},
			{"Ctrl+Shift+\\", "Close editor split"},
			{"F6", "Switch split pane focus"},
			{"Ctrl+Shift+G", "Show git panel"},
			{"Ctrl+Tab", "Last used tab"},
			{"Ctrl+PageDown / Ctrl+PageUp", "Next / previous tab"},
			{"Ctrl+Shift+Tab", "Previous tab"},
			{"Tab (file tree focus)", "Switch sidebar panels"},
			{"Ctrl+J", "Toggle agent panel"},
			{"Ctrl+'", "Focus agent panel"},
			{"Home / End (agent input)", "Move prompt cursor"},
			{"Ctrl+Home / Ctrl+End (agent)", "First / latest chat message"},
			{"Ctrl+` / Ctrl+~ / Alt+T", "Toggle terminal"},
			{"Shift+PageUp / Shift+PageDown (terminal)", "Scroll shell history"},
		},
	},
	{
		title: "Tabs",
		bindings: []keybinding{
			{"Ctrl+W", "Close tab"},
			{"Ctrl+Shift+T", "Reopen closed tab"},
		},
	},
	{
		title: "Debugging",
		bindings: []keybinding{
			{"F5", "Start debugging"},
			{"Shift+F5", "Stop debugging"},
			{"F9", "Toggle breakpoint"},
			{"C", "Continue"},
			{"N", "Step over"},
			{"I", "Step in"},
			{"O", "Step out"},
			{"Q", "Stop (in debugger)"},
		},
	},
	{
		title: "Problems Panel",
		bindings: []keybinding{
			{"F8", "Next problem"},
			{"Shift+F8", "Previous problem"},
			{"Up/Down (Problems focus)", "Navigate problems"},
			{"Enter (Problems focus)", "Go to problem"},
		},
	},
}

// HelpModel is the interactive help overlay with search and scroll.
type HelpModel struct {
	input    textinput.Model
	scrollY  int
	height   int
	width    int
	theme    ui.Theme
	lines    []helpLine // all rendered lines
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
	applyPanelInputTheme(&ti, theme)

	m := HelpModel{
		input: ti,
		theme: theme,
		width: 80,
	}
	m.lines = m.buildLines()
	m.filtered = m.lines
	return m
}

// SetTheme updates pre-rendered help lines without disturbing the filter or scroll.
func (m *HelpModel) SetTheme(theme ui.Theme) {
	m.theme = theme
	applyPanelInputTheme(&m.input, theme)
	m.rebuildLines()
}

func (m *HelpModel) rebuildLines() {
	m.lines = m.buildLines()
	query := strings.ToLower(m.input.Value())
	if query == "" {
		m.filtered = m.lines
	} else {
		m.filtered = m.filterLines(query)
	}
	m.scrollY = min(m.scrollY, m.maxScroll())
}

// applyPanelInputTheme themes the filter input and gives each of its styles the
// panel background, so the input row does not paint editor-colored patches
// inside the box.
func applyPanelInputTheme(input *textinput.Model, theme ui.Theme) {
	ui.ApplyTextInputTheme(input, theme)
	bg := theme.Panel.GetBackground()
	styles := input.Styles()
	for _, state := range []*textinput.StyleState{&styles.Focused, &styles.Blurred} {
		state.Text = state.Text.Background(bg)
		state.Placeholder = state.Placeholder.Background(bg)
		state.Suggestion = state.Suggestion.Background(bg)
		state.Prompt = state.Prompt.Background(bg)
	}
	input.SetStyles(styles)
}

// Focus focuses the search input.
func (m *HelpModel) Focus() tea.Cmd {
	return m.input.Focus()
}

// SetSize sets the overlay dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = max(1, w)
	m.height = max(1, h)
	m.input.SetWidth(max(1, min(m.width-12, 36)))
	m.rebuildLines()
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
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
		case tea.MouseWheelDown:
			m.scrollY += 3
			if m.scrollY > m.maxScroll() {
				m.scrollY = m.maxScroll()
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		// The overlay is entirely keyboard navigable, but a click should also
		// make the filter the active text target rather than becoming a dead
		// gesture or leaking focus to the editor behind the modal.
		return m, m.input.Focus()
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

	var sb strings.Builder
	bg := m.theme.Panel.GetBackground()

	// Title
	sb.WriteString(m.theme.HelpTitle.Background(bg).Render("Keyboard Shortcuts"))
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

	if len(m.filtered) == 0 {
		sb.WriteString(m.theme.Muted.Background(bg).Render("No matching shortcuts"))
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
		sb.WriteString(m.theme.Muted.Background(bg).Render("  Use arrows or scroll to navigate"))
	}

	content := sb.String()

	// Panel supplies the background for padding and any cell an inner style
	// leaves untouched; every inner segment carries the same background so no
	// reset in the middle of a row exposes the editor behind the box.
	helpStyle := m.theme.Panel.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.HelpBorder.GetForeground()).
		Padding(1, 2)

	return helpStyle.Width(m.boxWidth()).Render(content)
}

const (
	helpRowIndent  = 2  // cells before the key column
	helpKeyGap     = 2  // minimum cells between the key and description columns
	helpMaxKeyCol  = 28 // a pathological key wraps instead of eating the description
	helpMinDescCol = 16 // description cells the key column must leave free
	helpMaxWidth   = 80 // widest the overlay grows on large terminals
)

func (m HelpModel) buildLines() []helpLine {
	return m.renderGroups(helpGroups)
}

func (m HelpModel) filterLines(query string) []helpLine {
	var groups []bindingGroup
	for _, group := range helpGroups {
		groupMatches := strings.Contains(strings.ToLower(group.title), query)
		var matching []keybinding
		for _, b := range group.bindings {
			text := strings.ToLower(b.key + " " + b.desc)
			if groupMatches || strings.Contains(text, query) {
				matching = append(matching, b)
			}
		}
		if len(matching) > 0 {
			groups = append(groups, bindingGroup{title: group.title, bindings: matching})
		}
	}
	return m.renderGroups(groups)
}

// renderGroups lays the groups out as physical rows. The key column is sized
// to the longest key shown (capped), and both columns wrap with a hanging
// indent so a continuation row stays under its own column. Every segment
// carries the panel background.
func (m HelpModel) renderGroups(groups []bindingGroup) []helpLine {
	width := m.contentWidth()
	bg := m.theme.Panel.GetBackground()
	panel := m.theme.Panel
	titleStyle := m.theme.HelpTitle.Background(bg)
	keyStyle := m.theme.HelpKey.Background(bg)

	keyCol := 0
	for _, group := range groups {
		for _, b := range group.bindings {
			keyCol = max(keyCol, lipgloss.Width(b.key))
		}
	}
	keyCap := max(1, min(helpMaxKeyCol, max(8, width-helpRowIndent-helpKeyGap-helpMinDescCol)))
	keyCol = max(1, min(keyCol, keyCap))
	descCol := max(1, width-helpRowIndent-keyCol-helpKeyGap)

	indent := panel.Render(strings.Repeat(" ", helpRowIndent))
	gap := panel.Render(strings.Repeat(" ", helpKeyGap))

	var rows []helpLine
	for i, group := range groups {
		if i > 0 {
			rows = append(rows, helpLine{rendered: "", text: ""})
		}
		rows = append(rows, helpLine{
			rendered: titleStyle.Render(ansi.Truncate(group.title, width, "")),
			text:     strings.ToLower(group.title),
			isTitle:  true,
		})
		for _, b := range group.bindings {
			keyLines := wrapCell(b.key, keyCol)
			descLines := wrapCell(b.desc, descCol)
			text := strings.ToLower(b.key + " " + b.desc)
			for r := 0; r < max(len(keyLines), len(descLines)); r++ {
				var k, d string
				if r < len(keyLines) {
					k = keyLines[r]
				}
				if r < len(descLines) {
					d = descLines[r]
				}
				row := indent + keyStyle.Render(padRight(k, keyCol)) + gap + panel.Render(padRight(d, descCol))
				rows = append(rows, helpLine{rendered: ansi.Truncate(row, width, ""), text: text})
			}
		}
	}
	return rows
}

// wrapCell wraps plain text to width cells, breaking at spaces and splitting
// words longer than the column.
func wrapCell(s string, width int) []string {
	return strings.Split(ansi.Wrap(s, max(1, width), ""), "\n")
}

func (m HelpModel) boxWidth() int { return max(1, min(helpMaxWidth, m.width-4)) }

// contentWidth is the cell width available inside the border and padding.
func (m HelpModel) contentWidth() int { return max(1, m.boxWidth()-6) }

func (m HelpModel) visibleLines() int {
	// Account for title (1) + blank (1) + input (1) + blank (1) + scroll hint (1) + border/padding (~4)
	v := m.height - 10
	if v < 1 {
		v = 1
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
	if lipgloss.Width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}
