package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// ContextMenuMode handles right-click context menu
type ContextMenuMode struct {
	BaseMode
}

func (m *ContextMenuMode) ID() ModeID { return ModeContextMenu }

func (m *ContextMenuMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *ContextMenuMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *ContextMenuMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeContextMenu, nil
}

func (m *ContextMenuMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}

// BranchPickerMode handles git branch selection
type BranchPickerMode struct {
	BaseMode
}

func (m *BranchPickerMode) ID() ModeID { return ModeBranchPicker }

func (m *BranchPickerMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *BranchPickerMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *BranchPickerMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeBranchPicker, nil
}

func (m *BranchPickerMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}

// SettingsMode handles settings overlay
type SettingsMode struct {
	BaseMode
}

func (m *SettingsMode) ID() ModeID { return ModeSettings }

func (m *SettingsMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *SettingsMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *SettingsMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeSettings, nil
}

func (m *SettingsMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}
