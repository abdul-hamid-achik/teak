package app

import (
	"os"

	tea "charm.land/bubbletea/v2"
)

// FileSavedMsg is sent when a file has been saved successfully.
type FileSavedMsg struct {
	Path string
}

// FileErrorMsg is sent when a file operation fails.
type FileErrorMsg struct {
	Err error
}

// SaveFileCmd returns a command that saves the file.
func SaveFileCmd(saveFn func() error, path string) tea.Cmd {
	return func() tea.Msg {
		if err := saveFn(); err != nil {
			return FileErrorMsg{Err: err}
		}
		return FileSavedMsg{Path: path}
	}
}

// SwitchTabMsg requests switching to a specific tab.
type SwitchTabMsg struct {
	Index int
}

// CloseTabMsg requests closing a specific tab.
type CloseTabMsg struct {
	Index int
}

// FileLoadedMsg is sent when an async file read completes.
type FileLoadedMsg struct {
	Path     string
	Data     []byte
	TabIndex int // which tab to populate
	ForceNew bool // skip replaceable tab logic
}

// FileLoadErrorMsg is sent when an async file read fails.
type FileLoadErrorMsg struct {
	Path string
	Err  error
}

// loadFileCmd returns a command that reads a file asynchronously.
func loadFileCmd(path string, tabIndex int, forceNew bool) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return FileLoadErrorMsg{Path: path, Err: err}
		}
		return FileLoadedMsg{Path: path, Data: data, TabIndex: tabIndex, ForceNew: forceNew}
	}
}

// LspReadyMsg is sent when an LSP client finishes initializing.
type LspReadyMsg struct {
	FilePath string
}
