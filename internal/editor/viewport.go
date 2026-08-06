package editor

import (
	"image/color"
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

	wrapStyleCacheValid bool
	wrapStyleCacheTheme ui.Theme
	wrapEditorPair      wrapStylePair
	wrapCursorPair      wrapStylePair
	wrapEditorBgPair    wrapStylePair
	wrapCursorBgPair    wrapStylePair

	bracketCacheRope    *text.Rope
	bracketCacheVersion int
	bracketCacheCursor  text.Position
	bracketCachePos1    text.Position
	bracketCachePos2    text.Position
	bracketCacheFound   bool

	// The rope intentionally returns a fresh slice for each Line call. Wrapped
	// rendering may show many rows from one very long logical line, and the
	// viewport is often rendered repeatedly without a buffer change. Retain
	// only that one immutable line so a large line is not copied once per frame.
	renderLineCacheRope    *text.Rope
	renderLineCacheVersion int
	renderLineCacheLine    int
	renderLineCache        []byte
	renderLineCacheContent string
}

type wrapStylePair struct {
	prefix string
	suffix string
	valid  bool
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

// HasNonEmpty reports whether line intersects a real selection without
// requiring the line's byte length. Calling Ranges for the same line remains
// O(1) because SelectionLineIterator retains its last result.
func (it *selectionRangeIterator) HasNonEmpty(line int) bool {
	if it == nil || it.selections == nil {
		return false
	}
	for _, selection := range it.selections.ForLine(line) {
		if !selection.IsEmpty() {
			return true
		}
	}
	return false
}

func (v *Viewport) tabSize() int {
	if v.TabSize == 0 {
		return 4
	}
	return normalizeTabSize(v.TabSize)
}

func (v *Viewport) wrapStylePairFor(theme ui.Theme, isCursorLine bool) (*wrapStylePair, *wrapStylePair) {
	if !v.wrapStyleCacheValid || v.wrapStyleCacheTheme != theme {
		v.wrapStyleCacheTheme = theme
		v.wrapEditorPair = newWrapStylePair(theme.Editor)
		v.wrapCursorPair = newWrapStylePair(theme.CursorLine)
		v.wrapEditorBgPair = newWrapStylePair(lipgloss.NewStyle().Background(theme.Editor.GetBackground()))
		v.wrapCursorBgPair = newWrapStylePair(lipgloss.NewStyle().Background(theme.CursorLine.GetBackground()))
		v.wrapStyleCacheValid = true
	}
	if isCursorLine {
		return &v.wrapCursorPair, &v.wrapCursorBgPair
	}
	return &v.wrapEditorPair, &v.wrapEditorBgPair
}

func newWrapStylePair(style lipgloss.Style) wrapStylePair {
	prefix, suffix, valid := styleRenderPair(style)
	return wrapStylePair{prefix: prefix, suffix: suffix, valid: valid}
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

// renderLine returns an immutable rope line and reuses the last line fetched
// for this viewport when the buffer identity is unchanged. Rope edits always
// produce a new root, while version covers snapshot replacements that may be
// represented by a different buffer state with the same logical line number.
func (v *Viewport) renderLine(buf *text.Buffer, line int) []byte {
	if buf == nil {
		return nil
	}
	rope := buf.Rope()
	version := buf.Version()
	if v.renderLineCacheRope == rope &&
		v.renderLineCacheVersion == version &&
		v.renderLineCacheLine == line {
		return v.renderLineCache
	}
	lineBytes := buf.Line(line)
	v.renderLineCacheRope = rope
	v.renderLineCacheVersion = version
	v.renderLineCacheLine = line
	v.renderLineCache = lineBytes
	v.renderLineCacheContent = ""
	return lineBytes
}

// renderLineContent is the string form used by token rendering. Keeping it
// beside the byte slice avoids converting a long immutable rope line on every
// frame after renderLine has already made the safe one-line cache hit.
func (v *Viewport) renderLineContent(buf *text.Buffer, line int) ([]byte, string) {
	lineBytes := v.renderLine(buf, line)
	if v.renderLineCacheContent == "" && len(lineBytes) > 0 {
		v.renderLineCacheContent = string(lineBytes)
	}
	return lineBytes, v.renderLineCacheContent
}

// Render renders the visible portion of the buffer with gutter, syntax highlighting, and diagnostics.
func (v *Viewport) Render(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts) string {
	return v.RenderWithFoldsHighlights(buf, theme, hl, diagnostics, gutterOpts, nil, nil)
}

// RenderHighlights renders a viewport with optional plugin highlight ranges.
func (v *Viewport) RenderHighlights(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, pluginHighlights []HighlightRange) string {
	return v.RenderWithFoldsHighlights(buf, theme, hl, diagnostics, gutterOpts, nil, pluginHighlights)
}

// RenderWithFolds renders the visible portion of the buffer with optional code folding.
func (v *Viewport) RenderWithFolds(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, folds *FoldState) string {
	return v.RenderWithFoldsHighlights(buf, theme, hl, diagnostics, gutterOpts, folds, nil)
}

// RenderWithFoldsHighlights renders the visible portion of the buffer with
// optional code folding and bounded plugin highlight ranges.
func (v *Viewport) RenderWithFoldsHighlights(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, folds *FoldState, pluginHighlights []HighlightRange) string {
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
	sb.Grow(max(0, v.Height*(textWidth+v.GutterWidth+16)))
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
			// Token-only rows render directly from the highlighter cache. Defer
			// copying rope bytes until a selection, plugin highlight, bracket, or
			// plain-text branch actually inspects the line content.
			var lineBytes []byte
			lineLoaded := false
			var selectionRanges []selectionByteRange
			if selectionIterator.HasNonEmpty(line) {
				lineBytes = v.renderLine(buf, line)
				lineLoaded = true
				selectionRanges = selectionIterator.Ranges(line, len(lineBytes))
			}
			hasSelection := len(selectionRanges) > 0

			var lineHighlights []HighlightRange
			if hasPluginHighlightForLine(pluginHighlights, line) {
				if !lineLoaded {
					lineBytes = v.renderLine(buf, line)
					lineLoaded = true
				}
				lineHighlights = pluginHighlightRangesForLine(pluginHighlights, line, len(lineBytes))
			}

			// Check for syntax highlighting tokens
			var tokens []highlight.StyledToken
			if hl != nil {
				tokens = hl.Line(line)
			}

			if hasSelection {
				sb.WriteString(v.renderLineWithMultipleSelectionsTabs(lineBytes, selectionRanges, line == buf.Selections.PrimaryCursor().Line, textWidth, theme))
			} else if len(lineHighlights) > 0 {
				_, lineContent := v.renderLineContent(buf, line)
				rendered := v.renderLineWithHighlights(lineContent, tokens, lineHighlights, line == buf.Cursor.Line, textWidth, theme)
				if hasBracketMatch {
					rendered = v.applyBracketHighlight(rendered, lineContent, line, bracketPos1, bracketPos2, textWidth, theme)
				}
				sb.WriteString(rendered)
			} else if len(tokens) > 0 {
				if hasBracketMatch && (bracketPos1.Line == line || bracketPos2.Line == line) {
					_, lineContent := v.renderLineContent(buf, line)
					rendered := v.renderLineWithTokens(tokens, line == buf.Selections.PrimaryCursor().Line, textWidth, theme)
					rendered = v.applyBracketHighlight(rendered, lineContent, line, bracketPos1, bracketPos2, textWidth, theme)
					sb.WriteString(rendered)
				} else {
					v.renderLineWithTokensInto(&sb, tokens, line == buf.Selections.PrimaryCursor().Line, textWidth, theme)
				}
			} else {
				// plain text rendering
				if !lineLoaded {
					lineBytes = v.renderLine(buf, line)
				}
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

func pluginHighlightRangesForLine(ranges []HighlightRange, line, lineLen int) []HighlightRange {
	if len(ranges) == 0 {
		return nil
	}
	var selected []HighlightRange
	for _, highlight := range ranges {
		if highlight.Line != line {
			continue
		}
		start := max(0, min(highlight.StartCol, lineLen))
		end := max(start, min(highlight.EndCol, lineLen))
		if start >= end {
			continue
		}
		highlight.StartCol = start
		highlight.EndCol = end
		selected = append(selected, highlight)
	}
	return selected
}

func hasPluginHighlightForLine(ranges []HighlightRange, line int) bool {
	for _, highlight := range ranges {
		if highlight.Line == line {
			return true
		}
	}
	return false
}

func inheritedPluginHighlightStyle(style, base lipgloss.Style) lipgloss.Style {
	if _, noForeground := style.GetForeground().(lipgloss.NoColor); noForeground {
		if foreground := base.GetForeground(); foreground != nil {
			style = style.Foreground(foreground)
		}
	}
	if _, noBackground := style.GetBackground().(lipgloss.NoColor); noBackground {
		if background := base.GetBackground(); background != nil {
			style = style.Background(background)
		}
	}
	return style
}

func (v *Viewport) renderLineWithHighlights(lineContent string, tokens []highlight.StyledToken, ranges []HighlightRange, isCursorLine bool, textWidth int, theme ui.Theme) string {
	baseStyle := theme.Editor
	if isCursorLine {
		baseStyle = theme.CursorLine
	}
	lineBytes := []byte(lineContent)
	visibleStart := byteColumnAtDisplay(lineBytes, v.ScrollX, v.tabSize())
	visibleEnd := byteColumnAtDisplay(lineBytes, v.ScrollX+textWidth, v.tabSize())
	tokenStarts := tokenByteStarts(tokens)
	var sb strings.Builder
	pos := visibleStart
	displayPos := displayColumn(lineBytes, visibleStart, v.tabSize())
	for _, highlight := range ranges {
		start := max(pos, min(highlight.StartCol, visibleEnd))
		end := max(start, min(highlight.EndCol, visibleEnd))
		if start >= end {
			continue
		}
		if pos < start {
			rendered, nextDisplay := renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent, tokens, tokenStarts, pos, start, displayPos, baseStyle, v.tabSize())
			sb.WriteString(rendered)
			displayPos = nextDisplay
		}
		style := inheritedPluginHighlightStyle(highlight.Style, baseStyle)
		rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayPos, style, v.tabSize())
		sb.WriteString(rendered)
		displayPos = nextDisplay
		pos = end
	}
	if pos < visibleEnd {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexed(lineContent, tokens, tokenStarts, pos, visibleEnd, displayPos, baseStyle, v.tabSize())
		sb.WriteString(rendered)
	}
	truncated := ansi.Truncate(sb.String(), textWidth, "")
	padLen := max(0, textWidth-ansi.StringWidth(truncated))
	if padLen > 0 {
		truncated += baseStyle.Render(getSpaces(padLen))
	}
	return truncated
}

// RenderWithWrap renders the viewport with word wrap enabled.
func (v *Viewport) RenderWithWrap(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, wrap *WrapLayout) string {
	return v.RenderWithWrapHighlights(buf, theme, hl, diagnostics, gutterOpts, wrap, nil)
}

// RenderWithWrapHighlights renders word-wrapped content with optional plugin
// highlight ranges.
func (v *Viewport) RenderWithWrapHighlights(buf *text.Buffer, theme ui.Theme, hl *highlight.Highlighter, diagnostics []Diagnostic, gutterOpts *GutterOpts, wrap *WrapLayout, pluginHighlights []HighlightRange) string {
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
	var lineHighlights []HighlightRange
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
				lineBytes, lineContent = v.renderLineContent(buf, bufLine)
				lineTokens = nil
				lineTokenStarts = nil
				if hl != nil {
					lineTokens = hl.Line(bufLine)
					lineTokenStarts = v.wrapTokenStarts(bufLine, buf.Version(), lineTokens)
				}
				lineHighlights = pluginHighlightRangesForLine(pluginHighlights, bufLine, len(lineContent))
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

			// Selections take precedence over plugin and syntax highlighting.
			var rendered string
			if len(lineSelectionRanges) > 0 {
				rendered = v.renderWrapSegmentWithSelections(theme, lineTokens, lineTokenStarts, bufLine == buf.Cursor.Line, lineContent, lineSelectionRanges, segmentStart, segmentEnd, segmentDisplayStart)
			} else {
				rendered = v.renderWrapSegmentWithHighlights(theme, lineTokens, lineTokenStarts, bufLine == buf.Cursor.Line, lineContent, lineHighlights, segmentStart, segmentEnd, segmentDisplayStart)
			}
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
	var basePair, backgroundPair *wrapStylePair
	if len(tokens) > 0 {
		basePair, backgroundPair = v.wrapStylePairFor(theme, isCursorLine)
	}

	if !hasOverlap {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, segmentStart, segmentEnd, segmentDisplayStart, baseStyle, v.tabSize(), basePair, backgroundPair)
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
			rendered, nextDisplay := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, pos, start, displayPos, baseStyle, v.tabSize(), basePair, backgroundPair)
			sb.WriteString(rendered)
			displayPos = nextDisplay
		}
		rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayPos, selectionStyle, v.tabSize())
		sb.WriteString(rendered)
		displayPos = nextDisplay
		pos = end
	}
	if pos < segmentEnd {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, pos, segmentEnd, displayPos, baseStyle, v.tabSize(), basePair, backgroundPair)
		sb.WriteString(rendered)
	}
	return sb.String()
}

func (v *Viewport) renderWrapSegmentWithHighlights(theme ui.Theme, tokens []highlight.StyledToken, tokenStarts []int, isCursorLine bool, lineContent string, ranges []HighlightRange, segmentStart, segmentEnd, segmentDisplayStart int) string {
	baseStyle := theme.Editor
	if isCursorLine {
		baseStyle = theme.CursorLine
	}
	var basePair, backgroundPair *wrapStylePair
	if len(tokens) > 0 {
		// Reuse the same stable SGR pairs as the selection path. Rebuilding
		// them for every wrapped row calls lipgloss several times per frame,
		// even though the theme and cursor-line state have not changed.
		basePair, backgroundPair = v.wrapStylePairFor(theme, isCursorLine)
	}
	if len(ranges) == 0 {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, segmentStart, segmentEnd, segmentDisplayStart, baseStyle, v.tabSize(), basePair, backgroundPair)
		return rendered
	}
	var sb strings.Builder
	pos := segmentStart
	displayPos := segmentDisplayStart
	for _, highlight := range ranges {
		start := max(pos, max(segmentStart, highlight.StartCol))
		end := min(segmentEnd, highlight.EndCol)
		if start >= end {
			continue
		}
		if pos < start {
			rendered, nextDisplay := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, pos, start, displayPos, baseStyle, v.tabSize(), basePair, backgroundPair)
			sb.WriteString(rendered)
			displayPos = nextDisplay
		}
		style := inheritedPluginHighlightStyle(highlight.Style, baseStyle)
		rendered, nextDisplay := renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayPos, style, v.tabSize())
		sb.WriteString(rendered)
		displayPos = nextDisplay
		pos = end
	}
	if pos < segmentEnd {
		rendered, _ := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, pos, segmentEnd, displayPos, baseStyle, v.tabSize(), basePair, backgroundPair)
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
	return renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent, tokens, tokenStarts, start, end, displayStart, baseStyle, tabSize, nil, nil)
}

func renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(lineContent string, tokens []highlight.StyledToken, tokenStarts []int, start, end, displayStart int, baseStyle lipgloss.Style, tabSize int, basePair, backgroundPair *wrapStylePair) (string, int) {
	start = max(0, min(start, len(lineContent)))
	end = max(start, min(end, len(lineContent)))
	if start == end {
		return "", displayStart
	}
	if len(tokens) == 0 {
		return renderStyledByteRangeWithTabsAtDisplay(lineContent, start, end, displayStart, baseStyle, tabSize)
	}

	// Word-wrap normally renders a syntax token with the row's background.
	// Calling lipgloss Style.Render for every token is disproportionately
	// expensive, though: the token style is already cached and the row style
	// only needs to be reopened after each token's reset sequence. The fast
	// path emits that row SGR pair once and keeps the old implementation for
	// token styles that carry their own background.
	baseBackground := baseStyle.GetBackground()
	if _, noBackground := baseBackground.(lipgloss.NoColor); !noBackground {
		if compatible, needsBackgroundOverride := tokenRangeBackgroundCompatible(tokens, tokenStarts, start, end, baseBackground); compatible {
			return renderTokenByteRangeWithTabsAtDisplayIndexedFast(lineContent, tokens, tokenStarts, start, end, displayStart, baseStyle, tabSize, needsBackgroundOverride, basePair, backgroundPair)
		}
	}

	return renderTokenByteRangeWithTabsAtDisplayIndexedLegacy(lineContent, tokens, tokenStarts, start, end, displayStart, baseStyle, tabSize)
}

// tokenRangeBackgroundCompatible reports whether all syntax tokens intersecting
// a range either leave the row background alone or already use the same one.
// Punctuation tokens may inherit the editor style, so checking only for
// NoColor would unnecessarily send most ordinary lines through the slow path.
func tokenRangeBackgroundCompatible(tokens []highlight.StyledToken, tokenStarts []int, start, end int, baseBackground color.Color) (bool, bool) {
	needsBackgroundOverride := false
	tokenIndex := 0
	tokenStart := 0
	hasStarts := len(tokenStarts) == len(tokens)
	if hasStarts {
		tokenIndex = sort.Search(len(tokens), func(i int) bool {
			return tokenStarts[i]+len(tokens[i].Text) > start
		})
		if tokenIndex >= len(tokens) {
			return true, false
		}
		tokenStart = tokenStarts[tokenIndex]
	}
	for i := tokenIndex; i < len(tokens); i++ {
		token := tokens[i]
		if !hasStarts && i > 0 {
			tokenStart += len(tokens[i-1].Text)
		}
		tokenEnd := tokenStart + len(token.Text)
		if tokenEnd <= start {
			if hasStarts {
				tokenStart = tokenEnd
			}
			continue
		}
		if tokenStart >= end {
			break
		}
		tokenBackground := token.Style.GetBackground()
		if _, noBackground := tokenBackground.(lipgloss.NoColor); !noBackground && !sameColor(tokenBackground, baseBackground) && !token.FastSGR {
			return false, false
		}
		if _, noBackground := tokenBackground.(lipgloss.NoColor); !noBackground && !sameColor(tokenBackground, baseBackground) {
			needsBackgroundOverride = true
		}
		if hasStarts {
			tokenStart = tokenEnd
		}
	}
	return true, needsBackgroundOverride
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// renderTokenByteRangeWithTabsAtDisplayIndexedFast is the wrapped-rendering
// path for ordinary syntax tokens. It writes token SGRs directly and reopens
// the row style around each token, which preserves the background after a
// token's reset while avoiding one lipgloss pipeline per token.
func renderTokenByteRangeWithTabsAtDisplayIndexedFast(lineContent string, tokens []highlight.StyledToken, tokenStarts []int, start, end, displayStart int, baseStyle lipgloss.Style, tabSize int, needsBackgroundOverride bool, basePair, backgroundPair *wrapStylePair) (string, int) {
	if basePair == nil {
		pair := newWrapStylePair(baseStyle)
		basePair = &pair
	}
	if !basePair.valid {
		// Theme row styles are deliberately simple, but if a caller supplies a
		// transforming or layout style, preserve the old exact behavior.
		return renderTokenByteRangeWithTabsAtDisplayIndexedLegacy(lineContent, tokens, tokenStarts, start, end, displayStart, baseStyle, tabSize)
	}
	baseBackground := baseStyle.GetBackground()
	var backgroundPrefix string
	if needsBackgroundOverride {
		if backgroundPair == nil {
			pair := newWrapStylePair(lipgloss.NewStyle().Background(baseBackground))
			backgroundPair = &pair
		}
		if !backgroundPair.valid {
			return renderTokenByteRangeWithTabsAtDisplayIndexedLegacy(lineContent, tokens, tokenStarts, start, end, displayStart, baseStyle, tabSize)
		}
		backgroundPrefix = backgroundPair.prefix
	}
	basePrefix, baseSuffix := basePair.prefix, basePair.suffix

	var sb strings.Builder
	// The source bytes are a useful lower bound; ANSI prefixes add a small
	// amount per token but avoiding an exact calculation keeps this helper
	// allocation-free beyond its output buffer.
	sb.Grow(end - start)
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
				return renderStyledByteRangeWithTabsAtDisplay(lineContent, pos, end, displayPos, baseStyle, tabSize)
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
			displayPos = writeWrappedPlainRange(&sb, basePrefix, baseSuffix, lineContent, pos, overlapStart, displayPos, tabSize)
		}
		if overlapStart < overlapEnd {
			expanded, nextDisplay := expandTabsAtDisplayColumnWithEnd(
				lineContent[max(0, min(overlapStart, len(lineContent))):max(0, min(overlapEnd, len(lineContent)))],
				displayPos,
				tabSize,
			)
			sb.WriteString(basePrefix)
			tokenBackground := token.Style.GetBackground()
			if token.FastSGR {
				if _, noBackground := tokenBackground.(lipgloss.NoColor); noBackground || sameColor(tokenBackground, baseBackground) {
					token.WriteTo(&sb, expanded)
				} else {
					// The token already has constant SGR pieces. Reopen the row
					// background after its prefix so inherited editor backgrounds
					// are replaced by the active cursor-line background.
					sb.WriteString(token.Prefix)
					sb.WriteString(backgroundPrefix)
					sb.WriteString(expanded)
					sb.WriteString(token.Suffix)
				}
			} else {
				token.WriteTo(&sb, expanded)
			}
			sb.WriteString(baseSuffix)
			displayPos = nextDisplay
			pos = overlapEnd
		}
		tokenStart = tokenEnd
	}
	if pos < end {
		displayPos = writeWrappedPlainRange(&sb, basePrefix, baseSuffix, lineContent, pos, end, displayPos, tabSize)
	}
	return sb.String(), displayPos
}

// renderTokenByteRangeWithTabsAtDisplayIndexedLegacy keeps the pre-optimized
// implementation available for styles that cannot be represented as a stable
// prefix/suffix pair.
func renderTokenByteRangeWithTabsAtDisplayIndexedLegacy(lineContent string, tokens []highlight.StyledToken, tokenStarts []int, start, end, displayStart int, baseStyle lipgloss.Style, tabSize int) (string, int) {
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
				return renderStyledByteRangeWithTabsAtDisplay(lineContent, pos, end, displayPos, baseStyle, tabSize)
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

// writeWrappedPlainRange writes an unhighlighted range with the row style.
// The caller has already validated that the style has a stable SGR pair.
func writeWrappedPlainRange(sb *strings.Builder, prefix, suffix, lineContent string, start, end, displayStart, tabSize int) int {
	expanded, displayEnd := expandTabsAtDisplayColumnWithEnd(lineContent[start:end], displayStart, tabSize)
	sb.WriteString(prefix)
	sb.WriteString(expanded)
	sb.WriteString(suffix)
	return displayEnd
}

// styleRenderPair extracts the constant SGR wrapper from a simple row style.
// It intentionally rejects text-dependent styles so callers can fall back to
// lipgloss's full renderer without a visual regression.
func styleRenderPair(style lipgloss.Style) (string, string, bool) {
	const sentinel = "\x00\x01teak-viewport-style\x01\x00"
	rendered := style.Render(sentinel)
	marker := strings.Index(rendered, sentinel)
	if marker < 0 {
		return "", "", false
	}
	prefix := rendered[:marker]
	suffix := rendered[marker+len(sentinel):]
	for _, probe := range []string{"", "a", "hello world"} {
		if prefix+probe+suffix != style.Render(probe) {
			return "", "", false
		}
	}
	return prefix, suffix, true
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
	v.renderLineWithTokensInto(&sb, tokens, isCursorLine, textWidth, theme)
	return sb.String()
}

func (v *Viewport) renderLineWithTokensInto(sb *strings.Builder, tokens []highlight.StyledToken, isCursorLine bool, textWidth int, theme ui.Theme) {
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
			tok.WriteTo(sb, text)
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
