package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

// spacePool is a pre-allocated string of spaces for padding.
// This avoids repeated allocations from strings.Repeat(" ", n).
const maxSpacePool = 512

var spacePool = strings.Repeat(" ", maxSpacePool)

// getSpaces returns a string of n spaces from the pool.
// Falls back to strings.Repeat for large values.
func getSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= maxSpacePool {
		return spacePool[:n]
	}
	return strings.Repeat(" ", n)
}

// Viewport manages the visible area of the editor.
type Viewport struct {
	ScrollY     int
	ScrollX     int
	Width       int
	Height      int
	GutterWidth int
}

// Render renders the visible portion of the buffer with gutter, syntax highlighting, and diagnostics.
func (v *Viewport) Render(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts) string {
	return v.RenderWithFolds(buf, theme, hl, diagnostics, gutterOpts, nil)
}

// RenderWithFolds renders the visible portion of the buffer with optional code folding.
func (v *Viewport) RenderWithFolds(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, folds *FoldState) string {
	// Compute visible lines accounting for folds
	var visibleLines []int
	totalVisibleLines := buf.LineCount()
	if folds != nil && len(folds.Regions) > 0 {
		totalVisibleLines = folds.TotalVisibleLines(buf.LineCount())
		visibleLines = folds.VisibleLines(v.foldedScrollStart(folds, buf.LineCount()), v.Height, buf.LineCount())
	}

	gutter, gw := RenderGutterWithFolds(theme, buf.LineCount(), v.ScrollY, v.Height, buf.Cursor.Line, diagnostics, gutterOpts, folds, visibleLines)
	v.GutterWidth = gw + 1 // +1 for gutter padding

	gutterLines := strings.Split(gutter, "\n")
	textWidth := v.Width - v.GutterWidth
	if textWidth < 1 {
		textWidth = 1
	}

	// Scrollbar calculation
	showScrollbar := totalVisibleLines > v.Height
	var thumbStart, thumbEnd int
	if showScrollbar {
		textWidth-- // reserve 1 column for scrollbar
		if textWidth < 1 {
			textWidth = 1
		}
		thumbSize := max(1, v.Height*v.Height/totalVisibleLines)
		maxScroll := totalVisibleLines - v.Height
		if maxScroll < 1 {
			maxScroll = 1
		}
		thumbStart = v.ScrollY * (v.Height - thumbSize) / maxScroll
		thumbEnd = thumbStart + thumbSize
	}

	// Find matching bracket pair for highlighting
	bracketPos1, bracketPos2, hasBracketMatch := v.findBracketHighlights(buf)

	var sb strings.Builder
	for i := range v.Height {
		var line int
		if len(visibleLines) > 0 && i < len(visibleLines) {
			line = visibleLines[i]
		} else if len(visibleLines) > 0 {
			line = buf.LineCount() // past end
		} else {
			line = v.ScrollY + i
		}
		if i > 0 {
			sb.WriteByte('\n')
		}
		// gutter
		if i < len(gutterLines) {
			sb.WriteString(gutterLines[i])
		}
		sb.WriteByte(' ') // padding between gutter and text
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
				rendered := v.renderLineWithTokens(tokens, line == buf.Cursor.Line, textWidth, theme)
				if hasBracketMatch {
					rendered = v.applyBracketHighlight(rendered, lineContent, line, bracketPos1, bracketPos2, textWidth, theme)
				}
				sb.WriteString(rendered)
			} else {
				// plain text rendering
				displayed := applyScrollX(lineContent, v.ScrollX)
				displayed = truncateToWidth(displayed, textWidth)
				padLen := max(0, textWidth-displayWidth(displayed))
				if padLen > 0 {
					displayed += getSpaces(padLen)
				}
				if line == buf.Cursor.Line {
					sb.WriteString(theme.CursorLine.Render(displayed))
				} else {
					sb.WriteString(theme.Editor.Render(displayed))
				}
			}
		} else {
			// empty area below text
			sb.WriteString(theme.Editor.Render(getSpaces(textWidth)))
		}

		// Scrollbar
		if showScrollbar {
			if i >= thumbStart && i < thumbEnd {
				sb.WriteString(theme.ScrollThumb.Render(" "))
			} else {
				sb.WriteString(theme.ScrollTrack.Render(" "))
			}
		}
	}
	return sb.String()
}

// RenderWithWrap renders the viewport with word wrap enabled.
func (v *Viewport) RenderWithWrap(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, wrap *WrapLayout) string {
	// Compute gutter width
	baseWidth := gutterWidth(buf.LineCount())
	markerWidth := 0
	if gutterOpts != nil {
		markerWidth = 3 // 1 leading space + 2-cell icon + 1 trailing space
	}
	gwTotal := baseWidth + markerWidth
	v.GutterWidth = gwTotal + 1 // +1 for padding

	textWidth := v.Width - v.GutterWidth
	if textWidth < 1 {
		textWidth = 1
	}

	// Scrollbar based on total visual rows
	totalRows := wrap.TotalRows()
	showScrollbar := totalRows > v.Height
	var thumbStart, thumbEnd int
	if showScrollbar {
		textWidth--
		if textWidth < 1 {
			textWidth = 1
		}
		thumbSize := max(1, v.Height*v.Height/totalRows)
		visualScrollY := wrap.VisualRow(v.ScrollY)
		maxScroll := totalRows - v.Height
		if maxScroll < 1 {
			maxScroll = 1
		}
		thumbStart = visualScrollY * (v.Height - thumbSize) / maxScroll
		thumbEnd = thumbStart + thumbSize
	}

	// Build diagnostic map
	diagMap := make(map[int]int)
	for _, d := range diagnostics {
		for line := d.StartLine; line <= d.EndLine; line++ {
			if existing, ok := diagMap[line]; !ok || d.Severity < existing {
				diagMap[line] = d.Severity
			}
		}
	}

	// Build visual rows starting from ScrollY
	var sb strings.Builder
	visualRow := 0
	bufLine := v.ScrollY
	wrapOffset := 0

	for visualRow < v.Height {
		if visualRow > 0 {
			sb.WriteByte('\n')
		}

		if bufLine < buf.LineCount() {
			// Gutter: show line number on first wrap row, blank on continuation
			if wrapOffset == 0 {
				sb.WriteString(v.renderWrapGutterLine(theme, buf, gutterOpts, diagMap, bufLine, baseWidth, markerWidth))
			} else {
				sb.WriteString(theme.Gutter.Render(getSpaces(gwTotal)))
			}
			// Padding between gutter and text
			sb.WriteByte(' ')

			lineContent := string(buf.Line(bufLine))
			segment := wrapSegment(lineContent, wrapOffset, textWidth)

			// Apply syntax highlighting to segment if available
			rendered := v.renderWrapSegment(theme, hl, buf, bufLine, segment, wrapOffset, textWidth)
			padLen := max(0, textWidth-displayWidth(segment))

			if bufLine == buf.Cursor.Line {
				sb.WriteString(rendered)
				if padLen > 0 {
					sb.WriteString(theme.CursorLine.Render(getSpaces(padLen)))
				}
			} else {
				sb.WriteString(rendered)
				if padLen > 0 {
					sb.WriteString(theme.Editor.Render(getSpaces(padLen)))
				}
			}

			wrapOffset++
			if wrapOffset >= wrap.LineRows(bufLine) {
				bufLine++
				wrapOffset = 0
			}
		} else {
			sb.WriteString(theme.Gutter.Render(getSpaces(gwTotal)))
			sb.WriteByte(' ')
			sb.WriteString(theme.Editor.Render(getSpaces(textWidth)))
		}

		// Scrollbar
		if showScrollbar {
			if visualRow >= thumbStart && visualRow < thumbEnd {
				sb.WriteString(theme.ScrollThumb.Render(" "))
			} else {
				sb.WriteString(theme.ScrollTrack.Render(" "))
			}
		}

		visualRow++
	}
	return sb.String()
}

// renderWrapGutterLine renders a single gutter line for wrap mode.
func (v *Viewport) renderWrapGutterLine(theme ui.Theme, buf *text.Buffer, gutterOpts *GutterOpts, diagMap map[int]int, line, baseWidth, markerWidth int) string {
	var sb strings.Builder

	// Breakpoint marker (1 leading space + 2-cell icon + 1 trailing space)
	// Use pre-cached theme styles to avoid allocations
	if gutterOpts != nil {
		switch gutterOpts.Breakpoints[line] {
		case BPActive:
			sb.WriteByte(' ')
			sb.WriteString(theme.BreakpointActive.Render("\U000f0765"))
		case BPDisabled:
			sb.WriteByte(' ')
			sb.WriteString(theme.BreakpointDisabled.Render("\U000f0765"))
		default:
			sb.WriteString("   ")
		}
	}

	numStr := formatLineNumber(line, baseWidth)

	isExecLine := gutterOpts != nil && gutterOpts.ExecLine == line
	if isExecLine {
		sb.WriteString(theme.ExecLineMarker.Render(numStr))
	} else if sev, ok := diagMap[line]; ok {
		switch sev {
		case 1:
			sb.WriteString(theme.GutterError.Render(numStr))
		case 2:
			sb.WriteString(theme.GutterWarn.Render(numStr))
		default:
			if line == buf.Cursor.Line {
				sb.WriteString(theme.GutterActive.Render(numStr))
			} else {
				sb.WriteString(theme.Gutter.Render(numStr))
			}
		}
	} else if line == buf.Cursor.Line {
		sb.WriteString(theme.GutterActive.Render(numStr))
	} else {
		sb.WriteString(theme.Gutter.Render(numStr))
	}
	return sb.String()
}

// renderWrapSegment renders a wrap segment with syntax highlighting when available.
func (v *Viewport) renderWrapSegment(theme ui.Theme, hl *highlight.Highlighter, buf *text.Buffer, bufLine int, segment string, wrapOffset, textWidth int) string {
	defaultStyle := theme.Editor
	if bufLine == buf.Cursor.Line {
		defaultStyle = theme.CursorLine
	}

	if hl == nil || segment == "" {
		return defaultStyle.Render(segment)
	}

	tokens := hl.Line(bufLine)
	if len(tokens) == 0 {
		return defaultStyle.Render(segment)
	}

	// Map segment back to the full line using display-width offsets
	segStartWidth := wrapOffset * textWidth
	segEndWidth := segStartWidth + textWidth

	var sb strings.Builder
	currentWidth := 0

	// Process tokens and accumulate segments with the same style
	// instead of styling each rune individually
	for _, tok := range tokens {
		tokText := tok.Text
		tokWidth := runewidth.StringWidth(tokText)
		tokEndWidth := currentWidth + tokWidth

		// Skip tokens completely before the segment
		if tokEndWidth <= segStartWidth {
			currentWidth = tokEndWidth
			continue
		}

		// Stop if we've passed the segment
		if currentWidth >= segEndWidth {
			break
		}

		// Calculate overlap with visible segment
		overlapStart := max(0, segStartWidth-currentWidth)
		overlapEnd := min(tokWidth, segEndWidth-currentWidth)

		if overlapStart < overlapEnd {
			// Extract overlapping text portion efficiently
			overlapText := extractWidthRange(tokText, overlapStart, overlapEnd)

			// Apply style (with cursor line background if needed)
			style := tok.Style
			if bufLine == buf.Cursor.Line {
				style = style.Background(defaultStyle.GetBackground())
			}

			// Style the entire overlapping segment at once (not rune-by-rune)
			sb.WriteString(style.Render(overlapText))
		}

		currentWidth = tokEndWidth
	}

	result := sb.String()
	if result == "" && segment != "" {
		return defaultStyle.Render(segment)
	}
	return result
}

// extractWidthRange extracts a substring by display width range
// This is more efficient than styling each rune individually
func extractWidthRange(text string, startWidth, endWidth int) string {
	var result strings.Builder
	currentWidth := 0

	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if currentWidth >= startWidth && currentWidth < endWidth {
			result.WriteRune(r)
		}
		currentWidth += rw
		if currentWidth >= endWidth {
			break
		}
	}

	return result.String()
}

// wrapSegment extracts the Nth segment of a line when wrapped at the given width.
func wrapSegment(line string, segIdx, width int) string {
	if width < 1 || segIdx < 0 {
		return ""
	}
	// Walk through the string counting display width
	startWidth := segIdx * width
	endWidth := startWidth + width

	currentWidth := 0
	startByte := -1
	endByte := len(line)

	for i, r := range line {
		w := runewidth.RuneWidth(r)
		if currentWidth >= startWidth && startByte < 0 {
			startByte = i
		}
		if currentWidth >= endWidth {
			endByte = i
			break
		}
		currentWidth += w
	}
	if startByte < 0 {
		return ""
	}
	return line[startByte:endByte]
}

// foldedScrollStart converts the visual scroll position to the buffer line
// that should start the visible region, accounting for collapsed folds.
func (v *Viewport) foldedScrollStart(folds *FoldState, totalLines int) int {
	return folds.VisualLineToBuffer(v.ScrollY, totalLines)
}

// findBracketHighlights returns two positions to highlight and whether a match was found.
func (v *Viewport) findBracketHighlights(buf *text.Buffer) (text.Position, text.Position, bool) {
	cursor := buf.Cursor
	line := buf.Line(cursor.Line)

	// Check character at cursor
	if cursor.Col < len(line) {
		ch := line[cursor.Col]
		if IsOpenBracket(ch) || IsCloseBracket(ch) {
			if match, ok := FindMatchingBracket(buf, cursor); ok {
				return cursor, match, true
			}
		}
	}

	// Check character before cursor
	if cursor.Col > 0 && cursor.Col <= len(line) {
		prevPos := text.Position{Line: cursor.Line, Col: cursor.Col - 1}
		ch := line[cursor.Col-1]
		if IsOpenBracket(ch) || IsCloseBracket(ch) {
			if match, ok := FindMatchingBracket(buf, prevPos); ok {
				return prevPos, match, true
			}
		}
	}

	return text.Position{}, text.Position{}, false
}

// applyBracketHighlight applies bracket highlight styling to a rendered line at the matching positions.
func (v *Viewport) applyBracketHighlight(rendered, lineContent string, lineNum int, pos1, pos2 text.Position, textWidth int, theme ui.Theme) string {
	// Check if either bracket position is on this line
	var cols []int
	if pos1.Line == lineNum {
		cols = append(cols, pos1.Col)
	}
	if pos2.Line == lineNum {
		cols = append(cols, pos2.Col)
	}
	if len(cols) == 0 {
		return rendered
	}

	for _, col := range cols {
		// Convert byte column to display column, accounting for scroll
		if col >= len(lineContent) {
			continue
		}
		displayCol := displayWidth(lineContent[:col]) - v.ScrollX
		if displayCol < 0 || displayCol >= textWidth {
			continue
		}

		// Get the bracket character
		ch := lineContent[col]
		bracketStr := string(ch)
		styledBracket := theme.BracketMatch.Render(bracketStr)

		// Walk the rendered string (which contains ANSI codes) to find and replace
		// the bracket at the correct display position
		rendered = replaceAtDisplayCol(rendered, displayCol, bracketStr, styledBracket)
	}
	return rendered
}

// replaceAtDisplayCol replaces a character at a given display column in an ANSI-styled string.
func replaceAtDisplayCol(s string, targetCol int, oldChar, replacement string) string {
	col := 0
	i := 0
	for i < len(s) {
		// Skip ANSI escape sequences
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		rw := runewidth.RuneWidth(r)
		if col == targetCol && string(r) == oldChar {
			return s[:i] + replacement + s[i+size:]
		}
		col += rw
		i += size

		if col > targetCol {
			break
		}
	}
	return s
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
		sb.WriteString(baseStyle.Render(getSpaces(widthLeft)))
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
		sb.WriteString(baseStyle.Render(getSpaces(widthLeft)))
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
// gw is the effective gutter width (from Editor.effectiveGutterWidth).
// visibleLines is optional — when folds are active, pass the visible lines slice
// so screen rows map to the correct buffer lines.
func (v *Viewport) ScreenToBufferPosition(screenX, screenY int, buf *text.Buffer, gw int, visibleLines []int) text.Position {
	var line int
	if len(visibleLines) > 0 && screenY >= 0 && screenY < len(visibleLines) {
		line = visibleLines[screenY]
	} else {
		line = v.ScrollY + screenY
	}
	if line < 0 {
		line = 0
	}
	if line >= buf.LineCount() {
		line = buf.LineCount() - 1
	}
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

// ScreenToBufferPositionWrap maps screen coordinates to buffer position in word-wrap mode.
func (v *Viewport) ScreenToBufferPositionWrap(screenX, screenY int, buf *text.Buffer, gw int, wrap *WrapLayout) text.Position {
	// Convert screen Y to visual row relative to scroll position
	visualRow := wrap.VisualRow(v.ScrollY) + screenY
	bufLine, wrapOffset := wrap.BufferLine(visualRow)

	if bufLine < 0 {
		bufLine = 0
	}
	if bufLine >= buf.LineCount() {
		bufLine = buf.LineCount() - 1
	}

	screenCol := screenX - gw
	if screenCol < 0 {
		screenCol = 0
	}

	textWidth := v.Width - gw
	if textWidth < 1 {
		textWidth = 1
	}

	// Target display-width offset within the full line
	targetWidth := wrapOffset*textWidth + screenCol

	lineContent := buf.Line(bufLine)
	w := 0
	col := 0
	for i, r := range string(lineContent) {
		rw := runewidth.RuneWidth(r)
		if w+rw > targetWidth {
			col = i
			return text.Position{Line: bufLine, Col: col}
		}
		w += rw
		col = i + utf8.RuneLen(r)
	}
	if col > len(lineContent) {
		col = len(lineContent)
	}
	return text.Position{Line: bufLine, Col: col}
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
