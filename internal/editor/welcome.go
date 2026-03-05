package editor

import (
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// WelcomeTickMsg drives the welcome screen animation.
type WelcomeTickMsg struct{}

// Nord aurora colors for the color cycle animation
var auroraColors = []color.Color{
	ui.Nord7,  // teal
	ui.Nord8,  // cyan
	ui.Nord9,  // blue
	ui.Nord10, // deep blue
	ui.Nord15, // purple
	ui.Nord11, // red
	ui.Nord12, // orange
	ui.Nord13, // yellow
	ui.Nord14, // green
}

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

	subtitleStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
	keyStyle := lipgloss.NewStyle().Foreground(ui.Nord13).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
	hintStyle := lipgloss.NewStyle().Foreground(ui.Nord3)

	var lines []string

	// Logo with color cycling — each line gets a different aurora color offset
	for i, l := range logo {
		color := w.logoColor(i)
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		lines = append(lines, style.Render(l))
	}

	lines = append(lines, "")
	lines = append(lines, subtitleStyle.Render("A terminal code editor"))
	lines = append(lines, "")
	lines = append(lines, "")

	hints := []struct{ key, desc string }{
		{"Ctrl+B", "Toggle file tree"},
		{"Ctrl+F", "Find in file"},
		{"Ctrl+Q", "Quit"},
		{"F1", "Help"},
	}
	for _, h := range hints {
		lines = append(lines, keyStyle.Render(h.key)+"  "+descStyle.Render(h.desc))
	}

	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("Open a file from the tree to get started."))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Width(w.width).
		Height(w.height).
		Background(ui.Nord0).
		Render(content)
}

// logoColor returns the color for a logo line based on the current animation frame.
func (w *Welcome) logoColor(lineIdx int) color.Color {
	if w.settled {
		return ui.Nord8 // settled: static cyan
	}
	// Cycle through aurora colors; each line offset by 1 in the palette
	idx := (w.frame/4 + lineIdx) % len(auroraColors)
	return auroraColors[idx]
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
