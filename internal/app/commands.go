package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/diff"
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

// DiffLoadedMsg is sent when a diff has been computed.
type DiffLoadedMsg struct {
	Path     string
	Lines    []diff.DiffLine
	TabIndex int
	Err      error
}

// loadDiffCmd runs git diff and parses the result.
func loadDiffCmd(rootDir, relPath, status string, tabIndex int) tea.Cmd {
	return func() tea.Msg {
		absPath := filepath.Join(rootDir, relPath)

		// Check if path is a directory — skip diff
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			return DiffLoadedMsg{Path: relPath, Err: fmt.Errorf("%s is a directory", relPath), TabIndex: tabIndex}
		}

		// Untracked files: read file content directly, generate all-added lines
		if status == "??" || status == "U" {
			data, err := os.ReadFile(absPath)
			if err != nil {
				return DiffLoadedMsg{Path: relPath, Err: err, TabIndex: tabIndex}
			}
			lines := diff.AllAddedLines(string(data))
			return DiffLoadedMsg{Path: relPath, Lines: lines, TabIndex: tabIndex}
		}

		// Run git diff HEAD -- <file>
		cmd := exec.Command("git", "diff", "HEAD", "--", relPath)
		cmd.Dir = rootDir
		out, err := cmd.Output()
		if err != nil {
			// Try without HEAD for staged-only changes
			cmd2 := exec.Command("git", "diff", "--", relPath)
			cmd2.Dir = rootDir
			out, err = cmd2.Output()
			if err != nil {
				return DiffLoadedMsg{Path: relPath, Err: err, TabIndex: tabIndex}
			}
		}

		lines := diff.ParseUnifiedDiff(string(out))
		return DiffLoadedMsg{Path: relPath, Lines: lines, TabIndex: tabIndex}
	}
}
