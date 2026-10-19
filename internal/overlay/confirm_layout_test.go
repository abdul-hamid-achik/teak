package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

// TestConfirmButtonZonesClickable verifies the mouse zones survive both the
// one-row and the stacked layouts: a click in the middle of each button's zone
// must activate that button.
func TestConfirmButtonZonesClickable(t *testing.T) {
	theme := ui.DefaultTheme()
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"one row", 140, 40},
		{"stacked", 44, 24},
	} {
		for idx := range 3 {
			t.Run(tc.name, func(t *testing.T) {
				c := NewConfirm("Unsaved Changes", "msg", []string{"a.go"}, quitButtons(theme), theme)
				c.SetWidth(min(50, tc.width))
				c.SetScreenWidth(tc.width)
				compositeOverlayInFrame(t, c, tc.width, tc.height)
				z := awaitZone(t, confirmZoneID(idx))
				click := tea.MouseClickMsg{X: (z.StartX + z.EndX) / 2, Y: z.StartY, Button: tea.MouseLeft}
				updated, cmd := c.Update(click)
				if !updated.IsDismissed() || cmd == nil {
					t.Fatalf("click on button %d did not activate it", idx)
				}
				want := quitButtons(theme)[idx].Action
				if got := cmd(); got != want {
					t.Fatalf("click on button %d produced %v, want %v", idx, got, want)
				}
			})
		}
	}
}

// Without a known screen size the default-width dialog still fits its buttons
// on one row (the app never forwards WindowSizeMsg to the quit confirm).
func TestConfirmWidensForButtonsWithoutScreenSize(t *testing.T) {
	theme := ui.DefaultTheme()
	c := NewConfirm("Unsaved Changes", "msg", nil, quitButtons(theme), theme)
	rows, _ := buttonRows(t, c.View(), "Save All & Quit", "Quit Without Saving", "Cancel")
	if rows[0] != rows[1] || rows[1] != rows[2] {
		t.Fatalf("buttons not on one row: %v", rows)
	}
	// A caller that narrowed the dialog for a small terminal is not widened.
	c.SetWidth(30)
	rows, _ = buttonRows(t, c.View(), "Save All & Quit", "Quit Without Saving", "Cancel")
	if !(rows[0] < rows[1] && rows[1] < rows[2]) {
		t.Fatalf("narrow dialog should stack its buttons: %v", rows)
	}
}
