package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// PlaceOverlayAt composites overlay content at a specific (x, y) position over base content.
// It keeps overlay ANSI sequences intact so BubbleZone markers survive until
// the root view scans them. Base hit markers are suppressed only on rows that
// an overlay replaces, preventing stale click targets behind the overlay.
// Lipgloss Canvas normalizes unknown CSI sequences, which otherwise drops
// overlay markers during composition.
func PlaceOverlayAt(base, overlay string, x, y, baseWidth, baseHeight int) string {
	if baseWidth <= 0 || baseHeight <= 0 {
		return ""
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	lines := make([]string, baseHeight)
	for row := range lines {
		line := fitLine(lineAt(baseLines, row), baseWidth)
		if overlayRow := row - y; overlayRow >= 0 && overlayRow < len(overlayLines) {
			line = overlayLine(line, overlayLines[overlayRow], x, baseWidth)
		}
		lines[row] = fitLine(line, baseWidth)
	}
	return strings.Join(lines, "\n")
}

func lineAt(lines []string, row int) string {
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

func fitLine(line string, width int) string {
	line = ansi.Cut(line, 0, width)
	if padding := width - ansi.StringWidth(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func overlayLine(base, overlay string, x, width int) string {
	start := x
	if start < 0 {
		overlay = ansi.Cut(overlay, -start, ansi.StringWidth(overlay))
		start = 0
	}
	if start >= width || overlay == "" {
		return base
	}

	overlay = ansi.Cut(overlay, 0, width-start)
	overlayWidth := ansi.StringWidth(overlay)
	if overlayWidth == 0 {
		return base
	}

	// A base hit zone on a row replaced by an overlay would either remain
	// clickable behind the overlay or be duplicated by the two independent
	// ansi.Cut calls below. Overlay rows are modal in the root view, so discard
	// their stale base markers while preserving ordinary ANSI styling.
	base = stripBubbleZoneMarkers(base)
	return ansi.Cut(base, 0, start) + overlay + ansi.Cut(base, start+overlayWidth, width)
}

func stripBubbleZoneMarkers(s string) string {
	var out strings.Builder
	out.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+2 < len(s) && s[i+1] == '[' {
			end := i + 2
			for end < len(s) && s[end] >= '0' && s[end] <= '9' {
				end++
			}
			if end > i+2 && end < len(s) && s[end] == 'z' {
				i = end + 1
				continue
			}
		}
		out.WriteByte(s[i])
		i++
	}

	return out.String()
}

// RenderOverlay composites overlay content centered over base content.
func RenderOverlay(base, overlay string, width, height int) string {
	// Calculate center position for the overlay
	overlayLines := strings.Split(overlay, "\n")
	overlayW := lipgloss.Width(overlay)
	overlayH := len(overlayLines)

	x := (width - overlayW) / 2
	y := (height - overlayH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return PlaceOverlayAt(base, overlay, x, y, width, height)
}
