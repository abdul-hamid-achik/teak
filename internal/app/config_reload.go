package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
)

type configReloadedMsg struct {
	Config config.Config
	Err    error
}

func (m Model) isUserConfigPath(path string) bool {
	if path == "" {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(config.ConfigPath())
}

func reloadUserConfigCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return configReloadedMsg{Err: err}
		}
		return configReloadedMsg{Config: cfg}
	}
}

func (m Model) handleConfigFileChange() (tea.Model, tea.Cmd) {
	if m.showSettings && m.settingsM.Dirty() {
		m.status = "Config changed on disk; unsaved settings overlay kept"
		if m.watcher != nil {
			return m, m.watcher.listenCmd()
		}
		return m, nil
	}
	var cmds []tea.Cmd
	cmds = append(cmds, reloadUserConfigCmd())
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleConfigReloaded(msg configReloadedMsg) (tea.Model, tea.Cmd) {
	if m.showSettings && m.settingsM.Dirty() {
		return m, nil
	}
	if msg.Err != nil {
		m.status = fmt.Sprintf("Config reload failed; keeping previous settings: %v", msg.Err)
		return m, nil
	}
	cmd := m.applySavedSettings(msg.Config)
	m.status = "Config reloaded"
	if len(msg.Config.LoadWarnings) > 0 {
		m.status = "Config reloaded with warnings"
	}
	return m, cmd
}
