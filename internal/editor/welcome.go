package editor

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// WelcomeTickMsg drives the welcome screen animation.
type WelcomeTickMsg struct{}

// Welcome renders a welcome screen with a smooth color-cycling logo.
type Welcome struct {
	Active  bool
	theme   ui.Theme
	width   int
	height  int
	frame   int
	settled bool
}

// NewWelcome creates a new welcome screen.
func NewWelcome(theme ui.Theme) Welcome {
	return Welcome{
		Active: true,
		theme:  theme,
	}
}

// SetTheme updates rendering without restarting the welcome animation.
func (w *Welcome) SetTheme(theme ui.Theme) {
	w.theme = theme
}

// Init returns the first animation tick command.
func (w *Welcome) Init() tea.Cmd {
	return tickWelcome()
}

// SetSize stores dimensions for centering.
func (w *Welcome) SetSize(width, height int) {
	w.width = width
	w.height = height
}

// Update processes animation ticks.
func (w *Welcome) Update(msg WelcomeTickMsg) (*Welcome, tea.Cmd) {
	if !w.Active || w.settled {
		return w, nil
	}

	w.frame++

	// Run the color cycle for ~3 seconds (180 frames at 60fps), then settle
	if w.frame >= 180 {
		w.settled = true
		return w, nil
	}

	return w, tickWelcome()
}

// View renders the welcome screen content.
func (w *Welcome) View() string {
	if !w.Active {
		return ""
	}

	logo := []string{
		"████████╗███████╗ █████╗ ██╗  ██╗",
		"╚══██╔══╝██╔════╝██╔══██╗██║ ██╔╝",
		"   ██║   █████╗  ███████║█████╔╝ ",
		"   ██║   ██╔══╝  ██╔══██║██╔═██╗ ",
		"   ██║   ███████╗██║  ██║██║  ██╗",
		"   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝",
	}

	editorBg := w.theme.Editor.GetBackground()
	subtitleStyle := w.theme.Editor
	keyStyle := w.theme.HelpKey.Background(editorBg).Bold(true)
	descStyle := w.theme.Editor
	hintStyle := w.theme.Muted.Background(editorBg)

	// Logo with color cycling — each line gets a different aurora color offset
	var header []string
	for i, l := range logo {
		header = append(header, w.logoStyle(i).Render(l))
	}
	header = append(header, "", subtitleStyle.Render("A terminal code editor"), "", "")

	// Every row has the same width, so the centered block keeps keys and
	// descriptions in two columns instead of centering each line alone.
	keyWidth, descWidth := 0, 0
	for _, h := range welcomeHints {
		keyWidth = max(keyWidth, lipgloss.Width(h.key))
		descWidth = max(descWidth, lipgloss.Width(h.desc))
	}
	hints := make([]string, 0, len(welcomeHints))
	for _, h := range welcomeHints {
		hints = append(hints, keyStyle.Render(padRight(h.key, keyWidth))+descStyle.Render("  "+padRight(h.desc, descWidth)))
	}
	footer := []string{"", hintStyle.Render("Open a file from the tree to get started.")}

	lines := w.fitRows(header, hints, footer)

	content := strings.Join(lines, "\n")

	return w.theme.Editor.
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Width(w.width).
		Height(w.height).
		Render(content)
}

// welcomeHints are the starters that work with no file open, most useful
// first: a short terminal drops rows from the end.
var welcomeHints = []struct{ key, desc string }{
	{"Ctrl+P", "Quick Open"},
	{"Ctrl+Shift+P", "Command Palette"},
	{"F1", "Help"},
	{"Ctrl+N", "New File"},
	{"Ctrl+Shift+F", "Find in Project"},
	{"Ctrl+B", "Toggle file tree"},
	{"Ctrl+Q", "Quit"},
}

// fitRows joins the welcome sections, dropping content that does not fit the
// height: the footer first, then trailing hints (never below the first three),
// then the logo, then the remaining hints.
func (w *Welcome) fitRows(header, hints, footer []string) []string {
	join := func(h, hs, f []string) []string {
		rows := append(append(append([]string{}, h...), hs...), f...)
		return rows
	}
	rows := join(header, hints, footer)
	if w.height <= 0 || len(rows) <= w.height {
		return rows
	}
	rows = join(header, hints, nil)
	for len(rows) > w.height && len(hints) > 3 {
		hints = hints[:len(hints)-1]
		rows = join(header, hints, nil)
	}
	if len(rows) > w.height {
		// Keep the tagline, drop the logo and its spacer.
		header = header[len(header)-3 : len(header)-2]
		rows = join(header, hints, nil)
	}
	for len(rows) > w.height && len(hints) > 0 {
		hints = hints[:len(hints)-1]
		rows = join(header, hints, nil)
	}
	return rows
}

// logoStyle returns a themed accent style for a logo line.
func (w *Welcome) logoStyle(lineIdx int) lipgloss.Style {
	styles := [...]lipgloss.Style{
		w.theme.HelpTitle,
		w.theme.PromptAccent,
		w.theme.DiagInfo,
		w.theme.HelpKey,
		w.theme.PromptDanger,
		w.theme.DiagWarning,
		w.theme.GitAdded,
		w.theme.GitModified,
	}
	if w.settled {
		return styles[0].Bold(true)
	}
	return styles[(w.frame/4+lineIdx)%len(styles)].Bold(true)
}

// Dismiss deactivates the welcome screen.
func (w *Welcome) Dismiss() {
	w.Active = false
}

func tickWelcome() tea.Cmd {
	return tea.Tick(time.Second/60, func(time.Time) tea.Msg {
		return WelcomeTickMsg{}
	})
}
