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
	if cursor.Line < v.ScrollY {
		v.ScrollY = cursor.Line
	}
	if cursor.Line >= v.ScrollY+v.Height {
		v.ScrollY = cursor.Line - v.Height + 1
	}

	if cursor.Line < 0 || cursor.Line >= buf.LineCount() {
		return
	}
	if textWidth < 1 {
		textWidth = 1
	}

	lineContent := buf.Line(cursor.Line)
	col := cursor.Col
	if col > len(lineContent) {
		col = len(lineContent)
	}
	displayCol := displayWidth(string(lineContent[:col]))
	if displayCol < v.ScrollX {
		v.ScrollX = displayCol
	}
	if displayCol >= v.ScrollX+textWidth {
		v.ScrollX = displayCol - textWidth + 1
	}
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
