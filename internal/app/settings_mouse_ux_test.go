package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/diff"
	"teak/internal/filetree"
	"teak/internal/overlay"
	"teak/internal/settings"
	"teak/internal/ui"
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

	updatedAny, _ = updated.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      x + 3 + 12,
		Y:      y + 2 + 6,
	}))
	updated = updatedAny.(Model)
	if picker, ok := updated.overlayStack.Top().(*overlay.Picker); !ok || picker.ZoneID() != "settings-theme" {
		t.Fatalf("theme click opened %T, want settings theme picker", updated.overlayStack.Top())
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

func TestSettingsThemeEnterOpensExistingPicker(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	m.settingsM.SelectNextCategory()

	updatedAny, _ := m.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	picker, ok := updated.overlayStack.Top().(*overlay.Picker)
	if !ok {
		t.Fatalf("theme control opened %T, want the existing picker", updated.overlayStack.Top())
	}
	if got := picker.ZoneID(); got != "settings-theme" {
		t.Fatalf("theme picker zone = %q, want settings-theme", got)
	}
	if got, want := picker.FilteredCount(), len(ui.ThemeOptions()); got != want {
		t.Fatalf("theme picker options = %d, want %d", got, want)
	}
	cfg, err := updated.settingsM.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != m.appCfg.UI.Theme {
		t.Fatalf("opening the picker changed theme to %q", cfg.UI.Theme)
	}
}

func TestSettingsThemePickerStartsOnCurrentChoice(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	m.settingsM.SelectNextCategory()
	if !m.settingsM.SetThemeValue("material-palenight") {
		t.Fatal("could not select test theme")
	}

	updatedAny, _ := m.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
	picker := updatedAny.(Model).overlayStack.Top().(*overlay.Picker)
	_, selectCmd := picker.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if selectCmd == nil {
		t.Fatal("current picker item could not be selected")
	}
	selected := selectCmd().(overlay.PickerSelectMsg).Item.Value.(settingsThemePickerSelectMsg)
	if selected.ThemeID != "material-palenight" {
		t.Fatalf("initial picker theme = %q, want material-palenight", selected.ThemeID)
	}
}

func TestSettingsThemeSelectionPreviewsWithoutSaving(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	savedTheme := m.appCfg.UI.Theme
	oldHighlighter := m.activeEditor().Highlighter

	updatedAny, cmd := m.handlePickerSelect(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: settingsThemePickerSelectMsg{ThemeID: "github-light"},
	}})
	updated := updatedAny.(Model)
	if updated.activeThemeName != "github-light" {
		t.Fatalf("live theme = %q, want github-light", updated.activeThemeName)
	}
	if updated.appCfg.UI.Theme != savedTheme {
		t.Fatalf("preview changed saved app config to %q", updated.appCfg.UI.Theme)
	}
	cfg, err := updated.settingsM.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "github-light" || !updated.settingsM.Dirty() {
		t.Fatalf("settings theme = %q dirty=%v, want github-light and dirty", cfg.UI.Theme, updated.settingsM.Dirty())
	}
	if updated.activeEditor().Highlighter == oldHighlighter {
		t.Fatal("preview kept syntax tokens styled with the old theme")
	}
	if cmd == nil {
		t.Fatal("preview did not schedule asynchronous syntax recoloring")
	}
}

func TestSettingsDiscardRestoresThemePreview(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	savedTheme := m.appCfg.UI.Theme
	m.previewSettingsTheme("github-light")

	updatedAny, cmd := m.handleSettingsDiscard(settingsDiscardMsg{})
	updated := updatedAny.(Model)
	if updated.activeThemeName != savedTheme {
		t.Fatalf("live theme after discard = %q, want %q", updated.activeThemeName, savedTheme)
	}
	if updated.theme.Editor.GetBackground() != ui.ThemeByName(savedTheme).Editor.GetBackground() {
		t.Fatal("discard did not restore the saved theme styles")
	}
	if updated.showSettings {
		t.Fatal("discard left Settings open")
	}
	if cmd == nil {
		t.Fatal("discard did not schedule asynchronous syntax recoloring")
	}
}

func TestSettingsDiscardRestoresImplicitNordTheme(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	m.appCfg.UI.Theme = ""
	m.previewSettingsTheme("github-light")

	updatedAny, cmd := m.handleSettingsDiscard(settingsDiscardMsg{})
	updated := updatedAny.(Model)
	if updated.activeThemeName != "nord" || updated.theme.Editor.GetBackground() != ui.NordTheme().Editor.GetBackground() {
		t.Fatalf("discard with an empty saved theme left live theme %q, want nord", updated.activeThemeName)
	}
	if cmd == nil {
		t.Fatal("discard did not schedule Nord syntax recoloring")
	}
}

func TestSettingsResetThemeUpdatesPreview(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	m.settingsM.SelectNextCategory()
	m.previewSettingsTheme("github-light")

	updatedAny, cmd := m.updateSettings(tea.KeyPressMsg{Code: 'r'})
	updated := updatedAny.(Model)
	if updated.activeThemeName != "nord" {
		t.Fatalf("live theme after reset = %q, want nord", updated.activeThemeName)
	}
	if cmd == nil {
		t.Fatal("theme reset did not schedule asynchronous syntax recoloring")
	}
}

func TestDiffLoadedDuringThemePreviewUsesLiveTheme(t *testing.T) {
	m := newViewTestModel(t, true)
	m.applyTheme("github-light")
	m.tabBar.Tabs[m.activeTab].FilePath = "diff://main.go"
	preparedWithOldTheme := diff.New("main.go", []diff.DiffLine{{
		Right: "+package main", RightNum: 1, RightKind: diff.KindAdded,
	}}, ui.NordTheme())

	updatedAny, cmd := m.handleDiffLoaded(DiffLoadedMsg{
		Path: "main.go", View: &preparedWithOldTheme, TabIndex: m.activeTab,
	})
	updated := updatedAny.(Model)
	if _, ok := updated.diffViews[m.activeTab]; !ok {
		t.Fatal("prepared diff was not installed")
	}
	if cmd == nil {
		t.Fatal("stale prepared diff was not recolored with the live theme")
	}
}

func TestTreeLoadedDuringThemePreviewUsesLiveTheme(t *testing.T) {
	m := newViewTestModel(t, true)
	loaded := filetree.NewEmpty(m.rootDir, m.theme)
	loaded.Entries = []filetree.Entry{{Name: "main.go", Path: "main.go"}}
	m.applyTheme("github-light")
	updatedAny, cmd := m.handleTreeLoaded(treeLoadedMsg{
		Tree: loaded, Generation: m.treeRefreshGeneration,
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	got := updated.tree.View()
	updated.tree.SetTheme(updated.theme)
	if want := updated.tree.View(); got != want {
		t.Fatal("late tree load restored colors from before the theme preview")
	}
}
