package editor

import "github.com/mattn/go-runewidth"

// WrapLayout pre-computes visual rows per buffer line for word wrap.
type WrapLayout struct {
	lineWraps []int // lineWraps[i] = number of visual rows for buffer line i
	totalRows int
	width     int // text width (invalidated on resize)
}

// NewWrapLayout creates a WrapLayout for a buffer at the given text width.
func NewWrapLayout(lineGetter func(int) []byte, lineCount, width int) *WrapLayout {
	if width < 1 {
		width = 1
	}
	w := &WrapLayout{
		lineWraps: make([]int, lineCount),
		width:     width,
	}
	for i := 0; i < lineCount; i++ {
		rows := w.computeLineRows(lineGetter(i), width)
		w.lineWraps[i] = rows
		w.totalRows += rows
	}
	return w
}

// computeLineRows returns how many visual rows a line occupies at the given width.
func (w *WrapLayout) computeLineRows(line []byte, width int) int {
	_, _, rows, _ := wrappedLineSegment(string(line), -1, width)
	return rows
}

// wrappedLineSegment returns byte bounds for one visual row and the total row
// count. Wide runes move to the next row when they do not fit, so no row
// silently exceeds the configured width merely because a rune straddles a
// fixed display-width bucket.
func wrappedLineSegment(line string, segment, width int) (start, end, rows int, ok bool) {
	if width < 1 {
		width = 1
	}
	if line == "" {
		return 0, 0, 1, segment == 0
	}

	row := 0
	rowStart := 0
	rowWidth := 0
	for i, r := range line {
		runeWidth := runewidth.RuneWidth(r)
		if runeWidth < 0 {
			runeWidth = 0
		}
		if rowWidth > 0 && rowWidth+runeWidth > width {
			if row == segment {
				return rowStart, i, 0, true
			}
			row++
			rowStart = i
			rowWidth = 0
		}
		rowWidth += runeWidth
	}

	rows = row + 1
	if row == segment {
		return rowStart, len(line), rows, true
	}
	return 0, 0, rows, false
}

// wrappedPosition maps a byte column to its packed visual row and display
// column using the same wide-rune rules as wrappedLineSegment.
func wrappedPosition(line string, byteCol, width int) (row, displayCol int) {
	if width < 1 {
		width = 1
	}
	byteCol = max(0, min(byteCol, len(line)))

	for i, r := range line {
		runeWidth := runewidth.RuneWidth(r)
		if runeWidth < 0 {
			runeWidth = 0
		}
		if displayCol > 0 && displayCol+runeWidth > width {
			row++
			displayCol = 0
		}
		if i >= byteCol {
			return row, displayCol
		}
		displayCol += runeWidth
	}
	return row, displayCol
}

// TotalRows returns the total visual rows across all lines.
func (w *WrapLayout) TotalRows() int {
	return w.totalRows
}

// Width returns the configured text width.
func (w *WrapLayout) Width() int {
	return w.width
}

// VisualRow returns the first visual row index for a given buffer line.
func (w *WrapLayout) VisualRow(bufLine int) int {
	if bufLine <= 0 {
		return 0
	}
	row := 0
	limit := bufLine
	if limit > len(w.lineWraps) {
		limit = len(w.lineWraps)
	}
	for i := 0; i < limit; i++ {
		row += w.lineWraps[i]
	}
	return row
}

// BufferLine converts a visual row to (buffer line, wrap offset within that line).
func (w *WrapLayout) BufferLine(visualRow int) (int, int) {
	row := 0
	for i, rows := range w.lineWraps {
		if visualRow < row+rows {
			return i, visualRow - row
		}
		row += rows
	}
	// Past end
	if len(w.lineWraps) > 0 {
		return len(w.lineWraps) - 1, 0
	}
	return 0, 0
}

// LineRows returns the number of visual rows for a specific buffer line.
func (w *WrapLayout) LineRows(bufLine int) int {
	if bufLine < 0 || bufLine >= len(w.lineWraps) {
		return 1
	}
	return w.lineWraps[bufLine]
}

// Rebuild recalculates the layout for a new line count/content.
func (w *WrapLayout) Rebuild(lineGetter func(int) []byte, lineCount, width int) {
	if width < 1 {
		width = 1
	}
	w.width = width
	w.lineWraps = make([]int, lineCount)
	w.totalRows = 0
	for i := 0; i < lineCount; i++ {
		rows := w.computeLineRows(lineGetter(i), width)
		w.lineWraps[i] = rows
		w.totalRows += rows
	}
}
