package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// Manager tracks the current input mode and handles transitions
type Manager struct {
	current  Mode
	previous ModeID
	history  []ModeID
}

// NewManager creates a mode manager starting in Normal mode
func NewManager() *Manager {
	return &Manager{
		current:  &NormalMode{},
		previous: ModeNormal,
	}
}

// Current returns the current mode
func (m *Manager) Current() Mode { return m.current }

// CurrentID returns the current mode ID
func (m *Manager) CurrentID() ModeID { return m.current.ID() }

// Is returns true if currently in the given mode
func (m *Manager) Is(mode ModeID) bool { return m.current.ID() == mode }

// IsNormal returns true if in normal editing mode
func (m *Manager) IsNormal() bool { return m.current.ID() == ModeNormal }

// IsInputMode returns true if in any input mode (typing into a field)
func (m *Manager) IsInputMode() bool {
	switch m.current.ID() {
	case ModeRename, ModeGoToLine, ModeNewFile, ModeNewFolder, ModeSaveAs,
		ModeSearch, ModeSearchReplace, ModeCommandPalette, ModeQuickOpen:
		return true
	default:
		return false
	}
}

// IsOverlayMode returns true if in any overlay mode (dialogs, menus)
func (m *Manager) IsOverlayMode() bool {
	switch m.current.ID() {
	case ModeSettings, ModeContextMenu, ModeBranchPicker, ModeDeleteConfirm,
		ModeCloseConfirm, ModeCommandPalette:
		return true
	default:
		return false
	}
}

// Transition switches to a new mode
func (m *Manager) Transition(mdl *app.Model, newMode ModeID) []tea.Cmd {
	var cmds []tea.Cmd

	// Call exit on current mode
	if cmd := m.current.Exit(mdl); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Save previous mode
	m.previous = m.current.ID()
	m.history = append(m.history, m.previous)

	// Create and enter new mode
	m.current = m.createMode(newMode)
	if cmd := m.current.Enter(mdl); cmd != nil {
		cmds = append(cmds, cmd)
	}

	return cmds
}

// Back returns to the previous mode
func (m *Manager) Back(mdl *app.Model) []tea.Cmd {
	if m.previous == ModeNormal && len(m.history) > 0 {
		// Skip back through history
		prevIdx := len(m.history) - 1
		m.previous = m.history[prevIdx]
		m.history = m.history[:prevIdx]
	}
	return m.Transition(mdl, m.previous)
}

// Push forces a mode onto the stack (for nested modes)
func (m *Manager) Push(mdl *app.Model, newMode ModeID) []tea.Cmd {
	m.history = append(m.history, m.current.ID())
	return m.Transition(mdl, newMode)
}

func (m *Manager) createMode(id ModeID) Mode {
	switch id {
	case ModeNormal:
		return &NormalMode{}
	case ModeInsert:
		return &InsertMode{}
	case ModeRename:
		return &RenameMode{}
	case ModeGoToLine:
		return &GoToLineMode{}
	case ModeSearch:
		return &SearchMode{}
	case ModeSearchReplace:
		return &SearchReplaceMode{}
	case ModeNewFile:
		return &NewFileMode{}
	case ModeNewFolder:
		return &NewFolderMode{}
	case ModeDeleteConfirm:
		return &DeleteConfirmMode{}
	case ModeCloseConfirm:
		return &CloseConfirmMode{}
	case ModeContextMenu:
		return &ContextMenuMode{}
	case ModeBranchPicker:
		return &BranchPickerMode{}
	case ModeSettings:
		return &SettingsMode{}
	case ModeSaveAs:
		return &SaveAsMode{}
	case ModeCommandPalette:
		return &CommandPaletteMode{}
	case ModeQuickOpen:
		return &QuickOpenMode{}
	default:
		return &NormalMode{}
	}
}

// Update processes a message through the current mode
func (m *Manager) Update(msg tea.Msg, mdl *app.Model) (tea.Model, []tea.Cmd) {
	if !m.current.ShouldIntercept(msg) {
		return mdl, nil
	}

	updatedMode, transitionTo, cmds := m.current.Update(msg)
	m.current = updatedMode

	if transitionTo != ModeNormal && transitionTo != m.current.ID() {
		// Mode wants to transition
		cmds = append(cmds, func() tea.Msg {
			return ModeTransitionMsg{To: transitionTo}
		})
	}

	return mdl, cmds
}

// ModeTransitionMsg is sent when a mode wants to transition
type ModeTransitionMsg struct {
	To ModeID
}
