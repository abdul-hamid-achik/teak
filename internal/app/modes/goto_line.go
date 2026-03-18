package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// GoToLineMode handles go-to-line input
type GoToLineMode struct {
	BaseMode
	input string
}

func (m *GoToLineMode) ID() ModeID { return ModeGoToLine }

func (m *GoToLineMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *GoToLineMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *GoToLineMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			// Will be handled by app.go
			return &NormalMode{}, ModeNormal, cmds

		case "esc", "escape":
			return &NormalMode{}, ModeNormal, cmds

		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}

		default:
			if len(msg.String()) == 1 {
				m.input += msg.String()
			}
		}
	}

	return m, ModeGoToLine, cmds
}

func (m *GoToLineMode) ShouldIntercept(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// GetInput returns the current line number input
func (m *GoToLineMode) GetInput() string { return m.input }
