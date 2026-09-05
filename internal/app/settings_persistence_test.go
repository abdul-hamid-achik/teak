package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/overlay"
	"teak/internal/settings"
)

func TestSettingsSavePersistsAndAppliesSafeChangesLive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Enabled = false
	cfg.Session.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	model.showSettings = true
	model.settingsM = settings.New(model.theme, cfg, t.TempDir()+"/config.toml")

	// Editor: tab size, insert-tabs, auto-indent, format-on-save, word-wrap.
	updatedAny, _ := model.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updatedAny.(Model) // tab size 4 -> 5
	for range 4 {
		updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updatedAny.(Model)
		updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updatedAny.(Model)
	}
	updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updatedAny.(Model)
	updatedAny, _ = model.handlePickerSelect(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: settingsThemePickerSelectMsg{ThemeID: "github-light"},
	}})
	model = updatedAny.(Model)
	updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updatedAny.(Model)
	updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter}) // show tree false
	model = updatedAny.(Model)

	updatedAny, cmd := model.updateSettings(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("settings save returned no command")
	}
	result, ok := cmd().(settingsSaveResultMsg)
	if !ok {
		t.Fatalf("save command returned %T, want settingsSaveResultMsg", cmd())
	}
	if result.Err != nil {
		t.Fatalf("save command failed: %v", result.Err)
	}
	finalAny, _ := updated.Update(result)
	final := finalAny.(Model)

	if final.appCfg.Editor.TabSize != 5 || !final.appCfg.Editor.InsertTabs || final.appCfg.Editor.AutoIndent || !final.appCfg.Editor.WordWrap || !final.appCfg.Editor.FormatOnSave {
		t.Errorf("app config did not retain saved editor values: %+v", final.appCfg.Editor)
	}
	if final.showTree {
		t.Error("show_tree was not applied live")
	}
	ed := final.activeEditor()
	if ed.Config.TabSize != 5 || !ed.Config.InsertTabs || ed.Config.AutoIndent || !ed.Config.WordWrap {
		t.Errorf("editor config was not applied live: %+v", ed.Config)
	}
	if final.settingsM.Dirty() || !final.showSettings {
		t.Error("successful save should retain the overlay and clear only its dirty state")
	}
	if final.appCfg.UI.Theme != "github-light" || final.activeThemeName != "github-light" {
		t.Fatalf("theme was not saved and kept live: config=%q active=%q", final.appCfg.UI.Theme, final.activeThemeName)
	}
	if strings.Contains(final.settingsM.Status(), "restart Teak") {
		t.Errorf("saved live theme still requires restart: %q", final.settingsM.Status())
	}
	saved, err := os.ReadFile(final.settingsM.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `theme = "github-light"`) {
		t.Fatalf("saved config does not contain the selected theme:\n%s", saved)
	}
}

func TestSettingsSaveErrorKeepsOverlayAndValues(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Enabled = false
	cfg.Session.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	model.showSettings = true
	model.settingsM = settings.New(model.theme, cfg, t.TempDir()+"/config.toml")
	updatedAny, _ := model.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updatedAny.(Model)
	model.previewSettingsTheme("github-light")
	// Inject a filesystem-style asynchronous failure to verify Settings keeps
	// the values available for retry rather than closing or resetting the form.
	updatedAny, _ = model.Update(settingsSaveResultMsg{Err: errSettingsTest{}})
	updated := updatedAny.(Model)
	if !updated.showSettings || updated.settingsM.Dirty() == false {
		t.Fatal("save failure closed the overlay or discarded the current values")
	}
	if !strings.Contains(updated.settingsM.Status(), "Could not save settings") {
		t.Errorf("save error was not shown in Settings: %q", updated.settingsM.Status())
	}
	if updated.activeThemeName != "github-light" || updated.appCfg.UI.Theme != cfg.UI.Theme {
		t.Fatalf("save failure changed preview/config: active=%q saved=%q", updated.activeThemeName, updated.appCfg.UI.Theme)
	}
}

func TestApplySavedSettingsChangesThemeLive(t *testing.T) {
	m := newViewTestModel(t, true)
	cfg := m.appCfg
	cfg.UI.Theme = "tokyo-night"
	oldHighlighter := m.activeEditor().Highlighter

	cmd := m.applySavedSettings(cfg)
	if m.activeThemeName != "tokyo-night" || m.appCfg.UI.Theme != "tokyo-night" {
		t.Fatalf("theme reload left config=%q active=%q", m.appCfg.UI.Theme, m.activeThemeName)
	}
	if m.activeEditor().Highlighter == oldHighlighter {
		t.Fatal("theme reload kept syntax tokens styled with the previous theme")
	}
	if cmd == nil {
		t.Fatal("theme reload did not schedule asynchronous syntax recoloring")
	}
}

func TestConfigReloadRefreshesCleanSettingsTheme(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	cfg := m.appCfg
	cfg.UI.Theme = "tokyo-night"

	updatedAny, _ := m.handleConfigReloaded(configReloadedMsg{Config: cfg})
	updated := updatedAny.(Model)
	settingsConfig, err := updated.settingsM.Config()
	if err != nil {
		t.Fatal(err)
	}
	if settingsConfig.UI.Theme != "tokyo-night" {
		t.Fatalf("clean Settings still shows %q after config reload", settingsConfig.UI.Theme)
	}
}

func TestConfigReloadAfterSettingsSaveKeepsSelectionAndFeedback(t *testing.T) {
	m := newViewTestModel(t, true)
	m.openSettingsOverlay()
	m.settingsM.SelectNextCategory()
	m.previewSettingsTheme("github-light")
	cfg, err := m.settingsM.Config()
	if err != nil {
		t.Fatal(err)
	}
	savedAny, _ := m.handleSettingsSaveResult(settingsSaveResultMsg{Config: cfg})
	saved := savedAny.(Model)
	updatedAny, cmd := saved.handleConfigReloaded(configReloadedMsg{Generation: saved.configReloadGeneration, Config: cfg})
	updated := updatedAny.(Model)
	if updated.settingsM.SelectedCategory().ID != "ui" {
		t.Fatal("watcher echo of Settings save reset the selected category")
	}
	if updated.settingsM.Status() != "Settings saved and applied" || updated.status != "Settings saved and applied" {
		t.Fatal("watcher echo of Settings save erased persistence feedback")
	}
	if cmd != nil {
		t.Fatal("identical config reload scheduled unnecessary work")
	}
}

type errSettingsTest struct{}

func (errSettingsTest) Error() string { return "disk full" }
