package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// NewFileMode handles new file input
type NewFileMode struct {
	BaseMode
	input string
}

func (m *NewFileMode) ID() ModeID { return ModeNewFile }

func (m *NewFileMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *NewFileMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *NewFileMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
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

	return m, ModeNewFile, cmds
}

func (m *NewFileMode) ShouldIntercept(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// GetInput returns the file path input
func (m *NewFileMode) GetInput() string { return m.input }

// NewFolderMode handles new folder input
type NewFolderMode struct {
	BaseMode
	input string
}

func (m *NewFolderMode) ID() ModeID { return ModeNewFolder }

func (m *NewFolderMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *NewFolderMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *NewFolderMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
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

	return m, ModeNewFolder, cmds
}

func (m *NewFolderMode) ShouldIntercept(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// GetInput returns the folder path input
func (m *NewFolderMode) GetInput() string { return m.input }
