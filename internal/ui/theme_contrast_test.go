package ui

import (
	"image/color"
	"math"
	"testing"

	"charm.land/lipgloss/v2"
)

// Use the sRGB contrast ratio, not terminal brightness heuristics.
// https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html
func TestThemeControlsAndTextHaveReadableContrast(t *testing.T) {
	luminance := func(c color.Color) float64 {
		r, g, b, _ := c.RGBA()
		var result float64
		for i, v := range []uint32{r, g, b} {
			x := float64(v) / 65535
			if x <= 0.04045 {
				x /= 12.92
			} else {
				x = math.Pow((x+0.055)/1.055, 2.4)
			}
			result += x * []float64{0.2126, 0.7152, 0.0722}[i]
		}
		return result
	}
	for _, option := range ThemeOptions() {
		t.Run(option.ID, func(t *testing.T) {
			theme := option.Constructor()
			for name, style := range map[string]lipgloss.Style{
				"status": theme.StatusText, "push-pull": theme.GitPushPullButton,
				"selection": theme.Selection, "secondary-selection": theme.SecondarySelection,
				"find-match": theme.FindMatch, "find-current": theme.FindMatchCurrent,
				"commit": theme.GitCommitButton, "replace": theme.ReplaceButton,
				"autocomplete": theme.AutocompleteCursor, "search-active": theme.SearchActive,
				"diff-added": theme.DiffAdded, "diff-removed": theme.DiffRemoved,
				"inactive-tab": theme.TabInactive, "inactive-tab-close": theme.TabCloseInactive,
				"inactive-sidebar-tab": theme.SidebarTabInactive,
				"gutter":               theme.Gutter, "diff-gutter": theme.DiffGutter,
				"comment": lipgloss.NewStyle().Foreground(theme.SyntaxComment).Background(theme.Editor.GetBackground()),
			} {
				fg, bg := luminance(style.GetForeground()), luminance(style.GetBackground())
				if ratio := (max(fg, bg) + 0.05) / (min(fg, bg) + 0.05); ratio < 4.5 {
					t.Errorf("%s text contrast = %.2f:1, want at least 4.5:1", name, ratio)
				}
			}
		})
	}
}
