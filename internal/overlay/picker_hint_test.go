package overlay

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

func TestPickerDescriptionsRightAligned(t *testing.T) {
	theme := ui.DefaultTheme()
	muted := theme.Muted.GetForeground()
	items := []PickerItem{
		{Label: "Save File", Description: "Ctrl+S"},
		{Label: "Quit", Description: "Ctrl+Q"},
	}
	p := NewPicker("Command Palette", items, theme, "hint")
	p.SetDescriptionsRightAligned(true)
	p.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	rows := rawCells(p.View())

	for _, tc := range []struct{ label, hint string }{{"Save File", "Ctrl+S"}, {"Quit", "Ctrl+Q"}} {
		found := false
		for _, row := range rows {
			lx := findText(row, tc.label)
			if lx < 0 {
				continue
			}
			found = true
			hx := findText(row, tc.hint)
			// The hint ends at the content right edge (border + 2 padding cells).
			if end := hx + len(tc.hint); end != len(row)-3 {
				t.Errorf("%s: hint ends at column %d, want %d", tc.label, end, len(row)-3)
			}
			if !sameColor(row[hx].Style.Fg, muted) {
				t.Errorf("%s: hint fg = %v, want muted %v", tc.label, row[hx].Style.Fg, muted)
			}
			if sameColor(row[lx].Style.Fg, muted) {
				t.Errorf("%s: label rendered muted", tc.label)
			}
		}
		if !found {
			t.Errorf("row %q not rendered", tc.label)
		}
	}
}

func TestPickerRightAlignedTruncatesLabelNotHint(t *testing.T) {
	theme := ui.DefaultTheme()
	p := NewPicker("Command Palette", []PickerItem{{
		Label:       "Open a really quite remarkably long command label that cannot fit",
		Description: "Ctrl+Shift+P",
	}}, theme, "hint-trunc")
	p.SetDescriptionsRightAligned(true)
	p.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	var text string
	var boxWidth int
	for _, r := range rawCells(p.View()) {
		if s := rowText(r); strings.Contains(s, "Open a") {
			text, boxWidth = s, len(r)
		}
	}
	if text == "" {
		t.Fatal("item row not rendered")
	}
	if !strings.Contains(text, "...") {
		t.Errorf("label was not truncated: %q", text)
	}
	if !strings.HasSuffix(strings.TrimRight(text, "│ "), "Ctrl+Shift+P") {
		t.Errorf("hint missing or truncated at the row end: %q", text)
	}
	if got := lipgloss.Width(text); got != boxWidth {
		t.Errorf("row is %d cells, box is %d", got, boxWidth)
	}
}

func TestPickerDescriptionsDefaultStayAfterLabel(t *testing.T) {
	theme := ui.DefaultTheme()
	p := NewPicker("Open File", []PickerItem{{Label: "store.go", Description: "internal/store"}}, theme, "default")
	p.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	for _, row := range rawCells(p.View()) {
		if lx := findText(row, "store.go"); lx >= 0 {
			if hx := findText(row, "internal/store"); hx != lx+len("store.go")+2 {
				t.Errorf("description starts at %d, want %d (label + 2 spaces)", hx, lx+len("store.go")+2)
			}
			return
		}
	}
	t.Fatal("row not rendered")
}

func TestPickerRightAlignedRowsKeepMouseZones(t *testing.T) {
	theme := ui.DefaultTheme()
	p := NewPicker("Command Palette", []PickerItem{{Label: "Save File", Description: "Ctrl+S"}}, theme, "hint-zone")
	p.SetDescriptionsRightAligned(true)
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	compositeOverlayInFrame(t, p, 100, 30)
	awaitZone(t, p.itemZoneID(0))
}
