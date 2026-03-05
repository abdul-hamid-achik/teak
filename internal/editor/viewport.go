package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

// Viewport manages the visible area of the editor.
type Viewport struct {
	ScrollY     int
	ScrollX     int
	Width       int
	Height      int
	GutterWidth int
}

// Render renders the visible portion of the buffer with gutter, syntax highlighting, and diagnostics.
func (v *Viewport) Render(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic) string {
	gutter, gw := RenderGutter(theme, buf.LineCount(), v.ScrollY, v.Height, buf.Cursor.Line, diagnostics)
	v.GutterWidth = gw + 1 // +1 for gutter padding

	gutterLines := strings.Split(gutter, "\n")
	textWidth := v.Width - v.GutterWidth
	if textWidth < 1 {
		textWidth = 1
	}

	var sb strings.Builder
	for i := range v.Height {
		line := v.ScrollY + i
		if i > 0 {
			sb.WriteByte('\n')
		}
		// gutter
		if i < len(gutterLines) {
			sb.WriteString(gutterLines[i])
		}
		// text content
		if line < buf.LineCount() {
			lineBytes := buf.Line(line)
			lineContent := string(lineBytes)
			lineLen := len(lineBytes)

			// Check for selection on this line
			selStart, selEnd := selectionRange(buf.Selection, line, lineLen)
			hasSelection := selStart >= 0 && selEnd > selStart

			// Check for syntax highlighting tokens
			var tokens []highlight.StyledToken
			if hl != nil {
				tokens = hl.Line(line)
			}

			if hasSelection {
				sb.WriteString(v.renderLineWithSelection(lineContent, lineBytes, selStart, selEnd, line == buf.Cursor.Line, textWidth, theme))
			} else if len(tokens) > 0 {
				sb.WriteString(v.renderLineWithTokens(tokens, line == buf.Cursor.Line, textWidth, theme))
			} else {
				// plain text rendering
				displayed := applyScrollX(lineContent, v.ScrollX)
				displayed = truncateToWidth(displayed, textWidth)
				if line == buf.Cursor.Line {
					padded := displayed + strings.Repeat(" ", max(0, textWidth-displayWidth(displayed)))
					sb.WriteString(theme.CursorLine.Render(padded))
				} else {
					sb.WriteString(theme.Editor.Render(displayed))
				}
			}
		} else {
			// empty area below text
			sb.WriteString(theme.Editor.Render(strings.Repeat(" ", textWidth)))
		}
	}
	return sb.String()
}

// renderLineWithTokens renders a line using syntax-highlighted tokens.
func (v *Viewport) renderLineWithTokens(tokens []highlight.StyledToken, isCursorLine bool, textWidth int, theme ui.Theme) string {
	var sb strings.Builder
	widthLeft := textWidth
	scrollRemaining := v.ScrollX

	for _, tok := range tokens {
		if widthLeft <= 0 {
			break
		}
		text := tok.Text
		// Apply horizontal scroll
		if scrollRemaining > 0 {
			textW := runewidth.StringWidth(text)
			if textW <= scrollRemaining {
				scrollRemaining -= textW
				continue
			}
			// Skip runes until we've consumed scrollRemaining display width
			w := 0
			for j, r := range text {
				rw := runewidth.RuneWidth(r)
				if w+rw > scrollRemaining {
					text = text[j:]
					scrollRemaining = 0
					break
				}
				w += rw
			}
			if scrollRemaining > 0 {
				text = ""
				scrollRemaining -= w
			}
		}

		// Truncate to remaining width
		textW := displayWidth(text)
		if textW > widthLeft {
			text = truncateToWidth(text, widthLeft)
			textW = widthLeft
		}

		style := tok.Style
		if isCursorLine {
			style = style.Background(ui.Nord1)
		}
		sb.WriteString(style.Render(text))
		widthLeft -= textW
	}

	// Pad remaining width
	if widthLeft > 0 {
		baseStyle := theme.Editor
		if isCursorLine {
			baseStyle = theme.CursorLine
		}
		sb.WriteString(baseStyle.Render(strings.Repeat(" ", widthLeft)))
	}

	return sb.String()
}

// selectionRange returns the byte range of the selection overlapping a line.
// Returns (-1, -1) if no overlap.
func selectionRange(sel *text.Selection, line, lineLen int) (int, int) {
	if sel == nil || sel.IsEmpty() {
		return -1, -1
	}
	start, end := sel.Ordered()

	// No overlap
	if line < start.Line || line > end.Line {
		return -1, -1
	}

	startCol := 0
	if line == start.Line {
		startCol = start.Col
	}

	endCol := lineLen
	if line == end.Line {
		endCol = end.Col
	}

	if startCol >= endCol {
		return -1, -1
	}
	return startCol, endCol
}

func (v *Viewport) renderLineWithSelection(lineContent string, lineBytes []byte, selStart, selEnd int, isCursorLine bool, textWidth int, theme ui.Theme) string {
	// Clamp selection to line bounds
	lineLen := len(lineBytes)
	if selStart < 0 {
		selStart = 0
	}
	if selEnd > lineLen {
		selEnd = lineLen
	}

	// Split into before/selected/after by byte offset
	before := lineContent[:selStart]
	selected := lineContent[selStart:selEnd]
	after := lineContent[selEnd:]

	// Apply horizontal scroll to the segments
	scrollRemaining := v.ScrollX
	before, scrollRemaining = applyScrollXCount(before, scrollRemaining)
	selected, scrollRemaining = applyScrollXCount(selected, scrollRemaining)
	after, _ = applyScrollXCount(after, scrollRemaining)

	// Calculate available width for each segment
	widthLeft := textWidth
	var sb strings.Builder

	baseStyle := theme.Editor
	if isCursorLine {
		baseStyle = theme.CursorLine
	}

	// Before selection
	beforeW := displayWidth(before)
	if beforeW > widthLeft {
		before = truncateToWidth(before, widthLeft)
		beforeW = widthLeft
	}
	if beforeW > 0 {
		sb.WriteString(baseStyle.Render(before))
		widthLeft -= beforeW
	}

	// Selected
	if widthLeft > 0 {
		selectedW := displayWidth(selected)
		if selectedW > widthLeft {
			selected = truncateToWidth(selected, widthLeft)
			selectedW = widthLeft
		}
		if selectedW > 0 {
			sb.WriteString(theme.Selection.Render(selected))
			widthLeft -= selectedW
		}
	}

	// After selection
	if widthLeft > 0 {
		afterW := displayWidth(after)
		if afterW > widthLeft {
			after = truncateToWidth(after, widthLeft)
			afterW = widthLeft
		}
		if afterW > 0 {
			sb.WriteString(baseStyle.Render(after))
			widthLeft -= afterW
		}
	}

	// Pad remaining width
	if widthLeft > 0 {
		sb.WriteString(baseStyle.Render(strings.Repeat(" ", widthLeft)))
	}

	return sb.String()
}

// applyScrollXCount scrolls a string and returns remaining scroll amount.
func applyScrollXCount(s string, scrollX int) (string, int) {
	if scrollX <= 0 {
		return s, 0
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > scrollX {
			return s[i:], 0
		}
		w += rw
	}
	return "", scrollX - w
}

// ScreenToBufferPosition maps screen coordinates to buffer position.
func (v *Viewport) ScreenToBufferPosition(screenX, screenY int, buf *text.Buffer) text.Position {
	line := v.ScrollY + screenY
	if line < 0 {
		line = 0
	}
	if line >= buf.LineCount() {
		line = buf.LineCount() - 1
	}

	// Compute gutter width directly instead of relying on cached GutterWidth,
	// since View() uses a value receiver and the mutation from Render() is lost.
	gw := gutterWidth(buf.LineCount()) + 1 // +1 for gutter padding
	screenCol := screenX - gw
	if screenCol < 0 {
		screenCol = 0
	}

	lineContent := buf.Line(line)
	// Walk runes summing display widths to convert screen X to buffer column
	targetWidth := v.ScrollX + screenCol
	w := 0
	col := 0
	for i, r := range string(lineContent) {
		rw := runewidth.RuneWidth(r)
		if w+rw > targetWidth {
			col = i
			return text.Position{Line: line, Col: col}
		}
		w += rw
		col = i + utf8.RuneLen(r)
	}
	// Clamp to line length
	if col > len(lineContent) {
		col = len(lineContent)
	}
	return text.Position{Line: line, Col: col}
}

// EnsureCursorVisible scrolls to keep the cursor in view.
func (v *Viewport) EnsureCursorVisible(cursor text.Position, lineCount int) {
	// vertical
	if cursor.Line < v.ScrollY {
		v.ScrollY = cursor.Line
	}
	if cursor.Line >= v.ScrollY+v.Height {
		v.ScrollY = cursor.Line - v.Height + 1
	}

	// horizontal — use display width, not byte offset
	gw := gutterWidth(lineCount) + 1
	textWidth := v.Width - gw
	if textWidth < 1 {
		textWidth = 1
	}
	// ScrollX is in display columns, so compare with display width of cursor col
	displayCol := cursor.Col // for ASCII this is fine, but we don't have line content here
	if displayCol < v.ScrollX {
		v.ScrollX = displayCol
	}
	if displayCol >= v.ScrollX+textWidth {
		v.ScrollX = displayCol - textWidth + 1
	}
}

// ScrollUp scrolls up by n lines.
func (v *Viewport) ScrollUp(n int) {
	v.ScrollY -= n
	if v.ScrollY < 0 {
		v.ScrollY = 0
	}
}

// ScrollDown scrolls down by n lines, clamped to maxLine.
func (v *Viewport) ScrollDown(n, maxLine int) {
	v.ScrollY += n
	maxScroll := maxLine - v.Height + 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.ScrollY > maxScroll {
		v.ScrollY = maxScroll
	}
}

func applyScrollX(s string, scrollX int) string {
	if scrollX <= 0 {
		return s
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > scrollX {
			return s[i:]
		}
		w += rw
	}
	return ""
}

func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width {
			return s[:i]
		}
		w += rw
	}
	return s
}

func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}
