package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// SaveAsMode handles save as input
type SaveAsMode struct {
	BaseMode
	input string
}

func (m *SaveAsMode) ID() ModeID { return ModeSaveAs }

func (m *SaveAsMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *SaveAsMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *SaveAsMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
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

	return m, ModeSaveAs, cmds
}

func (m *SaveAsMode) ShouldIntercept(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// GetInput returns the file path input
func (m *SaveAsMode) GetInput() string { return m.input }

// CommandPaletteMode handles command palette
type CommandPaletteMode struct {
	BaseMode
}

func (m *CommandPaletteMode) ID() ModeID { return ModeCommandPalette }

func (m *CommandPaletteMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *CommandPaletteMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *CommandPaletteMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeCommandPalette, nil
}

func (m *CommandPaletteMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}

// QuickOpenMode handles quick file open
type QuickOpenMode struct {
	BaseMode
	query string
}

func (m *QuickOpenMode) ID() ModeID { return ModeQuickOpen }

func (m *QuickOpenMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *QuickOpenMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *QuickOpenMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeQuickOpen, nil
}

func (m *QuickOpenMode) ShouldIntercept(msg tea.Msg) bool {
	return true
}

// GetInput returns the query input
func (m *QuickOpenMode) GetInput() string { return m.query }
