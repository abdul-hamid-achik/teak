package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
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
	updatedAny, _ = model.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter}) // nord -> dracula
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
	if !strings.Contains(final.settingsM.Status(), "restart Teak") {
		t.Errorf("theme change did not explicitly require restart: %q", final.settingsM.Status())
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
}

type errSettingsTest struct{}

func (errSettingsTest) Error() string { return "disk full" }
