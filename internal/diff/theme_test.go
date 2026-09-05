package diff

import (
	"testing"

	"teak/internal/ui"
)

func TestSetThemeReplacesHighlighters(t *testing.T) {
	m := New("main.go", []DiffLine{{Left: "x", Right: "x", LeftNum: 1, RightNum: 1}}, ui.NordTheme())
	old := m.leftHL
	theme := ui.DraculaTheme()
	cmd := m.SetTheme(theme)
	if m.theme != theme || m.leftHL == old {
		t.Fatal("SetTheme must replace highlighters for the new palette")
	}
	if cmd == nil {
		t.Fatal("SetTheme must schedule asynchronous visible-viewport tokenization")
	}
}

func TestSetThemeSameThemeIsNoop(t *testing.T) {
	theme := ui.NordTheme()
	m := New("main.go", nil, theme)
	if cmd := m.SetTheme(theme); cmd != nil {
		t.Fatal("same theme should not schedule highlighter work")
	}
}
