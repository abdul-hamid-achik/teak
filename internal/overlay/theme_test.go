package overlay

import (
	"testing"

	"teak/internal/ui"
)

func TestStackSetThemePreservesOverlayState(t *testing.T) {
	picker := NewPicker("Themes", []PickerItem{{Label: "Nord"}}, ui.NordTheme(), "theme")
	picker.input.SetValue("nor")
	stack := Stack{}
	stack.Push(picker)
	theme := ui.DraculaTheme()
	stack.SetTheme(theme)
	if picker.input.Value() != "nor" || picker.theme != theme {
		t.Fatal("SetTheme must preserve picker state while replacing its theme")
	}
}

func TestPickerSetCursorClampsAndScrolls(t *testing.T) {
	picker := NewPicker("Themes", []PickerItem{{Label: "1"}, {Label: "2"}, {Label: "3"}, {Label: "4"}}, ui.NordTheme(), "theme")
	picker.maxHeight = 2
	picker.SetCursor(99)
	if picker.cursor != 3 || picker.scrollY != 1 {
		t.Fatalf("cursor=%d scroll=%d, want cursor=3 scroll=1", picker.cursor, picker.scrollY)
	}
	picker.SetCursor(-1)
	if picker.cursor != 0 || picker.scrollY != 0 {
		t.Fatalf("cursor=%d scroll=%d, want cursor=0 scroll=0", picker.cursor, picker.scrollY)
	}
}
