package editor

import (
	"sort"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	ScrollY int
	// WrapScrollY is the first visual row when word wrap is active. ScrollY
	// remains a logical buffer line for the normal and folded renderers.
	WrapScrollY int
	ScrollX     int
	Width       int
	Height      int
	GutterWidth int
	TabSize     int

	wrapTokenCacheLine    int
	wrapTokenCacheVersion int
	wrapTokenCacheCount   int
	wrapTokenCacheStarts  []int

	bracketCacheRope    *text.Rope
	bracketCacheVersion int
	bracketCacheCursor  text.Position
	bracketCachePos1    text.Position
	bracketCachePos2    text.Position
	bracketCacheFound   bool
}

// selectionByteRange is a half-open byte range within one buffer line.
// It is intentionally local to a viewport render frame and reused by
// selectionRangeIterator rather than allocated and sorted for every row.
type selectionByteRange struct {
	start, end int
}

// selectionRangeIterator translates the sorted text selection sweep into byte
// ranges for the current buffer line. A visible viewport advances it once per
// logical line, even if that line has several word-wrapped terminal rows.
type selectionRangeIterator struct {
	selections *text.SelectionLineIterator
	ranges     []selectionByteRange
}

func newSelectionRangeIterator(selections *text.Selections) *selectionRangeIterator {
	return &selectionRangeIterator{selections: selections.LineIterator()}
}

func (it *selectionRangeIterator) Ranges(line, lineLen int) []selectionByteRange {
	if it == nil || it.selections == nil {
		return nil
	}
	it.ranges = it.ranges[:0]
	for _, selection := range it.selections.ForLine(line) {
		if selection.IsEmpty() {
			continue
		}
		start, end := selection.Ordered()
		startCol, endCol := 0, lineLen
		if line == start.Line {
			startCol = start.Col
		}
		if line == end.Line {
			endCol = end.Col
		}
		if startCol < endCol {
			it.ranges = append(it.ranges, selectionByteRange{start: startCol, end: endCol})
		}
	}
	return it.ranges
}

func (v *Viewport) tabSize() int {
	if v.TabSize == 0 {
		return 4
	}
	return normalizeTabSize(v.TabSize)
}

func (v *Viewport) wrapScrollY(wrap *WrapLayout) int {
	if wrap == nil {
		return 0
	}
	if !wrap.TotalRowsKnown() {
		v.WrapScrollY = max(0, v.WrapScrollY)
		return v.WrapScrollY
	}
	if wrap.TotalRows() < 1 {
		v.WrapScrollY = 0
		return 0
	}
	maxScroll := max(0, wrap.TotalRows()-max(1, v.Height))
	v.WrapScrollY = max(0, min(v.WrapScrollY, maxScroll))
	return v.WrapScrollY
}

func (v *Viewport) wrapTokenStarts(line, version int, tokens []highlight.StyledToken) []int {
	if v.wrapTokenCacheLine == line && v.wrapTokenCacheVersion == version && v.wrapTokenCacheCount == len(tokens) {
		return v.wrapTokenCacheStarts
	}
	v.wrapTokenCacheLine = line
	v.wrapTokenCacheVersion = version
	v.wrapTokenCacheCount = len(tokens)
	v.wrapTokenCacheStarts = tokenByteStarts(tokens)
	return v.wrapTokenCacheStarts
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
	selectionIterator := newSelectionRangeIterator(buf.Selections)

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

			// Check for ALL selections on this line
			selectionRanges := selectionIterator.Ranges(line, lineLen)
			hasSelection := len(selectionRanges) > 0

			// Check for syntax highlighting tokens
			var tokens []highlight.StyledToken
			if hl != nil {
				tokens = hl.Line(line)
			}

			if hasSelection {
				sb.WriteString(v.renderLineWithMultipleSelectionsTabs(lineBytes, selectionRanges, line == buf.Selections.PrimaryCursor().Line, textWidth, theme))
			} else if len(tokens) > 0 {
				rendered := v.renderLineWithTokens(tokens, line == buf.Selections.PrimaryCursor().Line, textWidth, theme)
				if hasBracketMatch {
					rendered = v.applyBracketHighlight(rendered, lineContent, line, bracketPos1, bracketPos2, textWidth, theme)
				}
				sb.WriteString(rendered)
			} else {
				// plain text rendering
				displayed := applyScrollX(expandTabsForDisplay(lineBytes, v.tabSize()), v.ScrollX)
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
	metrics := computeGutterMetrics(buf.LineCount(), gutterOpts, false)
	v.GutterWidth = metrics.totalWidth()
	baseWidth := metrics.lineNumberWidth
	markerWidth := metrics.markerWidth
	gutterStyle := theme.Gutter.UnsetPadding()
	textWidth := wrap.Width()
	if textWidth < 1 {
		textWidth = 1
	}

	// Resolve only the page containing the first requested visual row before
	// deriving scrollbar geometry. This lets a long first line reveal its
	// wrapped height without forcing measurement of the rest of the document.
	visualScrollY := v.wrapScrollY(wrap)
	bufLine, wrapOffset := wrap.BufferLine(visualScrollY)
	if clamped := v.wrapScrollY(wrap); clamped != visualScrollY {
		visualScrollY = clamped
		bufLine, wrapOffset = wrap.BufferLine(visualScrollY)
	}

	// Scrollbar uses an exact total once all pages are known, otherwise a safe
	// lower-bound estimate that is refined as the user visits more pages.
	totalRows := wrap.TotalRows()
	showScrollbar := totalRows > v.Height
	var thumbStart, thumbEnd int
	if showScrollbar {
		thumbSize := max(1, v.Height*v.Height/totalRows)
		maxScroll := totalRows - v.Height
		if maxScroll < 1 {
			maxScroll = 1
		}
		thumbStart = visualScrollY * (v.Height - thumbSize) / maxScroll
		thumbEnd = thumbStart + thumbSize
	}

	// Build visual rows starting from the visual scroll position. This is
	// intentionally independent of ScrollY so a user can scroll through a
	// single very long logical line.
	var sb strings.Builder
	visualRow := 0
	// A Rope line may span multiple leaves and therefore materialize a byte
	// slice. Reuse it for all of its visual rows; without this, a single long
	// wrapped line was copied once per terminal row.
	loadedLine := -1
	var lineBytes []byte
	var lineContent string
	var lineTokens []highlight.StyledToken
	var lineTokenStarts []int
	selectionIterator := newSelectionRangeIterator(buf.Selections)
	loadedSelectionLine := -1
	var lineSelectionRanges []selectionByteRange

	for visualRow < v.Height {
		if visualRow > 0 {
			sb.WriteByte('\n')
		}

		if bufLine < buf.LineCount() {
			// Gutter: show line number on first wrap row, blank on continuation
			if wrapOffset == 0 {
				sb.WriteString(v.renderWrapGutterLine(theme, buf, gutterOpts, diagnostics, bufLine, baseWidth, markerWidth))
			} else {
				sb.WriteString(gutterStyle.Render(getSpaces(metrics.contentWidth())))
			}
			// Padding between gutter and text
			sb.WriteByte(' ')

			if loadedLine != bufLine {
				lineBytes = buf.Line(bufLine)
				lineContent = string(lineBytes)
				lineTokens = nil
				lineTokenStarts = nil
				if hl != nil {
					lineTokens = hl.Line(bufLine)
					lineTokenStarts = v.wrapTokenStarts(bufLine, buf.Version(), lineTokens)
				}
				loadedLine = bufLine
			}
			if loadedSelectionLine != bufLine {
				lineSelectionRanges = selectionIterator.Ranges(bufLine, len(lineContent))
				loadedSelectionLine = bufLine
			}
			segmentStart, segmentEnd, segmentDisplayStart, ok := wrap.SegmentBoundsForLine(bufLine, wrapOffset, lineBytes)
			if !ok {
				segmentStart, segmentEnd, segmentDisplayStart = 0, 0, 0
			}

			// Selections take precedence over syntax highlighting, matching regular rendering.
			rendered := v.renderWrapSegmentWithSelections(theme, lineTokens, lineTokenStarts, bufLine == buf.Cursor.Line, lineContent, lineSelectionRanges, segmentStart, segmentEnd, segmentDisplayStart)
			rendered = ansi.Truncate(rendered, textWidth, "")
			padLen := max(0, textWidth-ansi.StringWidth(rendered))

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
			sb.WriteString(gutterStyle.Render(getSpaces(metrics.contentWidth())))
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

// renderWrapSegmentWithSelections renders a wrapped segment, applying selection
// styles to any ranges that overlap the segment.
func (v *Viewport) renderWrapSegmentWithSelections(theme ui.Theme, tokens []highlight.StyledToken, tokenStarts []int, isCursorLine bool, lineContent string, ranges []selectionByteRange, segmentStart, segmentEnd, segmentDisplayStart int) string {
	hasOverlap := false
	for _, r := range ranges {
		if r.start < segmentEnd && r.end > segmentStart {
			hasOverlap = true
			break
		}
	}
	baseStyle := theme.Editor
	selectionStyle := theme.SecondarySelection
	if isCursorLine {
		baseStyle = theme.CursorLine
		selectionStyle = theme.Selection
	}

	if !hasOverlap {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent, tokens, tokenStarts, segmentStart, segmentEnd, segmentDisplayStart, baseStyle, v.tabSize())
		return rendered
	}

	var sb strings.Builder
	pos := segmentStart
	displayPos := segmentDisplayStart
	for _, r := range ranges {
		start := max(r.start, segmentStart)
		end := min(r.end, segmentEnd)
		if start >= end {
			continue
		}
		if pos < start {
			rendered, nextDisplay := renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent, tokens, tokenStarts, pos, start, displayPos, baseStyle, v.tabSize())
			sb.WriteString(rendered)
			displayPos = nextDisplay
		}
		rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayPos, selectionStyle, v.tabSize())
		sb.WriteString(rendered)
		displayPos = nextDisplay
		pos = end
	}
	if pos < segmentEnd {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent, tokens, tokenStarts, pos, segmentEnd, displayPos, baseStyle, v.tabSize())
		sb.WriteString(rendered)
	}
	return sb.String()
}

// renderWrapGutterLine renders a single gutter line for wrap mode.
func (v *Viewport) renderWrapGutterLine(theme ui.Theme, buf *text.Buffer, gutterOpts *GutterOpts, diagnostics []Diagnostic, line, baseWidth, markerWidth int) string {
	var sb strings.Builder
	gutterStyle := theme.Gutter.UnsetPadding()
	gutterActiveStyle := theme.GutterActive.UnsetPadding()
	gutterErrorStyle := theme.GutterError.UnsetPadding()
	gutterWarnStyle := theme.GutterWarn.UnsetPadding()

	// Breakpoint marker (1 leading space + icon + 1 trailing space)
	// Use pre-cached theme styles to avoid allocations
	if gutterOpts != nil {
		switch gutterOpts.Breakpoints[line] {
		case BPActive:
			sb.WriteByte(' ')
			sb.WriteString(theme.BreakpointActive.Render(breakpointGlyph()))
			sb.WriteByte(' ')
		case BPDisabled:
			sb.WriteByte(' ')
			sb.WriteString(theme.BreakpointDisabled.Render(breakpointGlyph()))
			sb.WriteByte(' ')
		default:
			sb.WriteString("   ")
		}
	}

	numStr := formatLineNumber(line, baseWidth)

	isExecLine := gutterOpts != nil && gutterOpts.ExecLine == line
	if isExecLine {
		sb.WriteString(theme.ExecLineMarker.Render(numStr))
	} else if sev, ok := diagnosticSeverityAt(diagnostics, line); ok {
		switch sev {
		case 1:
			sb.WriteString(gutterErrorStyle.Render(numStr))
		case 2:
			sb.WriteString(gutterWarnStyle.Render(numStr))
		default:
			if line == buf.Cursor.Line {
				sb.WriteString(gutterActiveStyle.Render(numStr))
			} else {
				sb.WriteString(gutterStyle.Render(numStr))
			}
		}
	} else if line == buf.Cursor.Line {
		sb.WriteString(gutterActiveStyle.Render(numStr))
	} else {
		sb.WriteString(gutterStyle.Render(numStr))
	}
	return sb.String()
}

// renderTokenByteRange renders a byte range using the cached syntax token
// styles. Gaps or missing tokens fall back to the line's base style.
func renderTokenByteRange(lineContent string, tokens []highlight.StyledToken, start, end int, baseStyle lipgloss.Style) string {
	start = max(0, min(start, len(lineContent)))
	end = max(start, min(end, len(lineContent)))
	if start == end {
		return ""
	}
	if len(tokens) == 0 {
		return baseStyle.Render(lineContent[start:end])
	}

	var sb strings.Builder
	pos := start
	tokenStart := 0
	for _, token := range tokens {
		tokenEnd := tokenStart + len(token.Text)
		if tokenEnd <= start {
			tokenStart = tokenEnd
			continue
		}
		if tokenStart >= end {
			break
		}

		overlapStart := max(start, tokenStart)
		overlapEnd := min(end, tokenEnd)
		if pos < overlapStart {
			sb.WriteString(baseStyle.Render(lineContent[pos:overlapStart]))
		}
		if overlapStart < overlapEnd {
			style := token.Style
			if _, noBackground := baseStyle.GetBackground().(lipgloss.NoColor); !noBackground {
				style = style.Background(baseStyle.GetBackground())
			}
			sb.WriteString(style.Render(lineContent[overlapStart:overlapEnd]))
			pos = overlapEnd
		}
		tokenStart = tokenEnd
	}
	if pos < end {
		sb.WriteString(baseStyle.Render(lineContent[pos:end]))
	}
	return sb.String()
}

func tokenByteStarts(tokens []highlight.StyledToken) []int {
	if len(tokens) == 0 {
		return nil
	}
	starts := make([]int, len(tokens))
	for i := range tokens {
		if i > 0 {
			starts[i] = starts[i-1] + len(tokens[i-1].Text)
		}
	}
	return starts
}

func renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent string, tokens []highlight.StyledToken, tokenStarts []int, start, end, displayStart int, baseStyle lipgloss.Style, tabSize int) (string, int) {
	start = max(0, min(start, len(lineContent)))
	end = max(start, min(end, len(lineContent)))
	if start == end {
		return "", displayStart
	}
	if len(tokens) == 0 {
		return renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayStart, baseStyle, tabSize)
	}

	var sb strings.Builder
	pos := start
	displayPos := displayStart
	tokenIndex := 0
	tokenStart := 0
	if len(tokenStarts) == len(tokens) {
		tokenIndex = sort.Search(len(tokens), func(i int) bool {
			return tokenStarts[i]+len(tokens[i].Text) > start
		})
		if tokenIndex >= len(tokens) {
			if pos < end {
				rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, pos, end, displayPos, baseStyle, tabSize)
				return rendered, nextDisplay
			}
			return "", displayPos
		}
		tokenStart = tokenStarts[tokenIndex]
	}
	for ; tokenIndex < len(tokens); tokenIndex++ {
		token := tokens[tokenIndex]
		tokenEnd := tokenStart + len(token.Text)
		if tokenEnd <= start {
			tokenStart = tokenEnd
			continue
		}
		if tokenStart >= end {
			break
		}

		overlapStart := max(start, tokenStart)
		overlapEnd := min(end, tokenEnd)
		if pos < overlapStart {
			rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, pos, overlapStart, displayPos, baseStyle, tabSize)
			sb.WriteString(rendered)
			displayPos = nextDisplay
		}
		if overlapStart < overlapEnd {
			if _, noBackground := baseStyle.GetBackground().(lipgloss.NoColor); noBackground {
				// No background override, so the token's precomputed escape
				// sequences describe this render exactly.
				expanded, nextDisplay := expandTabsAtDisplayColumnWithEnd(
					lineContent[max(0, min(overlapStart, len(lineContent))):max(0, min(overlapEnd, len(lineContent)))],
					displayPos, tabSize)
				sb.WriteString(token.Render(expanded))
				displayPos = nextDisplay
			} else {
				style := token.Style.Background(baseStyle.GetBackground())
				rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, overlapStart, overlapEnd, displayPos, style, tabSize)
				sb.WriteString(rendered)
				displayPos = nextDisplay
			}
			pos = overlapEnd
		}
		tokenStart = tokenEnd
	}
	if pos < end {
		rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, pos, end, displayPos, baseStyle, tabSize)
		sb.WriteString(rendered)
		displayPos = nextDisplay
	}
	return sb.String(), displayPos
}

func renderStyledByteRangeWithTabsAtDisplay(lineContent string, start, end, displayStart int, style lipgloss.Style, tabSize int) (string, int) {
	start = max(0, min(start, len(lineContent)))
	end = max(start, min(end, len(lineContent)))
	expanded, displayEnd := expandTabsAtDisplayColumnWithEnd(lineContent[start:end], displayStart, tabSize)
	return style.Render(expanded), displayEnd
}

// wrapSegmentBounds returns the Nth segment and its byte offsets in line.
func wrapSegmentBounds(line string, segIdx, width int) (string, int, int) {
	return wrapSegmentBoundsWithTabs(line, segIdx, width, 4)
}

func wrapSegmentBoundsWithTabs(line string, segIdx, width, tabSize int) (string, int, int) {
	if width < 1 || segIdx < 0 {
		return "", 0, 0
	}
	startByte, endByte, _, ok := wrappedLineSegmentWithTabs(line, segIdx, width, tabSize)
	if !ok {
		return "", 0, 0
	}
	return line[startByte:endByte], startByte, endByte
}

// findBracketHighlights returns two positions to highlight and whether a match was found.
func (v *Viewport) findBracketHighlights(buf *text.Buffer) (text.Position, text.Position, bool) {
	cursor := buf.Cursor
	if v.bracketCacheRope == buf.Rope() && v.bracketCacheVersion == buf.Version() && v.bracketCacheCursor == cursor {
		return v.bracketCachePos1, v.bracketCachePos2, v.bracketCacheFound
	}
	pos1, pos2, found := findBracketHighlightsBounded(buf, cursor)
	v.bracketCacheRope = buf.Rope()
	v.bracketCacheVersion = buf.Version()
	v.bracketCacheCursor = cursor
	v.bracketCachePos1 = pos1
	v.bracketCachePos2 = pos2
	v.bracketCacheFound = found
	return pos1, pos2, found
}

func findBracketHighlightsBounded(buf *text.Buffer, cursor text.Position) (text.Position, text.Position, bool) {
	rope := buf.Rope()
	for _, pos := range []text.Position{cursor, {Line: cursor.Line, Col: cursor.Col - 1}} {
		if pos.Col < 0 {
			continue
		}
		offset, ok := rope.PositionToOffsetUncached(pos)
		if !ok {
			continue
		}
		ch, ok := rope.ByteAtSafe(offset)
		if !ok || (!IsOpenBracket(ch) && !IsCloseBracket(ch)) {
			continue
		}
		if match, ok := FindMatchingBracketWithinBudget(buf, pos, MaxBracketScanBytes); ok {
			return pos, match, true
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
		displayCol := displayColumn([]byte(lineContent), col, v.tabSize()) - v.ScrollX
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

	visualCol := 0
	for _, tok := range tokens {
		if widthLeft <= 0 {
			break
		}
		text := expandTabsAtDisplayColumn(tok.Text, visualCol, v.tabSize())
		visualCol += displayWidth(text)
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

		if isCursorLine {
			// The cursor line overrides the background, producing a style the
			// token's precomputed sequences do not describe. It is one row per
			// frame, so the slower path here costs little.
			sb.WriteString(tok.Style.Background(ui.Nord1).Render(text))
		} else {
			sb.WriteString(tok.Render(text))
		}
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

// selectionRange returns the byte range of a single selection overlapping a line.
// Returns (-1, -1) if no overlap.
// Deprecated: Use selectionRanges for multiple selections support.
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

// renderLineWithMultipleSelectionsTabs renders selections from raw byte
// ranges after deriving one display-only tab-expanded line. This is important:
// expanding each raw range independently would reset tab stops at the range
// boundary and shift subsequent text.
func (v *Viewport) renderLineWithMultipleSelectionsTabs(lineBytes []byte, ranges []selectionByteRange, isPrimaryLine bool, textWidth int, theme ui.Theme) string {
	displayed := expandTabsForDisplay(lineBytes, v.tabSize())
	visibleStart := v.ScrollX
	visibleEnd := visibleStart + textWidth
	lineWidth := displayWidth(displayed)
	if visibleEnd > lineWidth {
		visibleEnd = lineWidth
	}

	type visualRange struct{ start, end int }
	visualRanges := make([]visualRange, 0, len(ranges))
	for _, r := range ranges {
		start := displayColumn(lineBytes, r.start, v.tabSize())
		end := displayColumn(lineBytes, r.end, v.tabSize())
		if start < visibleEnd && end > visibleStart {
			visualRanges = append(visualRanges, visualRange{max(start, visibleStart), min(end, visibleEnd)})
		}
	}

	base := theme.Editor
	selected := theme.SecondarySelection
	if isPrimaryLine {
		base = theme.CursorLine
		selected = theme.Selection
	}

	var sb strings.Builder
	pos := visibleStart
	for _, r := range visualRanges {
		if pos < r.start {
			sb.WriteString(base.Render(sliceDisplayColumns(displayed, pos, r.start)))
		}
		if r.start < r.end {
			sb.WriteString(selected.Render(sliceDisplayColumns(displayed, r.start, r.end)))
		}
		pos = max(pos, r.end)
	}
	if pos < visibleEnd {
		sb.WriteString(base.Render(sliceDisplayColumns(displayed, pos, visibleEnd)))
	}
	if pad := textWidth - (visibleEnd - visibleStart); pad > 0 {
		sb.WriteString(base.Render(getSpaces(pad)))
	}
	return sb.String()
}

func sliceDisplayColumns(s string, start, end int) string {
	if end <= start {
		return ""
	}
	startByte := byteAtDisplayColumn(s, start)
	endByte := byteAtDisplayColumn(s, end)
	return s[startByte:endByte]
}

func byteAtDisplayColumn(s string, target int) int {
	if target <= 0 {
		return 0
	}
	col := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			break
		}
		width := max(0, runewidth.RuneWidth(r))
		if col+width > target {
			return i
		}
		col += width
		if col == target {
			return i + size
		}
		i += size
	}
	return len(s)
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
	return text.Position{Line: line, Col: byteColumnAtDisplay(lineContent, v.ScrollX+screenCol, v.tabSize())}
}

// ScreenToBufferPositionWrap maps screen coordinates to buffer position in word-wrap mode.
func (v *Viewport) ScreenToBufferPositionWrap(screenX, screenY int, buf *text.Buffer, gw int, wrap *WrapLayout) text.Position {
	// Convert screen Y to visual row relative to scroll position
	visualRow := v.wrapScrollY(wrap) + screenY
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

	lineContent := buf.Line(bufLine)
	segmentStart, segmentEnd, segmentStartDisplay, ok := wrap.SegmentBoundsForLine(bufLine, wrapOffset, lineContent)
	if !ok {
		return text.Position{Line: bufLine, Col: len(lineContent)}
	}

	// The segment is at most the viewport width, so this local width scan is
	// bounded and never depends on the length before the segment.
	_, segmentEndDisplay := expandTabsAtDisplayColumnWithEnd(string(lineContent[segmentStart:segmentEnd]), segmentStartDisplay, v.tabSize())
	target := min(segmentStartDisplay+screenCol, segmentEndDisplay)
	return text.Position{Line: bufLine, Col: byteColumnAtDisplayFrom(lineContent, segmentStart, segmentStartDisplay, target, v.tabSize())}
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
