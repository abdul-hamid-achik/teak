package overlay

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// panelBackground is the background every cell inside a floating panel must
// carry. Themes built before the Panel role existed fall back to the help
// border background, which Panel mirrors.
func panelBackground(theme ui.Theme) color.Color {
	if bg := theme.Panel.GetBackground(); !isNoColor(bg) {
		return bg
	}
	return theme.HelpBorder.GetBackground()
}

func isNoColor(c color.Color) bool {
	if c == nil {
		return true
	}
	_, none := c.(lipgloss.NoColor)
	return none
}

// keepPanelBackground makes content carry bg after every SGR reset.
//
// Inner lipgloss styles close with a reset, and anything after that reset (a
// gap between buttons, the remainder of a prompt line, plugin-supplied ANSI
// text) would fall back to the terminal default and show up as a dark chip
// inside the panel. Re-asserting the background after each reset keeps those
// cells on the panel without every caller having to style its separators.
func keepPanelBackground(content string, bg color.Color) string {
	if isNoColor(bg) {
		return content
	}
	set := ansi.NewStyle().BackgroundColor(bg).String()
	if set == "" {
		return content
	}
	content = strings.NewReplacer(
		"\x1b[m", "\x1b[m"+set,
		"\x1b[0m", "\x1b[0m"+set,
	).Replace(content)
	return set + content
}

// fitWidth truncates or pads (with unstyled spaces, which keepPanelBackground
// later paints) s to exactly width cells.
func fitWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	if w := lipgloss.Width(s); w > width {
		return ansi.Truncate(s, width, "")
	} else if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
