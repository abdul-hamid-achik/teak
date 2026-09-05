package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	ID                        int
	Title                     string
	Content                   string
	Width                     int
	Height                    int
	screenWidth, screenHeight int
	dismissed                 bool
	theme                     ui.Theme
}

// NewFloat creates a read-only floating panel.
func NewFloat(id int, title, content string, width, height int) *Float {
	return &Float{ID: id, Title: title, Content: content, Width: width, Height: height, theme: ui.DefaultTheme()}
}

// SetTheme updates rendering without resetting the panel content.
func (f *Float) SetTheme(theme ui.Theme) { f.theme = theme }

// Update implements Overlay.
func (f *Float) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		f.screenWidth, f.screenHeight = size.Width, size.Height
		return f, nil
	}

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
	titleStyle := f.theme.HelpTitle
	contentStyle := lipgloss.NewStyle().Foreground(f.theme.TreeEntry.GetForeground())
	hintStyle := lipgloss.NewStyle().Foreground(f.theme.Gutter.GetForeground())
	width, height := max(1, f.Width), max(1, f.Height+6)
	if f.screenWidth > 0 {
		width = min(width, max(1, f.screenWidth-4))
	}
	if f.screenHeight > 0 {
		height = min(height, max(1, f.screenHeight-4))
	}
	content := f.Content
	if content == "" {
		content = "(empty)"
	}
	lines := strings.Split(content, "\n")
	if limit := max(1, height-8); len(lines) > limit {
		lines = lines[:limit]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, width-6), "…")
	}
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(f.Title))
	sb.WriteString("\n\n")
	sb.WriteString(contentStyle.Render(strings.Join(lines, "\n")))
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("Enter or Esc to close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(f.theme.HelpBorder.GetForeground()).
		Background(f.theme.HelpBorder.GetBackground()).
		Padding(1, 2).
		Width(width).
		Height(height).
		MaxHeight(height).
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
