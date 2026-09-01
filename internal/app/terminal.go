package app

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/termpanel"
)

func (m Model) terminalPanelHeight() int {
	if m.modelState == nil || !m.showTerminal || m.height < compactTerminalHeight {
		return 0
	}
	avail := m.height - 3 // status divider, status, tab bar
	if avail < 4 {
		return 0
	}
	h := avail / 3
	if h < 6 {
		h = 6
	}
	if h > 16 {
		h = 16
	}
	if h > avail-2 {
		h = max(0, avail-2)
	}
	return h
}

func (m *Model) toggleTerminalPanel() tea.Cmd {
	if m.showTerminal {
		m.showTerminal = false
		if m.focus == FocusTerminal {
			m.setFocus(FocusEditor)
		}
		m.relayout()
		return nil
	}
	m.showTerminal = true
	m.setFocus(FocusTerminal)
	m.relayout()
	return m.ensureTerminalStarted()
}

func (m *Model) ensureTerminalStarted() tea.Cmd {
	if m.terminal.Running() {
		return m.terminal.Listen()
	}
	if err := m.terminal.Start(); err != nil {
		m.status = err.Error()
		return nil
	}
	return m.terminal.Listen()
}

func (m Model) handleTerminalOutput(msg termpanel.OutputMsg) (tea.Model, tea.Cmd) {
	m.terminal.ApplyOutput(msg)
	if !m.showTerminal {
		return m, nil
	}
	return m, m.terminal.Listen()
}
