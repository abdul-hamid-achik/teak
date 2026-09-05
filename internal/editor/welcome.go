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

	subtitleStyle := w.theme.Editor
	keyStyle := w.theme.HelpKey.Bold(true)
	descStyle := w.theme.Editor
	hintStyle := lipgloss.NewStyle().Foreground(w.theme.HelpBorder.GetForeground())

	var lines []string

	// Logo with color cycling — each line gets a different aurora color offset
	for i, l := range logo {
		lines = append(lines, w.logoStyle(i).Render(l))
	}

	lines = append(lines, "")
	lines = append(lines, subtitleStyle.Render("A terminal code editor"))
	lines = append(lines, "")
	lines = append(lines, "")

	hints := []struct{ key, desc string }{
		{"Ctrl+B", "Toggle file tree"},
		{"Ctrl+F", "Find in file"},
		{"Ctrl+P", "Quick Open"},
		{"Ctrl+Q", "Quit"},
		{"F1", "Help"},
	}
	for _, h := range hints {
		lines = append(lines, keyStyle.Render(h.key)+"  "+descStyle.Render(h.desc))
	}

	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("Open a file from the tree to get started."))

	content := strings.Join(lines, "\n")

	return w.theme.Editor.
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Width(w.width).
		Height(w.height).
		Render(content)
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
