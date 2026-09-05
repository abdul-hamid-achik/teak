package filetree

import (
	"testing"

	"teak/internal/ui"
)

func TestSetThemeRefreshesCachedStyles(t *testing.T) {
	m := NewEmpty(".", ui.NordTheme())
	theme := ui.DraculaTheme()
	m.SetTheme(theme)
	if m.theme != theme || m.cachedStyles.entryBg != theme.TreeEntry.GetBackground() || m.cachedStyles.gitIgnoredColor != theme.Gutter.GetForeground() {
		t.Fatal("SetTheme did not replace tree rendering state")
	}
}
