package app

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Capture a completed read but delay delivery to Update, as a real command
// can finish before a newer read while its message arrives afterwards.
func captureConfigReload(t *testing.T, m *Model) configReloadedMsg {
	t.Helper()
	updated, cmd := m.handleConfigFileChange()
	*m = updated.(Model)
	if cmd == nil {
		t.Fatal("config change did not schedule a read")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		msg = batch[0]()
	}
	result, ok := msg.(configReloadedMsg)
	if !ok {
		t.Fatalf("reload command returned %T", msg)
	}
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	return result
}

func TestConfigReloadLatestRequestWins(t *testing.T) {
	for _, tt := range []struct {
		name       string
		staleError bool
	}{{"old config", false}, {"old error", true}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			m := newViewTestModel(t, true)
			old := captureConfigReload(t, &m)
			latest := captureConfigReload(t, &m)
			old.Config = m.appCfg
			old.Config.UI.Theme = "github-light"
			pending, _ := m.handleConfigReloaded(old)
			m = pending.(Model)
			if m.activeThemeName != "nord" {
				t.Fatal("old read applied while the latest read was pending")
			}
			latest.Config = m.appCfg
			latest.Config.UI.Theme = "tokyo-night"
			updated, _ := m.handleConfigReloaded(latest)
			m = updated.(Model)
			old.Config = m.appCfg
			old.Config.UI.Theme = "nord"
			if tt.staleError {
				old.Err = errors.New("superseded disk error")
			}
			updated, cmd := m.handleConfigReloaded(old)
			m = updated.(Model)
			if m.appCfg.UI.Theme != "tokyo-night" || m.activeThemeName != "tokyo-night" || m.status != "Config reloaded" || cmd != nil {
				t.Fatalf("stale read replaced latest state: theme=%s active=%s status=%s", m.appCfg.UI.Theme, m.activeThemeName, m.status)
			}
		})
	}
}

func TestConfigReloadCannotUndoUserConfigChange(t *testing.T) {
	for _, tt := range []struct {
		name string
		save bool
	}{{"word wrap", false}, {"saved settings", true}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			m := newViewTestModel(t, true)
			old := captureConfigReload(t, &m)
			old.Config = m.appCfg
			if tt.save {
				m.openSettingsOverlay()
				cfg := m.appCfg
				cfg.Editor.WordWrap = true
				updated, _ := m.handleSettingsSaveResult(settingsSaveResultMsg{Config: cfg})
				m = updated.(Model)
			} else {
				m.toggleWordWrap()
			}
			updated, _ := m.handleConfigReloaded(old)
			m = updated.(Model)
			if !m.appCfg.Editor.WordWrap || !m.activeEditor().Config.WordWrap {
				t.Fatal("older disk read undid the user's config change")
			}
		})
	}
}

func TestConfigChangeWhileSettingsDirtyInvalidatesPendingRead(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newViewTestModel(t, true)
	old := captureConfigReload(t, &m)
	old.Config = m.appCfg
	old.Config.UI.Theme = "tokyo-night"
	m.openSettingsOverlay()
	m.previewSettingsTheme("github-light")
	updated, _ := m.handleConfigFileChange()
	m = updated.(Model)
	m.discardSettingsChanges()
	updated, _ = m.handleConfigReloaded(old)
	m = updated.(Model)
	if m.appCfg.UI.Theme != "nord" || m.activeThemeName != "nord" {
		t.Fatal("superseded read returned after discarding Settings")
	}
}
