package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// FloatCloseMsg is emitted when a floating panel is dismissed with Escape.
type FloatCloseMsg struct {
	ID int
}

// Float is a bounded, read-only floating panel. It intentionally has no
// embedded editor or arbitrary widget tree; plugins can use it for status,
// previews, and diagnostics without adding a second event loop.
type Float struct {
	ID        int
	Title     string
	Content   string
	Width     int
	Height    int
	dismissed bool
}

// NewFloat creates a read-only floating panel.
func NewFloat(id int, title, content string, width, height int) *Float {
	return &Float{ID: id, Title: title, Content: content, Width: width, Height: height}
}

// Update implements Overlay.
func (f *Float) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, nil
	}
	switch key.String() {
	case "esc", "escape", "enter":
		f.dismissed = true
		id := f.ID
		return f, func() tea.Msg { return FloatCloseMsg{ID: id} }
	}
	return f, nil
}

// View implements Overlay.
func (f *Float) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	contentStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
	hintStyle := lipgloss.NewStyle().Foreground(ui.Nord3)
	content := f.Content
	if content == "" {
		content = "(empty)"
	}
	lines := strings.Split(content, "\n")
	if len(lines) > f.Height {
		lines = lines[:f.Height]
	}
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(f.Title))
	sb.WriteString("\n\n")
	sb.WriteString(contentStyle.Render(strings.Join(lines, "\n")))
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("Enter or Esc to close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2).
		Width(max(1, f.Width)).
		Height(max(1, f.Height+6)).
		Render(sb.String())
}

// IsDismissed implements Overlay.
func (f *Float) IsDismissed() bool {
	return f.dismissed
}

// CapturesInput implements Overlay.
func (f *Float) CapturesInput() bool {
	return true
}
