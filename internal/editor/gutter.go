package editor

import (
	"strconv"
	"strings"

	"teak/internal/ui"
)

// formatLineNumber formats a line number with padding for the gutter.
// Uses strconv.Itoa for performance (faster than fmt.Sprintf).
func formatLineNumber(line, width int) string {
	numStr := strconv.Itoa(line + 1)
	if len(numStr) < width {
		// Pre-allocate padded string
		padding := make([]byte, width-len(numStr))
		for i := range padding {
			padding[i] = ' '
		}
		numStr = string(padding) + numStr
	}
	return numStr
}

// BreakpointState represents the state of a breakpoint on a line.
type BreakpointState int

const (
	BPActive   BreakpointState = iota + 1 // red filled circle — will pause
	BPDisabled                            // grey circle — muted, won't pause
)

// GutterOpts holds optional debug-related gutter state.
type GutterOpts struct {
	Breakpoints map[int]BreakpointState // 0-based line → state
	ExecLine    int                     // 0-based current execution line, -1 if none
}

func breakpointGlyph() string {
	if ui.NerdFontEnabled() {
		return "\U000f0765"
	}
	return "*"
}

func foldGlyph(indicator string) string {
	if !ui.NerdFontEnabled() {
		return indicator
	}
	if indicator == ">" {
		return "\U000f0142"
	}
	return "\U000f0140"
}

// diagnosticSeveritiesForVisibleLines returns only diagnostics that can be
// rendered in the supplied rows. LSP ranges can span an entire generated file;
// expanding every range to a line -> severity map on each frame made a small
// viewport proportional to the document size. The viewport is deliberately
// small, so intersecting each diagnostic with these rows is bounded by the
// render budget instead.
func diagnosticSeveritiesForVisibleLines(diagnostics []Diagnostic, visibleLines []int) map[int]int {
	if len(diagnostics) == 0 || len(visibleLines) == 0 {
		return nil
	}

	severities := make(map[int]int, len(visibleLines))
	for _, d := range diagnostics {
		if d.EndLine < d.StartLine {
			continue
		}
		for _, line := range visibleLines {
			if line < d.StartLine || line > d.EndLine {
				continue
			}
			if current, ok := severities[line]; !ok || d.Severity < current {
				severities[line] = d.Severity
			}
		}
	}
	return severities
}

func diagnosticSeveritiesForLineRange(diagnostics []Diagnostic, startLine, height int) map[int]int {
	if len(diagnostics) == 0 || height <= 0 {
		return nil
	}
	endLine := startLine + height - 1
	severities := make(map[int]int, height)
	for _, d := range diagnostics {
		start := max(startLine, d.StartLine)
		end := min(endLine, d.EndLine)
		for line := start; line <= end; line++ {
			if current, ok := severities[line]; !ok || d.Severity < current {
				severities[line] = d.Severity
			}
		}
	}
	return severities
}

func diagnosticSeverityAt(diagnostics []Diagnostic, line int) (int, bool) {
	severity := 0
	for _, d := range diagnostics {
		if line < d.StartLine || line > d.EndLine {
			continue
		}
		if severity == 0 || d.Severity < severity {
			severity = d.Severity
		}
	}
	return severity, severity != 0
}

// RenderGutter renders line numbers for visible lines with optional diagnostic icons.
// Returns the rendered gutter string and its width.
func RenderGutter(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic, opts *GutterOpts) (string, int) {
	metrics := computeGutterMetrics(totalLines, opts, false)
	baseWidth := metrics.lineNumberWidth
	width := metrics.contentWidth()

	diagMap := diagnosticSeveritiesForLineRange(diagnostics, scrollY, height)

	var sb strings.Builder

	// Use pre-cached theme styles instead of creating new styles each render
	gutterStyle := theme.Gutter.UnsetPadding()
	gutterActiveStyle := theme.GutterActive.UnsetPadding()
	gutterErrorStyle := theme.GutterError.UnsetPadding()
	gutterWarnStyle := theme.GutterWarn.UnsetPadding()
	bpActiveStyle := theme.BreakpointActive
	bpDisabledStyle := theme.BreakpointDisabled
	execStyle := theme.ExecLineMarker

	for i := range height {
		line := scrollY + i
		if line >= totalLines {
			sb.WriteString(gutterStyle.Render(getSpaces(width)))
		} else {
			// Breakpoint marker column (1 leading space + icon + 1 trailing space)
			if opts != nil {
				switch opts.Breakpoints[line] {
				case BPActive:
					sb.WriteByte(' ')
					sb.WriteString(bpActiveStyle.Render(breakpointGlyph()))
					sb.WriteByte(' ')
				case BPDisabled:
					sb.WriteByte(' ')
					sb.WriteString(bpDisabledStyle.Render(breakpointGlyph()))
					sb.WriteByte(' ')
				default:
					sb.WriteString("   ")
				}
			}

			numStr := formatLineNumber(line, baseWidth)

			// Check if this is the current execution line
			isExecLine := opts != nil && opts.ExecLine == line

			if isExecLine {
				sb.WriteString(execStyle.Render(numStr))
			} else if sev, ok := diagMap[line]; ok {
				switch sev {
				case 1: // error
					sb.WriteString(gutterErrorStyle.Render(numStr))
				case 2: // warning
					sb.WriteString(gutterWarnStyle.Render(numStr))
				default:
					if line == activeLine {
						sb.WriteString(gutterActiveStyle.Render(numStr))
					} else {
						sb.WriteString(gutterStyle.Render(numStr))
					}
				}
			} else if line == activeLine {
				sb.WriteString(gutterActiveStyle.Render(numStr))
			} else {
				sb.WriteString(gutterStyle.Render(numStr))
			}
		}
		if i < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), width
}

// RenderGutterWithFolds renders the gutter with fold indicators.
// Fold rows reserve a 2-cell slot after line numbers so text starts at a
// consistent column whether a fold icon is present or not.
// If folds is nil or visibleLines is empty, falls back to standard rendering.
func RenderGutterWithFolds(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic, opts *GutterOpts, folds *FoldState, visibleLines []int) (string, int) {
	if folds == nil || len(folds.Regions) == 0 || len(visibleLines) == 0 {
		return RenderGutter(theme, totalLines, scrollY, height, activeLine, diagnostics, opts)
	}

	metrics := computeGutterMetrics(totalLines, opts, true)
	baseWidth := metrics.lineNumberWidth
	width := metrics.contentWidth()

	diagMap := diagnosticSeveritiesForVisibleLines(diagnostics, visibleLines)

	var sb strings.Builder
	// Use pre-cached theme styles instead of creating new styles each render
	gutterStyle := theme.Gutter.UnsetPadding()
	gutterActiveStyle := theme.GutterActive.UnsetPadding()
	gutterErrorStyle := theme.GutterError.UnsetPadding()
	gutterWarnStyle := theme.GutterWarn.UnsetPadding()
	bpActiveStyle := theme.BreakpointActive
	bpDisabledStyle := theme.BreakpointDisabled
	execStyle := theme.ExecLineMarker
	foldCollapsedStyle := theme.FoldCollapsed
	foldExpandedStyle := theme.FoldExpanded

	for i := range height {
		var line int
		inRange := i < len(visibleLines)
		if inRange {
			line = visibleLines[i]
		}

		if !inRange || line >= totalLines {
			sb.WriteString(gutterStyle.Render(getSpaces(width)))
		} else {
			// Breakpoint marker column (1 leading space + icon + 1 trailing space)
			if opts != nil {
				switch opts.Breakpoints[line] {
				case BPActive:
					sb.WriteByte(' ')
					sb.WriteString(bpActiveStyle.Render(breakpointGlyph()))
					sb.WriteByte(' ')
				case BPDisabled:
					sb.WriteByte(' ')
					sb.WriteString(bpDisabledStyle.Render(breakpointGlyph()))
					sb.WriteByte(' ')
				default:
					sb.WriteString("   ")
				}
			}

			// Line number
			numStr := formatLineNumber(line, baseWidth)
			isExecLine := opts != nil && opts.ExecLine == line

			if isExecLine {
				sb.WriteString(execStyle.Render(numStr))
			} else if sev, ok := diagMap[line]; ok {
				switch sev {
				case 1:
					sb.WriteString(gutterErrorStyle.Render(numStr))
				case 2:
					sb.WriteString(gutterWarnStyle.Render(numStr))
				default:
					if line == activeLine {
						sb.WriteString(gutterActiveStyle.Render(numStr))
					} else {
						sb.WriteString(gutterStyle.Render(numStr))
					}
				}
			} else if line == activeLine {
				sb.WriteString(gutterActiveStyle.Render(numStr))
			} else {
				sb.WriteString(gutterStyle.Render(numStr))
			}

			// Fold indicator column (icon + trailing space)
			indicator := folds.FoldIndicator(line)
			switch indicator {
			case ">":
				sb.WriteString(foldCollapsedStyle.Render(foldGlyph(indicator)))
				sb.WriteByte(' ')
			case "v":
				sb.WriteString(foldExpandedStyle.Render(foldGlyph(indicator)))
				sb.WriteByte(' ')
			default:
				sb.WriteString("  ")
			}
		}
		if i < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), width
}

func gutterWidth(totalLines int) int {
	w := 1
	n := totalLines
	for n >= 10 {
		w++
		n /= 10
	}
	if w < 3 {
		w = 3
	}
	return w
}
