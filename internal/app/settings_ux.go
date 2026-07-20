package app

import (
	"charm.land/lipgloss/v2"
	"teak/internal/config"
	"teak/internal/overlay"
	"teak/internal/settings"
	"teak/internal/ui"
)

const (
	settingsModalMaxWidth  = 72
	settingsModalMaxHeight = 22
)

// settingsModalGeometry is shared by View and mouse routing so the clickable
// content cannot drift from the centered modal after a resize.
func (m Model) settingsModalGeometry() (x, y, width, height int) {
	width = min(settingsModalMaxWidth, max(1, m.width))
	height = min(settingsModalMaxHeight, max(1, m.height))
	x = max(0, (m.width-width)/2)
	y = max(0, (m.height-height)/2)
	return x, y, width, height
}

func (m *Model) setSettingsOverlaySize() {
	_, _, width, height := m.settingsModalGeometry()
	// border + horizontal padding consume six cells, while border + vertical
	// padding consume four lines. Settings itself clamps these to safe values.
	m.settingsM.SetSize(width-6, height-4)
}

func (m Model) requestCloseSettings() Model {
	if !m.settingsM.Dirty() {
		m.showSettings = false
		return m
	}
	if m.unsavedConfirm != nil {
		return m
	}
	m.cancelActiveEditorDrag()
	confirm := overlay.NewConfirm(
		"Discard Settings Changes?",
		"Your modified settings have not been saved.",
		[]string{"Discarding restores the settings that were active when this overlay opened."},
		[]overlay.Button{
			{Label: "Discard", Style: lipgloss.NewStyle().Background(ui.Nord11).Foreground(ui.Nord6).Padding(0, 2), Action: settingsDiscardMsg{}},
			{Label: "Keep Editing", Action: settingsKeepEditingMsg{}},
		},
		m.theme,
	)
	confirm.SetWidth(min(50, max(1, m.width)))
	m.unsavedConfirm = confirm
	return m
}

func (m *Model) discardSettingsChanges() {
	path := m.settingsM.ConfigPath()
	m.settingsM = settings.New(m.theme, m.appCfg, path)
	m.setSettingsOverlaySize()
	m.showSettings = false
	m.status = "Settings changes discarded"
}

func (m *Model) openSettingsOverlay() {
	m.cancelActiveEditorDrag()
	if !m.showSettings {
		// Start each newly opened editor from the saved configuration. This is
		// important after a prior discard or an external config reload.
		path := m.settingsM.ConfigPath()
		if path == "" {
			path = config.ConfigPath()
		}
		m.settingsM = settings.New(m.theme, m.appCfg, path)
	}
	m.showSettings = true
	m.setSettingsOverlaySize()
}
