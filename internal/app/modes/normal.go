package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// NormalMode handles normal editing mode (not typing)
type NormalMode struct {
	BaseMode
}

func (m *NormalMode) ID() ModeID { return ModeNormal }

func (m *NormalMode) Enter(mdl *app.Model) tea.Cmd {
	return nil
}

func (m *NormalMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeNormal, nil
}

func (m *NormalMode) ShouldIntercept(msg tea.Msg) bool {
	// Normal mode doesn't intercept - keybindings handle input
	return false
}
