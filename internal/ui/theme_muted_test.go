package ui

import (
	"image/color"
	"testing"
)

func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// Muted is the secondary-text role: descriptions, hints, placeholders and
// ignored entries. It must stay readable on both the editor and the panel
// background while being visibly dimmer than normal text, which is what the
// gutter foreground (raised to full text brightness for contrast) cannot be.
func TestThemeMutedIsReadableAndDimmerThanText(t *testing.T) {
	for _, option := range ThemeOptions() {
		t.Run(option.ID, func(t *testing.T) {
			theme := option.Constructor()
			muted := theme.Muted.GetForeground()
			text := theme.Editor.GetForeground()
			for name, bg := range map[string]color.Color{
				"editor": theme.Editor.GetBackground(),
				"panel":  theme.Panel.GetBackground(),
			} {
				if ratio := contrastRatio(muted, bg); ratio < 4.5 {
					t.Errorf("muted on %s contrast = %.2f:1, want at least 4.5:1", name, ratio)
				}
			}
			// Where normal text has headroom above the readability floor, muted
			// must use it; low-contrast palettes may have none to give.
			editorBg := theme.Editor.GetBackground()
			if textRatio := contrastRatio(text, editorBg); textRatio >= 7 {
				if mutedRatio := contrastRatio(muted, editorBg); mutedRatio > textRatio*0.85 {
					t.Errorf("muted (%.2f:1) is not visibly dimmer than text (%.2f:1)", mutedRatio, textRatio)
				}
			}
		})
	}
}

func TestThemePanelMatchesOverlayBackground(t *testing.T) {
	for _, option := range ThemeOptions() {
		t.Run(option.ID, func(t *testing.T) {
			theme := option.Constructor()
			if theme.Panel.GetBackground() != theme.HelpBorder.GetBackground() {
				t.Errorf("panel background %v differs from overlay background %v", theme.Panel.GetBackground(), theme.HelpBorder.GetBackground())
			}
			if contrastRatio(theme.Panel.GetForeground(), theme.Panel.GetBackground()) < 4.5 {
				t.Errorf("panel text contrast below 4.5:1")
			}
		})
	}
}
