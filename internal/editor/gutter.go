package editor

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// GutterOpts holds optional debug-related gutter state.
type GutterOpts struct {
	Breakpoints   map[int]bool // 0-based line numbers with breakpoints
	ExecLine      int          // 0-based current execution line, -1 if none
}

// RenderGutter renders line numbers for visible lines with optional diagnostic icons.
// Returns the rendered gutter string and its width.
func RenderGutter(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic, opts *GutterOpts) (string, int) {
	// Add 2 columns for breakpoint marker when opts provided
	baseWidth := gutterWidth(totalLines)
	markerWidth := 0
	if opts != nil {
		markerWidth = 2 // "● " or "  "
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

	// Styles for breakpoint marker and execution line
	bpStyle := lipgloss.NewStyle().Foreground(ui.Nord11) // red
	execStyle := lipgloss.NewStyle().Background(ui.Nord3).Foreground(ui.Nord13) // yellow on dark

	for i := range height {
		line := scrollY + i
		if line >= totalLines {
			sb.WriteString(theme.Gutter.Render(strings.Repeat(" ", width)))
		} else {
			// Breakpoint marker column
			if opts != nil {
				if opts.Breakpoints[line] {
					sb.WriteString(bpStyle.Render("● "))
				} else {
					sb.WriteString("  ")
				}
			}

			numStr := fmt.Sprintf("%*d", baseWidth, line+1)

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
