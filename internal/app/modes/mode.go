package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// ModeID uniquely identifies a mode
type ModeID int

const (
	ModeNormal ModeID = iota
	ModeInsert
	ModeRename
	ModeGoToLine
	ModeSearch
	ModeSearchReplace
	ModeNewFile
	ModeNewFolder
	ModeDeleteConfirm
	ModeCloseConfirm
	ModeContextMenu
	ModeBranchPicker
	ModeSettings
	ModeSaveAs
	ModeCommandPalette
	ModeQuickOpen
	ModePluginKey
)

// Mode represents an input mode with its own update logic
type Mode interface {
	// ID returns the mode identifier
	ID() ModeID

	// Enter is called when entering this mode (for setup)
	Enter(m *app.Model) tea.Cmd

	// Exit is called when leaving this mode (for cleanup)
	Exit(m *app.Model) tea.Cmd

	// Update handles messages while in this mode
	// Returns: updated mode, new mode (if transitioning), commands
	Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd)

	// ShouldIntercept returns true if this mode should handle the message
	ShouldIntercept(msg tea.Msg) bool
}

// BaseMode provides default implementations for optional methods
type BaseMode struct{}

func (b BaseMode) Enter(m *app.Model) tea.Cmd { return nil }
func (b BaseMode) Exit(m *app.Model) tea.Cmd  { return nil }
func (b BaseMode) ShouldIntercept(msg tea.Msg) bool { return true }
