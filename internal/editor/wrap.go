package editor

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

const (
	// wrapBlockLines is deliberately small: a fresh viewport needs at most one
	// block scan, while a document with one million short lines initially
	// allocates only a few thousand block descriptors (not a row per line).
	wrapBlockLines = 256

	// maxWrapSegmentWindow bounds the only per-visual-row state retained by a
	// WrapLayout. A layout never allocates one segment object per terminal row.
	maxWrapSegmentWindow = 512

	// maxWrapResidentBlocks caps hydrated per-line row pages. Exact block
	// totals remain after eviction, so deep mapping stays correct without
	// retaining an int for every line after a full-file visit.
	maxWrapResidentBlocks = 64
)

// wrapSegmentStart is a compact index entry for one visual row. End offsets
// are implicit: they are the next entry in the active window (or the source
// line length for its final entry).
type wrapSegmentStart struct {
	byteOffset int
	displayCol int
}

// wrapBlock is a lazily measured page of logical lines. Until a page is
// needed, totalRows is its lower-bound estimate (one row per logical line) and
// rows remains nil. Once measured, rows provides exact local deep scrolling.
type wrapBlock struct {
	firstLine int
	lineCount int
	rows      []int
	totalRows int
	known     bool
	rowAccess uint64
}

// WrapLayout maps logical lines to visual rows for word wrap. It uses a
// paginated sparse index: construction and resize allocate block descriptors
// only, then measure text pages as the viewport, cursor, selection or mouse
// actually reaches them. Segment boundaries are a separate bounded window.
//
// This means opening/resizing a 64 MiB document with a million lines neither
// materializes every visual row nor calls Buffer.Line for every logical line.
// A direct jump to a deep row intentionally fills the pages before that row,
// because exact mapping requires their row totals; normal wheel scrolling
// fills just the pages crossed.
type WrapLayout struct {
	lineGetter func(int) []byte
	lineCount  int
	blocks     []wrapBlock

	width      int // text width (invalidated on resize)
	tabSize    int
	degraded   bool
	buildCount int

	// segmentStarts holds one bounded window for one logical line. The final
	// entry may be a successor sentinel for the last actual cached row.
	segmentStarts      []wrapSegmentStart
	segmentWindowLine  int
	segmentWindowFirst int // inclusive visual row within segmentWindowLine
	segmentWindowLast  int // exclusive visual row within segmentWindowLine

	rowAccessTick uint64
	residentRows  int
}

// NewWrapLayout creates a lazily paginated WrapLayout for a buffer at width.
func NewWrapLayout(lineGetter func(int) []byte, lineCount, width int) *WrapLayout {
	return NewWrapLayoutWithTabSize(lineGetter, lineCount, width, 4)
}

// NewWrapLayoutWithTabSize accounts for display-only tab expansion while
// retaining raw byte offsets for cursor and mouse mapping.
func NewWrapLayoutWithTabSize(lineGetter func(int) []byte, lineCount, width, tabSize int) *WrapLayout {
	w := &WrapLayout{tabSize: normalizeTabSize(tabSize)}
	w.rebuild(lineGetter, lineCount, width)
	return w
}

func (w *WrapLayout) rebuild(lineGetter func(int) []byte, lineCount, width int) {
	if width < 1 {
		width = 1
	}
	lineCount = max(0, lineCount)
	w.width = width
	w.buildCount++
	w.lineGetter = lineGetter
	w.lineCount = lineCount
	w.degraded = lineGetter == nil
	w.resetSegmentWindow()
	w.rowAccessTick = 0
	w.residentRows = 0
	if w.degraded {
		w.blocks = nil
		return
	}

	blockCount := 0
	if lineCount > 0 {
		blockCount = (lineCount + wrapBlockLines - 1) / wrapBlockLines
	}
	w.blocks = make([]wrapBlock, blockCount)
	for i := range w.blocks {
		first := i * wrapBlockLines
		count := min(wrapBlockLines, lineCount-first)
		w.blocks[i] = wrapBlock{firstLine: first, lineCount: count, totalRows: count}
	}
}

func (w *WrapLayout) resetSegmentWindow() {
	w.segmentStarts = nil
	w.segmentWindowLine = -1
	w.segmentWindowFirst = 0
	w.segmentWindowLast = 0
}

func (w *WrapLayout) blockIndex(line int) int {
	if line < 0 || line >= w.lineCount {
		return -1
	}
	return line / wrapBlockLines
}

// ensureBlockTotal guarantees an exact row total while retaining the rows only
// when the page was already hydrated. It avoids re-reading evicted pages when
// a caller merely needs a prefix sum.
func (w *WrapLayout) ensureBlockTotal(index int) *wrapBlock {
	if w == nil || index < 0 || index >= len(w.blocks) || w.lineGetter == nil {
		return nil
	}
	b := &w.blocks[index]
	if b.known {
		return b
	}
	return w.ensureBlockRows(index)
}

// ensureBlockRows hydrates one page only. It is the sole place where a sparse
// layout calls lineGetter for an already-known page, keeping SetSize and
// incremental invalidation cheap.
func (w *WrapLayout) ensureBlockRows(index int) *wrapBlock {
	if w == nil || index < 0 || index >= len(w.blocks) || w.lineGetter == nil {
		return nil
	}
	b := &w.blocks[index]
	if b.known && b.rows != nil {
		w.touchBlockRows(index)
		return b
	}

	rows := b.rows
	if cap(rows) < b.lineCount {
		rows = make([]int, b.lineCount)
		w.residentRows++
	} else {
		rows = rows[:b.lineCount]
		if b.rows == nil {
			w.residentRows++
		}
	}
	total := 0
	for i := range rows {
		rows[i] = wrappedLineRowsWithTabs(w.lineGetter(b.firstLine+i), w.width, w.tabSize)
		total += rows[i]
	}
	b.rows = rows
	b.totalRows = total
	b.known = true
	w.touchBlockRows(index)
	w.evictBlockRows(index)
	return b
}

func (w *WrapLayout) touchBlockRows(index int) {
	if index < 0 || index >= len(w.blocks) || w.blocks[index].rows == nil {
		return
	}
	w.rowAccessTick++
	w.blocks[index].rowAccess = w.rowAccessTick
}

func (w *WrapLayout) evictBlockRows(protected int) {
	for w.residentRows > maxWrapResidentBlocks {
		candidate := -1
		var oldest uint64
		for i := range w.blocks {
			if i == protected || w.blocks[i].rows == nil {
				continue
			}
			if candidate < 0 || w.blocks[i].rowAccess < oldest {
				candidate, oldest = i, w.blocks[i].rowAccess
			}
		}
		if candidate < 0 {
			return
		}
		w.blocks[candidate].rows = nil
		w.blocks[candidate].rowAccess = 0
		w.residentRows--
	}
}

func (w *WrapLayout) lineRows(bufLine int) int {
	index := w.blockIndex(bufLine)
	b := w.ensureBlockRows(index)
	if b == nil {
		return 1
	}
	return b.rows[bufLine-b.firstLine]
}

func (w *WrapLayout) blockTotalEstimate(block wrapBlock) int {
	if block.known {
		return block.totalRows
	}
	return block.lineCount
}

// TotalRows returns an exact total after every block has been reached, and a
// safe lower-bound estimate otherwise. Callers that must clamp at EOF should
// use TotalRowsKnown first; renderers use this estimate for a stable scrollbar
// without forcing a whole-document scan.
func (w *WrapLayout) TotalRows() int {
	if w == nil {
		return 0
	}
	total := 0
	for _, block := range w.blocks {
		total += w.blockTotalEstimate(block)
	}
	return total
}

// TotalRowsKnown reports whether TotalRows is exact without performing any
// new measurement.
func (w *WrapLayout) TotalRowsKnown() bool {
	if w == nil {
		return true
	}
	for _, block := range w.blocks {
		if !block.known {
			return false
		}
	}
	return true
}

// HasMoreRowsThan determines scrollbar eligibility while measuring only the
// prefix required to exceed limit. It never scans a large document merely to
// decide that its first screen needs a scrollbar.
func (w *WrapLayout) HasMoreRowsThan(limit int) bool {
	if w == nil || limit < 0 {
		return w != nil
	}
	total := 0
	for i := range w.blocks {
		b := w.ensureBlockTotal(i)
		if b == nil {
			return false
		}
		total += b.totalRows
		if total > limit {
			return true
		}
	}
	return false
}

// cacheSegmentWindow builds a bounded segment index around requested. It is
// called only from rendering, hit testing, and cursor work; layout creation
// only allocates sparse block descriptors.
func (w *WrapLayout) cacheSegmentWindow(bufLine, requested int, line []byte) {
	if bufLine < 0 || bufLine >= w.lineCount {
		w.resetSegmentWindow()
		return
	}
	rows := w.lineRows(bufLine)
	requested = max(0, min(requested, rows-1))
	first := max(0, requested-maxWrapSegmentWindow/4)
	last := min(rows, first+maxWrapSegmentWindow)
	if last-first < maxWrapSegmentWindow && first > 0 {
		first = max(0, last-maxWrapSegmentWindow)
	}

	// Add one row after the window when available, as the end boundary for the
	// final actual row. Thus the cache has at most window+1 entries.
	captureEnd := last
	if captureEnd < rows {
		captureEnd++
	}
	starts := make([]wrapSegmentStart, 0, captureEnd-first)
	appendStart := func(row, byteOffset, displayCol int) {
		if row >= first && row < captureEnd {
			starts = append(starts, wrapSegmentStart{byteOffset: byteOffset, displayCol: displayCol})
		}
	}

	appendStart(0, 0, 0)
	row := 0
	rowWidth := 0
	displayCol := 0
	for i := 0; i < len(line); {
		start := i
		runeWidth, size := wrapRuneWidth(line[i:], displayCol, w.width, w.tabSize)
		i += size
		if rowWidth > 0 && rowWidth+runeWidth > w.width {
			row++
			appendStart(row, start, displayCol)
			rowWidth = 0
		}
		rowWidth += runeWidth
		displayCol += runeWidth
	}
	if len(starts) == 0 {
		starts = append(starts, wrapSegmentStart{})
		first, last = 0, 1
	}
	w.segmentStarts = starts
	w.segmentWindowLine = bufLine
	w.segmentWindowFirst = first
	w.segmentWindowLast = last
}

func (w *WrapLayout) segmentBoundsForLine(bufLine, wrapOffset int, line []byte) (start, end, displayStart int, ok bool) {
	if bufLine < 0 || bufLine >= w.lineCount || wrapOffset < 0 || wrapOffset >= w.lineRows(bufLine) {
		return 0, 0, 0, false
	}
	if w.segmentWindowLine != bufLine || wrapOffset < w.segmentWindowFirst || wrapOffset >= w.segmentWindowLast {
		w.cacheSegmentWindow(bufLine, wrapOffset, line)
	}
	idx := wrapOffset - w.segmentWindowFirst
	if idx < 0 || idx >= len(w.segmentStarts) {
		return 0, 0, 0, false
	}
	entry := w.segmentStarts[idx]
	start, displayStart = entry.byteOffset, entry.displayCol
	if wrapOffset+1 >= w.lineRows(bufLine) {
		return start, len(line), displayStart, true
	}
	if idx+1 < len(w.segmentStarts) {
		return start, w.segmentStarts[idx+1].byteOffset, displayStart, true
	}

	// cacheSegmentWindow normally adds a successor sentinel. Keep a stateless
	// fallback for robustness should a line getter change between calls.
	_, end, _, ok = wrappedLineSegmentWithTabs(string(line), wrapOffset, w.width, w.tabSize)
	if !ok {
		return 0, 0, 0, false
	}
	return start, end, displayStart, true
}

// SegmentBounds returns the raw byte range and absolute display column for a
// visual row. Compatibility callers fetch a line only on a cache miss.
func (w *WrapLayout) SegmentBounds(bufLine, wrapOffset, lineLen int) (start, end, displayStart int, ok bool) {
	if w == nil || w.lineGetter == nil {
		return 0, 0, 0, false
	}
	line := w.lineGetter(bufLine)
	if lineLen >= 0 && len(line) > lineLen {
		line = line[:lineLen]
	}
	return w.segmentBoundsForLine(bufLine, wrapOffset, line)
}

// SegmentBoundsForLine is the allocation-friendly form used by the viewport,
// which already has the logical line materialized for rendering.
func (w *WrapLayout) SegmentBoundsForLine(bufLine, wrapOffset int, line []byte) (start, end, displayStart int, ok bool) {
	if w == nil {
		return 0, 0, 0, false
	}
	return w.segmentBoundsForLine(bufLine, wrapOffset, line)
}

// PositionForByte maps a raw byte column to a wrapped row and its column
// within that row. It uses the active window when possible and builds one
// after an out-of-window lookup so normal cursor movement stays local.
func (w *WrapLayout) PositionForByte(bufLine, byteCol int, line []byte) (wrapOffset, displayCol int) {
	if w == nil || bufLine < 0 || bufLine >= w.lineCount {
		return 0, 0
	}
	rows := w.lineRows(bufLine)
	byteCol = max(0, min(byteCol, len(line)))
	if w.segmentWindowLine == bufLine && len(w.segmentStarts) > 0 {
		actualCount := w.segmentWindowLast - w.segmentWindowFirst
		if actualCount > 0 && actualCount <= len(w.segmentStarts) {
			firstByte := w.segmentStarts[0].byteOffset
			lastByte := len(line)
			if w.segmentWindowLast < rows && len(w.segmentStarts) > actualCount {
				lastByte = w.segmentStarts[actualCount].byteOffset
			}
			if byteCol >= firstByte && (byteCol < lastByte || (w.segmentWindowLast == rows && byteCol == lastByte)) {
				idx := sort.Search(actualCount, func(i int) bool {
					return w.segmentStarts[i].byteOffset > byteCol
				}) - 1
				if idx >= 0 {
					entry := w.segmentStarts[idx]
					endDisplay := advanceDisplayColumn(line, entry.byteOffset, byteCol, entry.displayCol, w.tabSize)
					return w.segmentWindowFirst + idx, endDisplay - entry.displayCol
				}
			}
		}
	}

	wrapOffset, displayCol = wrappedPositionBytesWithTabs(line, byteCol, w.width, w.tabSize)
	w.cacheSegmentWindow(bufLine, wrapOffset, line)
	return wrapOffset, displayCol
}

// wrappedLineRowsWithTabs counts visual rows without allocating a string or
// retaining segment boundaries. It is used only when a sparse page is reached.
func wrappedLineRowsWithTabs(line []byte, width, tabSize int) int {
	if width < 1 {
		width = 1
	}
	if len(line) == 0 {
		return 1
	}
	rows := 1
	rowWidth := 0
	displayCol := 0
	for i := 0; i < len(line); {
		runeWidth, size := wrapRuneWidth(line[i:], displayCol, width, tabSize)
		i += size
		if rowWidth > 0 && rowWidth+runeWidth > width {
			rows++
			rowWidth = 0
		}
		rowWidth += runeWidth
		displayCol += runeWidth
	}
	return rows
}

// wrapRuneWidth consumes one tab or rune and returns its display width. It
// preserves malformed-UTF-8 progress and clips an over-wide rune to one row.
func wrapRuneWidth(line []byte, displayCol, width, tabSize int) (runeWidth, size int) {
	if len(line) == 0 {
		return 0, 0
	}
	if line[0] == '\t' {
		runeWidth, size = tabWidth(displayCol, tabSize), 1
	} else {
		r, decoded := utf8.DecodeRune(line)
		if decoded < 1 {
			decoded = 1
		}
		runeWidth, size = runewidth.RuneWidth(r), decoded
	}
	if runeWidth < 0 {
		runeWidth = 0
	}
	if runeWidth > width {
		runeWidth = width
	}
	return runeWidth, size
}

func wrappedLineSegmentWithTabs(line string, segment, width, tabSize int) (start, end, rows int, ok bool) {
	if width < 1 {
		width = 1
	}
	if line == "" {
		return 0, 0, 1, segment == 0
	}

	row := 0
	rowStart := 0
	rowWidth := 0
	logicalCol := 0
	for i := 0; i < len(line); {
		startByte := i
		runeWidth := 0
		if line[i] == '\t' {
			runeWidth = tabWidth(logicalCol, tabSize)
			i++
		} else {
			r, size := utf8.DecodeRuneInString(line[i:])
			if size < 1 {
				size = 1
			}
			i += size
			runeWidth = runewidth.RuneWidth(r)
		}
		if runeWidth < 0 {
			runeWidth = 0
		}
		if runeWidth > width {
			runeWidth = width
		}
		if rowWidth > 0 && rowWidth+runeWidth > width {
			if row == segment {
				return rowStart, startByte, 0, true
			}
			row++
			rowStart = startByte
			rowWidth = 0
		}
		rowWidth += runeWidth
		logicalCol += runeWidth
	}

	rows = row + 1
	if row == segment {
		return rowStart, len(line), rows, true
	}
	return 0, 0, rows, false
}

func wrappedPositionWithTabs(line string, byteCol, width, tabSize int) (row, displayCol int) {
	return wrappedPositionBytesWithTabs([]byte(line), byteCol, width, tabSize)
}

func wrappedPositionBytesWithTabs(line []byte, byteCol, width, tabSize int) (row, displayCol int) {
	if width < 1 {
		width = 1
	}
	byteCol = max(0, min(byteCol, len(line)))

	logicalCol := 0
	for i := 0; i < len(line); {
		startByte := i
		runeWidth, size := wrapRuneWidth(line[i:], logicalCol, width, tabSize)
		i += size
		if displayCol > 0 && displayCol+runeWidth > width {
			row++
			displayCol = 0
		}
		if startByte >= byteCol {
			return row, displayCol
		}
		displayCol += runeWidth
		logicalCol += runeWidth
	}
	return row, displayCol
}

// Degraded reports whether the layout lacks a source line getter. Valid large
// documents are never degraded merely because they have many rows or lines.
func (w *WrapLayout) Degraded() bool { return w == nil || w.degraded }

// BuildCount verifies repeated identical resizes avoid rebuilding descriptors.
func (w *WrapLayout) BuildCount() int {
	if w == nil {
		return 0
	}
	return w.buildCount
}

// Width returns the configured text width.
func (w *WrapLayout) Width() int {
	if w == nil {
		return 0
	}
	return w.width
}

// VisualRow returns the exact first visual row for a buffer line. A direct
// jump measures only the preceding pages required for exactness.
func (w *WrapLayout) VisualRow(bufLine int) int {
	if w == nil || bufLine <= 0 {
		return 0
	}
	if bufLine >= w.lineCount {
		return w.exactTotalRows()
	}
	blockIndex := w.blockIndex(bufLine)
	total := 0
	for i := 0; i < blockIndex; i++ {
		total += w.ensureBlockTotal(i).totalRows
	}
	b := w.ensureBlockRows(blockIndex)
	for i := 0; i < bufLine-b.firstLine; i++ {
		total += b.rows[i]
	}
	return total
}

// exactTotalRows measures remaining pages only when an operation genuinely
// needs an exact EOF position (for example a direct line jump beyond EOF).
func (w *WrapLayout) exactTotalRows() int {
	if w == nil {
		return 0
	}
	total := 0
	for i := range w.blocks {
		total += w.ensureBlockTotal(i).totalRows
	}
	return total
}

// BufferLine converts a visual row to (buffer line, wrap offset). It measures
// pages from the start until the requested row is found, which makes deep
// mapping exact without any eager full-document work.
func (w *WrapLayout) BufferLine(visualRow int) (int, int) {
	if w == nil || w.lineCount == 0 {
		return 0, 0
	}
	if visualRow <= 0 {
		return 0, 0
	}
	remaining := visualRow
	for i := range w.blocks {
		b := w.ensureBlockTotal(i)
		if remaining < b.totalRows {
			b = w.ensureBlockRows(i)
			for local, rows := range b.rows {
				if remaining < rows {
					return b.firstLine + local, remaining
				}
				remaining -= rows
			}
		}
		remaining -= b.totalRows
	}
	lastLine := w.lineCount - 1
	return lastLine, max(0, w.lineRows(lastLine)-1)
}

// VisibleBufferRange converts a visual viewport to the half-open range of
// logical lines it touches. Syntax tokenization remains line-oriented.
func (w *WrapLayout) VisibleBufferRange(visualStart, height int) (start, end int) {
	if w == nil || w.lineCount == 0 {
		return 0, 0
	}
	start, _ = w.BufferLine(visualStart)
	if height < 1 {
		return start, min(start+1, w.lineCount)
	}
	last, _ := w.BufferLine(visualStart + height - 1)
	return start, min(last+1, w.lineCount)
}

// LineRows returns the exact number of visual rows for one logical line,
// measuring only that line's page if necessary.
func (w *WrapLayout) LineRows(bufLine int) int {
	if w == nil || bufLine < 0 || bufLine >= w.lineCount {
		return 1
	}
	return w.lineRows(bufLine)
}

// Rebuild invalidates every lazy page for a new line count/content or width.
// It does not fetch text synchronously.
func (w *WrapLayout) Rebuild(lineGetter func(int) []byte, lineCount, width int) {
	if w == nil {
		return
	}
	w.rebuild(lineGetter, lineCount, width)
}

// ApplyEdit invalidates only blocks intersecting a known edit. It does no text
// scan and no per-document copy. Structural edits recreate sparse descriptors
// because line-to-block membership shifted; this is O(number of blocks), not
// O(number of logical or visual rows).
func (w *WrapLayout) ApplyEdit(lineGetter func(int) []byte, lineCount, startLine, endLine int, replacement string) bool {
	if w == nil || w.degraded || lineGetter == nil || startLine < 0 || endLine < startLine || endLine >= w.lineCount {
		return false
	}
	lineCount = max(0, lineCount)
	oldCount := endLine - startLine + 1
	newCount := strings.Count(replacement, "\n") + 1
	if w.lineCount-oldCount+newCount != lineCount {
		return false
	}
	w.lineGetter = lineGetter
	if lineCount != w.lineCount {
		w.rebuild(lineGetter, lineCount, w.width)
		return true
	}

	firstBlock := w.blockIndex(startLine)
	lastChangedLine := min(lineCount-1, startLine+newCount-1)
	lastBlock := w.blockIndex(lastChangedLine)
	for i := firstBlock; i >= 0 && i <= lastBlock; i++ {
		b := &w.blocks[i]
		// Keep the page backing slice for the common type/delete path. It is
		// overwritten before use on the next visit, so invalidation remains
		// correct while avoiding a 256-int allocation per keystroke.
		b.totalRows = b.lineCount
		b.known = false
	}
	w.resetSegmentWindow()
	return true
}
