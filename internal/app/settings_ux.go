package app

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/overlay"
	"teak/internal/settings"
	"teak/internal/ui"
)

const (
	settingsModalMaxWidth  = 72
	settingsModalMaxHeight = 22
)

type settingsThemePickerSelectMsg struct{ ThemeID string }

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
			{Label: "Discard", Style: m.dangerButtonStyle(), Action: settingsDiscardMsg{}},
			{Label: "Keep Editing", Action: settingsKeepEditingMsg{}},
		},
		m.theme,
	)
	confirm.SetWidth(min(50, max(1, m.width)))
	m.unsavedConfirm = confirm
	return m
}

func (m Model) openSettingsThemePicker() (tea.Model, tea.Cmd) {
	items := make([]overlay.PickerItem, 0, len(ui.ThemeOptions()))
	current := ""
	if setting := m.settingsM.SelectedSetting(); setting != nil && setting.ID == "ui.theme" {
		current, _ = setting.Value.(string)
	}
	selected := 0
	for index, option := range ui.ThemeOptions() {
		variant := "Dark"
		if option.Variant == ui.ThemeLight {
			variant = "Light"
		}
		if option.ID == current {
			selected = index
		}
		items = append(items, overlay.PickerItem{
			Label:       option.Name,
			Description: variant + " • " + option.ID,
			Search:      option.ID,
			Value:       settingsThemePickerSelectMsg{ThemeID: option.ID},
		})
	}
	picker := overlay.NewPicker("Choose theme", items, m.theme, "settings-theme")
	picker.SetSize(min(64, max(1, m.width-4)), min(20, max(1, m.height-4)))
	picker.SetCursor(selected)
	m.overlayStack.Push(picker)
	return m, picker.Focus()
}

func (m *Model) previewSettingsTheme(name string) tea.Cmd {
	if !m.settingsM.SetThemeValue(name) {
		m.settingsM.SetStatus("Theme is no longer available")
		return nil
	}
	cmd := m.applyTheme(name)
	if m.settingsM.Dirty() {
		m.settingsM.SetStatus("Theme preview active — press Ctrl+S to save or Esc to cancel")
	} else {
		m.settingsM.SetStatus("Saved theme restored")
	}
	return cmd
}

func (m *Model) applyTheme(name string) tea.Cmd {
	if name == "" {
		name = "nord"
	}
	if !ui.HasTheme(name) || name == m.activeThemeName {
		return nil
	}
	theme := ui.ThemeByName(name)
	m.theme = theme
	m.activeThemeName = name

	m.tabBar.SetTheme(theme)
	m.tree.SetTheme(theme)
	m.treeContextMenu.SetTheme(theme)
	m.gitPanel.SetTheme(theme)
	m.branchPickerM.SetTheme(theme)
	m.gitContextMenu.SetTheme(theme)
	m.helpM.SetTheme(theme)
	m.problemsPanel.SetTheme(theme)
	m.settingsM.SetTheme(theme)
	m.debuggerPanel.SetTheme(theme)
	m.agentPanel.SetTheme(theme)
	m.terminal.SetTheme(theme)
	m.searchM.SetTheme(theme)
	if m.welcome != nil {
		m.welcome.SetTheme(theme)
	}
	if m.unsavedConfirm != nil {
		m.unsavedConfirm.SetTheme(theme)
	}
	m.overlayStack.SetTheme(theme)

	cmds := make([]tea.Cmd, 0, len(m.editors)+len(m.diffViews))
	for i := range m.editors {
		if cmd := m.editors[i].SetTheme(theme); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for tab, view := range m.diffViews {
		if cmd := view.SetTheme(theme); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.diffViews[tab] = view
	}
	return tea.Batch(cmds...)
}

func (m *Model) discardSettingsChanges() tea.Cmd {
	cmd := m.applyTheme(m.appCfg.UI.Theme)
	path := m.settingsM.ConfigPath()
	m.settingsM = settings.New(m.theme, m.appCfg, path)
	m.setSettingsOverlaySize()
	m.showSettings = false
	m.status = "Settings changes discarded"
	return cmd
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
