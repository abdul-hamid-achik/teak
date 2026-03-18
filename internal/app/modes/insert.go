package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// InsertMode handles insert mode (typing into buffer)
type InsertMode struct {
	BaseMode
}

func (m *InsertMode) ID() ModeID { return ModeInsert }

func (m *InsertMode) Enter(mdl *app.Model) tea.Cmd {
	return nil
}

func (m *InsertMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeInsert, nil
}

func (m *InsertMode) ShouldIntercept(msg tea.Msg) bool {
	// Insert mode passes through to editor - handles its own input
	return false
}
