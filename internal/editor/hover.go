package editor

import (
	"strings"

	"teak/internal/ui"
)

// Hover manages the hover popup state.
type Hover struct {
	Content string
	Visible bool
	theme   ui.Theme
}

// NewHover creates a new hover popup.
func NewHover(theme ui.Theme) Hover {
	return Hover{theme: theme}
}

// Show displays hover content.
func (h *Hover) Show(content string) {
	h.Content = content
	h.Visible = content != ""
}

// Hide dismisses the hover popup.
func (h *Hover) Hide() {
	h.Visible = false
	h.Content = ""
}

// View renders the hover popup.
func (h Hover) View() string {
	if !h.Visible || h.Content == "" {
		return ""
	}

	// Limit width and wrap lines
	maxWidth := 60
	lines := strings.Split(h.Content, "\n")
	var wrapped []string
	for _, line := range lines {
		if len(line) > maxWidth {
			line = line[:maxWidth-3] + "..."
		}
		wrapped = append(wrapped, line)
	}
	if len(wrapped) > 10 {
		wrapped = wrapped[:10]
		wrapped = append(wrapped, "...")
	}

	content := strings.Join(wrapped, "\n")
	return h.theme.HoverBox.Render(content)
}
