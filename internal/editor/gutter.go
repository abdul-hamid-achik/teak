package editor

import (
	"fmt"
	"strings"

	"teak/internal/ui"
)

// RenderGutter renders line numbers for visible lines with optional diagnostic icons.
// Returns the rendered gutter string and its width.
func RenderGutter(theme ui.Theme, totalLines, scrollY, height, activeLine int, diagnostics []Diagnostic) (string, int) {
	width := gutterWidth(totalLines)

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

	for i := range height {
		line := scrollY + i
		if line >= totalLines {
			sb.WriteString(theme.Gutter.Render(strings.Repeat(" ", width)))
		} else {
			numStr := fmt.Sprintf("%*d", width, line+1)

			// Check for diagnostic icon
			if sev, ok := diagMap[line]; ok {
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
