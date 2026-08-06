package app

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/editor"
)

// lspOverlayPlacement is the actual on-screen rectangle after a popup has
// been clipped to the active editor. It is deliberately independent from the
// terminal canvas so it can be shared by View and mouse routing.
type lspOverlayPlacement struct {
	x, y          int
	width, height int
	content       string
}

func (m Model) currentLSPOverlayPlacement() (lspOverlayPlacement, bool) {
	ed := m.activeEditor()
	if ed == nil {
		return lspOverlayPlacement{}, false
	}
	return m.lspOverlayPlacement(ed, ed.LSPOverlayView())
}

func (p lspOverlayPlacement) contains(x, y int) bool {
	return p.width > 0 && p.height > 0 &&
		x >= p.x && x < p.x+p.width && y >= p.y && y < p.y+p.height
}

// lspOverlayPlacement anchors the selected LSP popup at the cursor without
// allowing it to spill into the sidebar, tab bar, status bar, or agent panel.
// It prefers the row below the cursor and switches above it when that leaves
// more room. A one-row editor has no safe popup row, so the overlay is hidden
// rather than replacing the editing cell.
func (m Model) lspOverlayPlacement(ed *editor.Editor, overlay string) (lspOverlayPlacement, bool) {
	if ed == nil || overlay == "" {
		return lspOverlayPlacement{}, false
	}
	body := m.activeEditorBodyRect()
	if body.width <= 0 || body.height <= 1 {
		return lspOverlayPlacement{}, false
	}

	cursorX, cursorY := ed.CursorPosition()
	anchorX := min(max(body.x+cursorX, body.x), body.x+body.width-1)
	anchorY := min(max(body.y+cursorY, body.y), body.y+body.height-1)
	above := anchorY - body.y
	below := body.y + body.height - (anchorY + 1)
	if above == 0 && below == 0 {
		return lspOverlayPlacement{}, false
	}

	// Calculate the un-clipped height first; it decides which side gives the
	// popup the most useful visible area.
	requestedHeight := len(strings.Split(overlay, "\n"))
	placeAbove := below < requestedHeight && above > below
	availableHeight := below
	y := anchorY + 1
	if placeAbove {
		availableHeight = above
	}
	if availableHeight <= 0 {
		// There is no row above, so use the non-zero space below even when the
		// requested popup is taller than it.
		availableHeight = below
		y = anchorY + 1
	}
	content, width, height := clipLSPOverlay(overlay, body.width, availableHeight)
	if content == "" || width == 0 || height == 0 {
		return lspOverlayPlacement{}, false
	}
	if placeAbove {
		y = anchorY - height
	}
	x := min(anchorX, body.x+body.width-width)
	return lspOverlayPlacement{x: x, y: y, width: width, height: height, content: content}, true
}

// clipLSPOverlay preserves ANSI and Unicode cell boundaries while enforcing
// the editor rectangle. ui.PlaceOverlayAt performs the final composition.
func clipLSPOverlay(overlay string, maxWidth, maxHeight int) (string, int, int) {
	if maxWidth <= 0 || maxHeight <= 0 || overlay == "" {
		return "", 0, 0
	}
	lines := strings.Split(overlay, "\n")
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
	}
	width := 0
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, maxWidth, "")
		width = max(width, ansi.StringWidth(lines[i]))
	}
	if width == 0 {
		return "", 0, 0
	}
	return strings.Join(lines, "\n"), width, len(lines)
}
