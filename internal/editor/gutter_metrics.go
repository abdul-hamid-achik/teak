package editor

const (
	breakpointColumnWidth = 3
	foldColumnWidth       = 2
	gitColumnWidth        = 1
	gutterPaddingWidth    = 1
)

type gutterMetrics struct {
	lineNumberWidth int
	markerWidth     int
	foldWidth       int
	gitWidth        int
}

func computeGutterMetrics(totalLines int, opts *GutterOpts, showFoldColumn bool) gutterMetrics {
	metrics := gutterMetrics{
		lineNumberWidth: gutterWidth(totalLines),
	}
	if opts != nil && opts.Breakpoints != nil {
		metrics.markerWidth = breakpointColumnWidth
	}
	if opts != nil && opts.ShowGit {
		metrics.gitWidth = gitColumnWidth
	}
	if showFoldColumn {
		metrics.foldWidth = foldColumnWidth
	}
	return metrics
}

func (gm gutterMetrics) contentWidth() int {
	return gm.lineNumberWidth + gm.markerWidth + gm.foldWidth + gm.gitWidth
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
