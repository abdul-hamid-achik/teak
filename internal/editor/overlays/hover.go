package overlays

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
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

	// Wrap long lines instead of hard-truncating them: signatures and doc
	// text are useless when half of them are replaced by an ellipsis. The
	// popup stays bounded so a giant payload cannot cover the screen.
	const maxWidth = 60
	const maxLines = 20
	lines := strings.Split(h.Content, "\n")
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapANSILine(line, maxWidth)...)
		if len(wrapped) > maxLines {
			break
		}
	}
	if len(wrapped) > maxLines {
		wrapped = wrapped[:maxLines]
		wrapped = append(wrapped, "…")
	}
	return h.theme.HoverBox.Render(strings.Join(wrapped, "\n"))
}

// wrapANSILine wraps a styled line at maxWidth display columns, breaking on
// spaces where possible. Words longer than the limit are truncated once.
func wrapANSILine(line string, maxWidth int) []string {
	if ansi.StringWidth(line) <= maxWidth {
		return []string{line}
	}
	var out []string
	var current strings.Builder
	width := 0
	for _, word := range strings.Split(line, " ") {
		w := ansi.StringWidth(word)
		if w > maxWidth {
			if current.Len() > 0 {
				out = append(out, current.String())
				current.Reset()
				width = 0
			}
			out = append(out, ansi.Truncate(word, maxWidth, ""))
			continue
		}
		if width == 0 {
			current.WriteString(word)
			width = w
			continue
		}
		if width+1+w > maxWidth {
			out = append(out, current.String())
			current.Reset()
			current.WriteString(word)
			width = w
			continue
		}
		current.WriteString(" ")
		current.WriteString(word)
		width += 1 + w
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}
