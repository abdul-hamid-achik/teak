package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// RenameSubmittedMsg is sent when rename is submitted
type RenameSubmittedMsg struct {
	NewName string
}

// RenameMode handles rename symbol input
type RenameMode struct {
	BaseMode
	input string
}

func (m *RenameMode) ID() ModeID { return ModeRename }

func (m *RenameMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *RenameMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *RenameMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if m.input != "" {
				cmds = append(cmds, func() tea.Msg {
					return RenameSubmittedMsg{NewName: m.input}
				})
			}
			return &NormalMode{}, ModeNormal, cmds

		case "esc", "escape":
			return &NormalMode{}, ModeNormal, cmds

		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}

		case "ctrl+u":
			m.input = ""

		default:
			if len(msg.String()) == 1 {
				m.input += msg.String()
			}
		}
	}

	return m, ModeRename, cmds
}

func (m *RenameMode) ShouldIntercept(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// GetInput returns the current rename input
func (m *RenameMode) GetInput() string { return m.input }
