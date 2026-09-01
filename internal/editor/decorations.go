package editor

import (
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// maxDecorationLineBytes bounds per-line decoration work so a giant line in
// the viewport cannot scan the whole row on the UI goroutine.
const maxDecorationLineBytes = 8192

func (e Editor) decorationHighlights(visibleLines []int, startLine, endLine int) []HighlightRange {
	if e.Buffer == nil {
		return nil
	}
	needGuides := e.Config.IndentGuides
	needTrail := e.Config.HighlightTrailingWS
	needRuler := e.Config.RulerColumn > 0
	if !needGuides && !needTrail && !needRuler {
		return nil
	}
	tabSize := e.Config.TabSize
	if tabSize < 1 {
		tabSize = 4
	}
	var ranges []HighlightRange
	if len(visibleLines) > 0 {
		ranges = make([]HighlightRange, 0, len(visibleLines))
		for _, line := range visibleLines {
			ranges = append(ranges, e.decorationHighlightsForLine(line, tabSize, needGuides, needTrail, needRuler)...)
		}
		return ranges
	}
	if startLine > endLine {
		return nil
	}
	lineCount := e.Buffer.LineCount()
	for line := startLine; line <= endLine && line < lineCount; line++ {
		ranges = append(ranges, e.decorationHighlightsForLine(line, tabSize, needGuides, needTrail, needRuler)...)
	}
	return ranges
}

func (e Editor) decorationHighlightsForLine(line, tabSize int, needGuides, needTrail, needRuler bool) []HighlightRange {
	content := e.Buffer.Line(line)
	if len(content) == 0 {
		return nil
	}
	scan := content
	if len(scan) > maxDecorationLineBytes {
		scan = scan[:maxDecorationLineBytes]
	}
	var ranges []HighlightRange
	if needGuides {
		ranges = append(ranges, indentGuideRanges(line, scan, tabSize, e.theme.IndentGuide)...)
	}
	if needTrail {
		if trail, ok := trailingWSRange(line, content, e.theme.TrailingWS); ok {
			ranges = append(ranges, trail)
		}
	}
	if needRuler {
		if ruler, ok := rulerRange(line, scan, e.Config.RulerColumn, tabSize, e.theme.Ruler); ok {
			ranges = append(ranges, ruler)
		}
	}
	return ranges
}

func indentGuideRanges(line int, content []byte, tabSize int, style lipgloss.Style) []HighlightRange {
	end := 0
	for end < len(content) && (content[end] == ' ' || content[end] == '\t') {
		end++
	}
	if end == 0 {
		return nil
	}
	var ranges []HighlightRange
	display := 0
	for i := 0; i < end; i++ {
		if content[i] == '\t' {
			display = ((display / tabSize) + 1) * tabSize
		} else {
			display++
		}
		if display > 0 && display%tabSize == 0 {
			ranges = append(ranges, HighlightRange{
				Line:     line,
				StartCol: i,
				EndCol:   i + 1,
				Style:    style,
			})
		}
	}
	return ranges
}

func trailingWSRange(line int, content []byte, style lipgloss.Style) (HighlightRange, bool) {
	end := len(content)
	start := end
	limit := 0
	if end > 256 {
		limit = end - 256
	}
	for start > limit && (content[start-1] == ' ' || content[start-1] == '\t') {
		start--
	}
	if start == end {
		return HighlightRange{}, false
	}
	return HighlightRange{Line: line, StartCol: start, EndCol: end, Style: style}, true
}

func rulerRange(line int, content []byte, rulerCol, tabSize int, style lipgloss.Style) (HighlightRange, bool) {
	if rulerCol <= 0 {
		return HighlightRange{}, false
	}
	byteCol := byteColumnAtDisplay(content, rulerCol, tabSize)
	if byteCol < 0 || byteCol >= len(content) {
		return HighlightRange{}, false
	}
	_, size := utf8.DecodeRune(content[byteCol:])
	if size < 1 {
		size = 1
	}
	end := byteCol + size
	if end > len(content) {
		end = len(content)
	}
	return HighlightRange{Line: line, StartCol: byteCol, EndCol: end, Style: style}, true
}
