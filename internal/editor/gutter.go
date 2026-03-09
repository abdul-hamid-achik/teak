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

// RenderGutter renders line numbers for visible lines with optional diagnostic icons.
// Returns the rendered gutter string and its width.
func RenderGutter(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic, opts *GutterOpts) (string, int) {
	// Add 4 columns for breakpoint marker when opts provided (1 leading space + 2-cell icon + 1 trailing space)
	baseWidth := gutterWidth(totalLines)
	markerWidth := 0
	if opts != nil {
		markerWidth = 3
	}
	width := baseWidth + markerWidth

	// Build a map of line -> worst diagnostic severity
	diagMap := make(map[int]int) // line -> severity (1=error, 2=warn, 3=info, 4=hint)
	for _, d := range diagnostics {
		for line := d.StartLine; line <= d.EndLine; line++ {
			if existing, ok := diagMap[line]; !ok || d.Severity < existing {
				diagMap[line] = d.Severity
			}
		}
	}

	var sb strings.Builder

	// Use pre-cached theme styles instead of creating new styles each render
	bpActiveStyle := theme.BreakpointActive
	bpDisabledStyle := theme.BreakpointDisabled
	execStyle := theme.ExecLineMarker

	for i := range height {
		line := scrollY + i
		if line >= totalLines {
			sb.WriteString(theme.Gutter.Render(getSpaces(width)))
		} else {
			// Breakpoint marker column (1 leading space + 2-cell icon + 1 trailing space)
			if opts != nil {
				switch opts.Breakpoints[line] {
				case BPActive:
					sb.WriteByte(' ')
					sb.WriteString(bpActiveStyle.Render("\U000f0765"))
				case BPDisabled:
					sb.WriteByte(' ')
					sb.WriteString(bpDisabledStyle.Render("\U000f0765"))
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
					sb.WriteString(theme.GutterError.Render(numStr))
				case 2: // warning
					sb.WriteString(theme.GutterWarn.Render(numStr))
				default:
					if line == activeLine {
						sb.WriteString(theme.GutterActive.Render(numStr))
					} else {
						sb.WriteString(theme.Gutter.Render(numStr))
					}
				}
			} else if line == activeLine {
				sb.WriteString(theme.GutterActive.Render(numStr))
			} else {
				sb.WriteString(theme.Gutter.Render(numStr))
			}
		}
		if i < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), width
}

// RenderGutterWithFolds renders the gutter with fold indicators.
// A dedicated 1-char column is added after line numbers for fold indicators (Nerd Font chevrons).
// If folds is nil or visibleLines is empty, falls back to standard rendering.
func RenderGutterWithFolds(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic, opts *GutterOpts, folds *FoldState, visibleLines []int) (string, int) {
	if folds == nil || len(folds.Regions) == 0 || len(visibleLines) == 0 {
		return RenderGutter(theme, totalLines, scrollY, height, activeLine, diagnostics, opts)
	}

	baseWidth := gutterWidth(totalLines)
	markerWidth := 0
	if opts != nil {
		markerWidth = 3 // 1 leading space + 2-cell icon + 1 trailing space
	}
	foldWidth := 2 // Nerd Font chevron icon (2-cell glyph)
	width := baseWidth + markerWidth + foldWidth

	diagMap := make(map[int]int)
	for _, d := range diagnostics {
		for line := d.StartLine; line <= d.EndLine; line++ {
			if existing, ok := diagMap[line]; !ok || d.Severity < existing {
				diagMap[line] = d.Severity
			}
		}
	}

	var sb strings.Builder
	// Use pre-cached theme styles instead of creating new styles each render
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
			sb.WriteString(theme.Gutter.Render(getSpaces(width)))
		} else {
			// Breakpoint marker column (1 leading space + 2-cell icon + 1 trailing space)
			if opts != nil {
				switch opts.Breakpoints[line] {
				case BPActive:
					sb.WriteByte(' ')
					sb.WriteString(bpActiveStyle.Render("\U000f0765"))
				case BPDisabled:
					sb.WriteByte(' ')
					sb.WriteString(bpDisabledStyle.Render("\U000f0765"))
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
					sb.WriteString(theme.GutterError.Render(numStr))
				case 2:
					sb.WriteString(theme.GutterWarn.Render(numStr))
				default:
					if line == activeLine {
						sb.WriteString(theme.GutterActive.Render(numStr))
					} else {
						sb.WriteString(theme.Gutter.Render(numStr))
					}
				}
			} else if line == activeLine {
				sb.WriteString(theme.GutterActive.Render(numStr))
			} else {
				sb.WriteString(theme.Gutter.Render(numStr))
			}

			// Fold indicator column (2-cell Nerd Font chevron)
			indicator := folds.FoldIndicator(line)
			switch indicator {
			case ">":
				sb.WriteString(foldCollapsedStyle.Render("\U000f0142"))
			case "v":
				sb.WriteString(foldExpandedStyle.Render("\U000f0140"))
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
