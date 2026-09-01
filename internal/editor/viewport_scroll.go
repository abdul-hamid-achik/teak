package editor

import (
	"teak/internal/text"
)

func (v *Viewport) foldedScrollStart(folds *FoldState, totalLines int) int {
	return folds.VisualLineToBuffer(v.ScrollY, totalLines)
}

func (v *Viewport) EnsureCursorVisible(cursor text.Position, lineCount int) {
	if cursor.Line < v.ScrollY {
		v.ScrollY = cursor.Line
	}
	if cursor.Line >= v.ScrollY+v.Height {
		v.ScrollY = cursor.Line - v.Height + 1
	}

	gw := gutterWidth(lineCount) + 1
	textWidth := v.Width - gw
	if textWidth < 1 {
		textWidth = 1
	}
	displayCol := cursor.Col
	if displayCol < v.ScrollX {
		v.ScrollX = displayCol
	}
	if displayCol >= v.ScrollX+textWidth {
		v.ScrollX = displayCol - textWidth + 1
	}
}

func (v *Viewport) ensureCursorVisible(buf *text.Buffer, cursor text.Position, textWidth int) {
	v.ensureCursorVisibleWithFolds(buf, cursor, textWidth, nil, 0)
}

// clampScrollMargin confines a configured margin so it can never consume the
// whole viewport: at most half the height minus one row.
func clampScrollMargin(margin, height int) int {
	if margin < 0 {
		return 0
	}
	return min(margin, max(0, height/2-1))
}

// ensureCursorVisibleWithFolds scrolls the cursor into view. When folds is
// non-nil, ScrollY counts visual rows rather than buffer lines, so the cursor's
// buffer line must be converted before it is compared or assigned. Margin
// keeps the cursor at least that many rows from the viewport edges.
//
// Comparing a buffer line against a fold-aware ScrollY made the two disagree by
// however many lines were hidden: after collapsing a region, one arrow key
// pushed ScrollY far past the end of the collapsed document and the viewport
// rendered blank, with no way back except scrolling manually.
func (v *Viewport) ensureCursorVisibleWithFolds(buf *text.Buffer, cursor text.Position, textWidth int, folds *FoldState, margin int) {
	if cursor.Line < 0 || cursor.Line >= buf.LineCount() {
		return
	}
	lineContent := buf.Line(cursor.Line)
	col := min(max(0, cursor.Col), len(lineContent))
	v.ensureCursorVisibleAtDisplayColumn(cursor, buf.LineCount(), displayColumn(lineContent, col, v.tabSize()), textWidth, folds, margin)
}

func (v *Viewport) ensureCursorVisibleAtDisplayColumn(cursor text.Position, lineCount, displayCol, textWidth int, folds *FoldState, margin int) {
	margin = clampScrollMargin(margin, v.Height)
	scrollLine := cursor.Line
	maxScroll := -1
	if folds != nil {
		scrollLine = folds.BufferLineToVisual(cursor.Line, lineCount)
		maxScroll = max(folds.TotalVisibleLines(lineCount)-v.Height, 0)
	}

	if scrollLine < v.ScrollY+margin {
		v.ScrollY = scrollLine - margin
	}
	if scrollLine >= v.ScrollY+v.Height-margin {
		v.ScrollY = scrollLine - v.Height + margin + 1
	}
	if maxScroll >= 0 && v.ScrollY > maxScroll {
		v.ScrollY = maxScroll
	}
	if v.ScrollY < 0 {
		v.ScrollY = 0
	}

	if textWidth < 1 {
		textWidth = 1
	}
	if displayCol < v.ScrollX {
		v.ScrollX = displayCol
	}
	if displayCol >= v.ScrollX+textWidth {
		v.ScrollX = displayCol - textWidth + 1
	}
}

func (v *Viewport) ScrollLeft(n int) {
	v.ScrollX = max(0, v.ScrollX-max(0, n))
}

func (v *Viewport) ScrollRight(n int) {
	v.ScrollX += max(0, n)
}

func (v *Viewport) ScrollUp(n int) {
	v.ScrollY -= n
	if v.ScrollY < 0 {
		v.ScrollY = 0
	}
}

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

// ScrollWrapUp moves in visual rows, allowing a single long logical line to
// be navigated with the mouse wheel.
func (v *Viewport) ScrollWrapUp(n int) {
	v.WrapScrollY = max(0, v.WrapScrollY-max(0, n))
}

// ScrollWrapDown moves in visual rows and clamps to the last full viewport.
func (v *Viewport) ScrollWrapDown(n int, wrap *WrapLayout) {
	if wrap == nil {
		return
	}
	if !wrap.TotalRowsKnown() {
		v.WrapScrollY = max(0, v.WrapScrollY+max(0, n))
		return
	}
	maxScroll := max(0, wrap.TotalRows()-max(1, v.Height))
	v.WrapScrollY = min(maxScroll, v.WrapScrollY+max(0, n))
}
