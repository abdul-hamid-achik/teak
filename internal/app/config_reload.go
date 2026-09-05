package app

import (
	"fmt"
	"path/filepath"
	"reflect"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/settings"
)

type configReloadedMsg struct {
	Generation uint64
	Config     config.Config
	Err        error
}

func (m Model) isUserConfigPath(path string) bool {
	if path == "" {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(config.ConfigPath())
}

func reloadUserConfigCmd(generation uint64) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return configReloadedMsg{Generation: generation, Err: err}
		}
		return configReloadedMsg{Generation: generation, Config: cfg}
	}
}

func (m Model) handleConfigFileChange() (tea.Model, tea.Cmd) {
	m.configReloadGeneration++
	if m.showSettings && m.settingsM.Dirty() {
		m.status = "Config changed on disk; unsaved settings overlay kept"
		if m.watcher != nil {
			return m, m.watcher.listenCmd()
		}
		return m, nil
	}
	var cmds []tea.Cmd
	cmds = append(cmds, reloadUserConfigCmd(m.configReloadGeneration))
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleConfigReloaded(msg configReloadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.configReloadGeneration {
		return m, nil
	}
	if m.showSettings && m.settingsM.Dirty() {
		return m, nil
	}
	if msg.Err != nil {
		m.status = fmt.Sprintf("Config reload failed; keeping previous settings: %v", msg.Err)
		return m, nil
	}
	// Atomic Settings saves also notify the watcher. Keep the selection and
	// save feedback when that notification just echoes the applied config.
	if reflect.DeepEqual(msg.Config, m.appCfg) {
		return m, nil
	}
	cmd := m.applySavedSettings(msg.Config)
	if m.showSettings {
		path := m.settingsM.ConfigPath()
		m.settingsM = settings.New(m.theme, msg.Config, path)
		m.setSettingsOverlaySize()
	}
	m.status = "Config reloaded"
	if len(msg.Config.LoadWarnings) > 0 {
		m.status = "Config reloaded with warnings"
	}
	return m, cmd
}
