package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// DeleteConfirmMode handles delete confirmation
type DeleteConfirmMode struct {
	BaseMode
	path string
}

func (m *DeleteConfirmMode) ID() ModeID { return ModeDeleteConfirm }

func (m *DeleteConfirmMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *DeleteConfirmMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *DeleteConfirmMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeDeleteConfirm, nil
}

func (m *DeleteConfirmMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}

// CloseConfirmMode handles close tab confirmation
type CloseConfirmMode struct {
	BaseMode
	tabIndex int
}

func (m *CloseConfirmMode) ID() ModeID { return ModeCloseConfirm }

func (m *CloseConfirmMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *CloseConfirmMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *CloseConfirmMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeCloseConfirm, nil
}

func (m *CloseConfirmMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}
