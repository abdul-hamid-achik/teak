package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// PlaceOverlayAt composites overlay content at a specific (x, y) position over base content.
// Uses lipgloss Canvas/Compositor for proper ANSI-aware rendering.
func PlaceOverlayAt(base, overlay string, x, y, baseWidth, baseHeight int) string {
	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
	)
	canvas := lipgloss.NewCanvas(baseWidth, baseHeight)
	return canvas.Compose(comp).Render()
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
