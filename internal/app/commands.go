package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/diff"
	"teak/internal/git"
	"teak/internal/text"
	"teak/internal/ui"
)

// FileSavedMsg is sent when a file has been saved successfully.
type FileSavedMsg struct {
	Path             string
	RequestID        int
	WatcherWatermark uint64
}

// FileErrorMsg is sent when a file operation fails.
type FileErrorMsg struct {
	Path      string
	RequestID int
	Err       error
}

// SaveFileCmd returns a command that saves the file.
func SaveFileCmd(saveFn func() error, path string, requestID int) tea.Cmd {
	return func() tea.Msg {
		if err := saveFn(); err != nil {
			return FileErrorMsg{Path: path, RequestID: requestID, Err: err}
		}
		return FileSavedMsg{Path: path, RequestID: requestID}
	}
}

// SaveSnapshotCmd writes an immutable rope snapshot. It deliberately never
// touches a Buffer: save completion is reconciled on the Bubble Tea UI
// goroutine, where later edits can remain dirty.
func SaveSnapshotCmd(snapshot *text.Rope, path string, requestID int) tea.Cmd {
	return func() tea.Msg {
		if err := text.WriteRopeAtomically(path, snapshot); err != nil {
			return FileErrorMsg{Path: path, RequestID: requestID, Err: err}
		}
		return FileSavedMsg{Path: path, RequestID: requestID}
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
	Path       string
	Data       []byte
	Snapshot   *text.Rope // production loads prepare this off the UI goroutine; Data is legacy/test fallback
	LineEnding text.LineEnding
	// RecoveredDirty marks crash-recovery installs: the content comes from a
	// recovery record, not from disk, so the buffer must surface as unsaved.
	RecoveredDirty bool
	EditorID       uint64 // stable target identity; never an editor slice index
	RequestID      uint64 // monotonically increasing request identity
	TabIndex       int    // legacy test-only fallback, ignored for identified requests
	ForceNew       bool   // skip replaceable tab logic
}

// FileLoadErrorMsg is sent when an async file read fails.
type FileLoadErrorMsg struct {
	Path      string
	EditorID  uint64
	RequestID uint64
	Err       error
}

const maxEditorFileBytes int64 = 64 << 20

var errEditorFileTooLarge = fmt.Errorf("file exceeds Teak's %d MiB editor limit", maxEditorFileBytes>>20)

// errEditorFileNotRegular prevents an open request from blocking on a FIFO or
// device. Editor buffers model finite files; directories and stream-like paths
// have no safe bounded read contract here.
var errEditorFileNotRegular = fmt.Errorf("editor input is not a regular file")

// readEditorFile bounds memory before allocation. Stat catches sparse files
// cheaply; the limited stream protects against a concurrently growing file.
func readEditorFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openEditorInput(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, _, err := readOpenedEditorFile(ctx, file, path, maxEditorFileBytes)
	return data, err
}

// readOpenedEditorFile reads an already-opened input while preserving the
// editor's regular-file and memory limits. The caller owns file and must close
// it. Keeping this separate lets session restoration read through a pinned
// os.Root instead of reopening a path that may have changed since validation.
func readOpenedEditorFile(ctx context.Context, file *os.File, path string, limit int64) ([]byte, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if limit <= 0 {
		return nil, nil, errEditorFileTooLarge
	}
	if info, err := file.Stat(); err != nil {
		return nil, nil, err
	} else {
		if !info.Mode().IsRegular() {
			return nil, info, fmt.Errorf("%w: %s", errEditorFileNotRegular, path)
		}
		if info.Size() > limit {
			return nil, info, errEditorFileTooLarge
		}
		data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, limit+1))
		if err != nil {
			return nil, info, err
		}
		if int64(len(data)) > limit {
			return nil, info, errEditorFileTooLarge
		}
		if err := ctx.Err(); err != nil {
			return nil, info, err
		}
		return data, info, nil
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// loadFileCmd returns a command that reads a file asynchronously.
func loadFileCmd(ctx context.Context, path string, editorID, requestID uint64, forceNew bool) tea.Cmd {
	return func() tea.Msg {
		data, err := readEditorFile(ctx, path)
		if err != nil {
			return FileLoadErrorMsg{Path: path, EditorID: editorID, RequestID: requestID, Err: err}
		}
		// readEditorFile returned a fresh allocation owned by this command.
		// Normalize CRLF off the UI goroutine; the buffer keeps LF content and
		// remembers the original convention for save.
		normalized, ending, prepErr := text.PrepareLoadedBytes(data)
		if prepErr != nil {
			return FileLoadErrorMsg{Path: path, EditorID: editorID, RequestID: requestID, Err: prepErr}
		}
		return FileLoadedMsg{Path: path, Snapshot: text.NewOwned(normalized), LineEnding: ending, EditorID: editorID, RequestID: requestID, ForceNew: forceNew}
	}
}

// LspReadyMsg is sent when an LSP client finishes initializing.
type LspReadyMsg struct {
	FilePath    string
	OpenVersion int
}

// DiffLoadedMsg is sent when a diff has been parsed and highlighted outside
// Bubble Tea's Update loop.
type DiffLoadedMsg struct {
	Path      string
	View      *diff.Model
	EditorID  uint64
	RequestID uint64
	TabIndex  int // legacy test-only fallback
	Err       error
}

// SaveAllAndQuitMsg is sent when the user confirms saving all dirty buffers before quitting.
type SaveAllAndQuitMsg struct{}

// QuitWithoutSavingMsg is sent when the user confirms quitting without saving.
type QuitWithoutSavingMsg struct{}

// ForceCloseTabMsg closes a tab without saving.
type ForceCloseTabMsg struct {
	Index int
}

// SaveAndCloseTabMsg saves a tab then closes it.
type SaveAndCloseTabMsg struct {
	Index int
}

// FindNextMsg navigates to the next search result.
type FindNextMsg struct{}

// FindPrevMsg navigates to the previous search result.
type FindPrevMsg struct{}

// loadDiffCmd runs git diff and parses the result.
func loadDiffCmd(ctx context.Context, rootDir, relPath, status string, editorID, requestID uint64, theme ui.Theme, viewportHeight int) tea.Cmd {
	return func() tea.Msg {
		result := func(lines []diff.DiffLine, err error) tea.Msg {
			if err != nil {
				return DiffLoadedMsg{Path: relPath, EditorID: editorID, RequestID: requestID, Err: err}
			}
			if err := ctx.Err(); err != nil {
				return DiffLoadedMsg{Path: relPath, EditorID: editorID, RequestID: requestID, Err: err}
			}
			view := diff.New(relPath, lines, theme)
			if !view.PrepareViewport(ctx, 0, max(1, viewportHeight)) {
				err := ctx.Err()
				if err == nil {
					err = fmt.Errorf("diff highlighting did not complete")
				}
				return DiffLoadedMsg{Path: relPath, EditorID: editorID, RequestID: requestID, Err: err}
			}
			if err := ctx.Err(); err != nil {
				return DiffLoadedMsg{Path: relPath, EditorID: editorID, RequestID: requestID, Err: err}
			}
			return DiffLoadedMsg{Path: relPath, View: &view, EditorID: editorID, RequestID: requestID}
		}
		absPath := filepath.Join(rootDir, relPath)

		// Check if path is a directory — skip diff
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			return result(nil, fmt.Errorf("%s is a directory", relPath))
		}

		// Untracked files: read file content directly, generate all-added lines
		if status == "??" || status == "U" {
			data, err := readEditorFile(ctx, absPath)
			if err != nil {
				return result(nil, err)
			}
			lines := diff.AllAddedLines(string(data))
			return result(lines, nil)
		}

		out, err := git.DiffOutput(ctx, rootDir, relPath)
		if err != nil {
			return result(nil, err)
		}
		if int64(len(out)) > maxEditorFileBytes {
			return result(nil, fmt.Errorf("diff exceeds %d-byte limit", maxEditorFileBytes))
		}

		lines := diff.ParseUnifiedDiff(string(out))
		return result(lines, nil)
	}
}
