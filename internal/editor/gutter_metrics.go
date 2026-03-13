package editor

const (
	breakpointColumnWidth = 3
	foldColumnWidth       = 2
	gutterPaddingWidth    = 1
)

type gutterMetrics struct {
	lineNumberWidth int
	markerWidth     int
	foldWidth       int
}

func computeGutterMetrics(totalLines int, opts *GutterOpts, showFoldColumn bool) gutterMetrics {
	metrics := gutterMetrics{
		lineNumberWidth: gutterWidth(totalLines),
	}
	if opts != nil {
		metrics.markerWidth = breakpointColumnWidth
	}
	if showFoldColumn {
		metrics.foldWidth = foldColumnWidth
	}
	return metrics
}

func (gm gutterMetrics) contentWidth() int {
	return gm.lineNumberWidth + gm.markerWidth + gm.foldWidth
}

func (gm gutterMetrics) totalWidth() int {
	return gm.contentWidth() + gutterPaddingWidth
}

func (gm gutterMetrics) textWidth(viewportWidth int) int {
	textWidth := viewportWidth - gm.totalWidth()
	if textWidth < 1 {
		return 1
	}
	return textWidth
}
