package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/settings"
)

func TestSettingsMouseClickUsesCenteredModalCoordinates(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 80, 24
	m.showSettings = true
	m.settingsM = settings.New(m.theme, m.appCfg, "")
	m.settingsM.SetSize(m.width, m.height)

	x, y, _, _ := m.settingsModalGeometry()
	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		// Content origin is box border + padding. The UI tab starts after the
		// title/config rows, and the second tab selects User Interface.
		X: x + 3 + 12,
		Y: y + 2 + 4,
	}))
	updated := updatedAny.(Model)
	if got := updated.settingsM.SelectedCategory().ID; got != "ui" {
		t.Fatalf("category after centered modal click = %q, want ui", got)
	}
}

func TestSettingsEscapeWithDirtyValuesRequiresDiscardConfirmation(t *testing.T) {
	m := newViewTestModel(t, true)
	m.showSettings = true
	m.settingsM.IncrementIntValue()

	updatedAny, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated := updatedAny.(Model)
	if !updated.showSettings {
		t.Fatal("dirty settings closed without confirmation")
	}
	if updated.unsavedConfirm == nil {
		t.Fatal("dirty settings did not show a discard confirmation")
	}

	// The explicit discard action closes only Settings; cancel remains safe.
	updatedAny, _ = updated.Update(settingsDiscardMsg{})
	updated = updatedAny.(Model)
	if updated.showSettings {
		t.Fatal("explicit settings discard did not close the overlay")
	}
}
