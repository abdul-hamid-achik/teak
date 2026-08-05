package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/plugin"
)

type saveAsDestinationResolutionMsg struct {
	RequestID int
	Path      string
	Overwrite bool
}

func (m Model) saveAsDestinationReserved(path string) bool {
	var err error
	path, err = m.normalizeEditorFilePath(path)
	if err != nil {
		return false
	}
	for _, req := range m.pendingSaves {
		if req.SaveAs && cleanExternalConflictPath(req.Path) == path {
			return true
		}
		if req.QueuedSaveAs && cleanExternalConflictPath(req.QueuedPath) == path {
			return true
		}
	}
	return false
}

func (m Model) saveAsDestinationReservedByOtherEditor(editorID uint64, path string) bool {
	var err error
	path, err = m.normalizeEditorFilePath(path)
	if err != nil {
		return false
	}
	for _, req := range m.pendingSaves {
		if req.EditorID == editorID {
			continue
		}
		if req.SaveAs && cleanExternalConflictPath(req.Path) == path {
			return true
		}
		if req.QueuedSaveAs && cleanExternalConflictPath(req.QueuedPath) == path {
			return true
		}
	}
	return false
}

func (m Model) saveAsDestinationOpenInAnotherEditor(editorID uint64, path string) bool {
	var err error
	path, err = m.normalizeEditorFilePath(path)
	if err != nil {
		return false
	}
	for index := range m.editors {
		if m.editors[index].ID() == editorID {
			continue
		}
		editorPath, editorErr := m.normalizeEditorFilePath(m.editors[index].Buffer.FilePath)
		if editorErr == nil && editorPath == path {
			return true
		}
	}
	return false
}

func (m Model) normalizeEditorFilePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path is empty")
	}
	if !filepath.IsAbs(path) && m.rootDir != "" {
		path = filepath.Join(m.rootDir, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve file path: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func (m Model) normalizeSaveAsDestination(path string) (string, error) {
	absolute, err := m.normalizeEditorFilePath(path)
	if err != nil {
		return "", fmt.Errorf("resolve Save As destination: %w", err)
	}
	return absolute, nil
}

func (m Model) showSaveAsDestinationConfirmation(requestID int, path string) Model {
	name := filepath.Base(path)
	m.saveAsDestinationPromptID = requestID
	m.unsavedConfirm = overlay.NewConfirm(
		"Replace Existing File?",
		fmt.Sprintf("%q already exists.", name),
		[]string{
			"Overwrite Existing replaces the exact version Teak just inspected.",
			"If it changes again before the write, Teak will ask again.",
			"Cancel keeps both the destination and local buffer unchanged.",
		},
		[]overlay.Button{
			{
				Label: "Overwrite Existing",
				Action: saveAsDestinationResolutionMsg{
					RequestID: requestID,
					Path:      path,
					Overwrite: true,
				},
			},
			{
				Label: "Cancel",
				Action: saveAsDestinationResolutionMsg{
					RequestID: requestID,
					Path:      path,
				},
			},
		},
		m.theme,
	)
	return m
}

func (m *Model) cancelSaveAsDestinationPrompt(requestID int) {
	if requestID == 0 || m.saveAsDestinationPromptID != requestID {
		return
	}
	req, ok := m.pendingSaves[requestID]
	if ok {
		delete(m.pendingSaves, requestID)
		if req.QuitAfter {
			m.cancelQuitAfterSaves()
		}
		m.status = fmt.Sprintf("Save As cancelled; kept existing %s", filepath.Base(req.Path))
	}
	m.saveAsDestinationPromptID = 0
}

func (m Model) handleSaveAsDestinationResolution(msg saveAsDestinationResolutionMsg) (tea.Model, tea.Cmd) {
	req, ok := m.pendingSaves[msg.RequestID]
	path := cleanExternalConflictPath(msg.Path)
	if !ok || !req.SaveAs || cleanExternalConflictPath(req.Path) != path {
		m.unsavedConfirm = nil
		if m.saveAsDestinationPromptID == msg.RequestID {
			m.saveAsDestinationPromptID = 0
		}
		return m, nil
	}
	m.unsavedConfirm = nil
	m.saveAsDestinationPromptID = 0
	if !msg.Overwrite {
		delete(m.pendingSaves, msg.RequestID)
		if req.QuitAfter {
			m.cancelQuitAfterSaves()
		}
		m.status = fmt.Sprintf("Save As cancelled; kept existing %s", filepath.Base(path))
		return m, nil
	}
	if req.DiskExpectation != saveDiskExact || req.ExpectedDiskSnapshot == nil {
		delete(m.pendingSaves, msg.RequestID)
		if req.QuitAfter {
			m.cancelQuitAfterSaves()
		}
		m.status = fmt.Sprintf("Save As blocked: could not safely verify %s", filepath.Base(path))
		return m, nil
	}
	m.status = fmt.Sprintf("Verifying existing destination before Save As: %s", filepath.Base(path))
	return m, m.startSaveRequest(msg.RequestID)
}

// reconcileSaveAs applies the UI and integration changes that belong to a
// successful Save As. It must only run after the snapshot writer has reported
// success; before then the buffer continues to represent its original path.
func (m *Model) reconcileSaveAs(tabIndex int, oldPath, newPath string) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) {
		return nil
	}

	ed := &m.editors[tabIndex]
	ed.Config.CommentPrefix = editor.CommentPrefixForFile(newPath)
	if tabIndex < len(m.tabBar.Tabs) {
		m.tabBar.Tabs[tabIndex].Label = filepath.Base(newPath)
		m.tabBar.Tabs[tabIndex].FilePath = newPath
		m.tabBar.Tabs[tabIndex].Dirty = ed.Buffer.Dirty()
		m.tabBar.PinTab(tabIndex)
	}

	if filepath.Ext(oldPath) != filepath.Ext(newPath) || ed.Highlighter == nil {
		replacement := editor.New(ed.Buffer, m.theme, ed.Config)
		replacement.SetSize(ed.Viewport.Width, ed.Viewport.Height)
		replacement.HasLSP = ed.HasLSP
		m.setEditor(tabIndex, replacement)
	}

	if m.watcher != nil {
		if oldPath != "" && oldPath != newPath {
			m.watcher.UnwatchFile(oldPath)
		}
		m.watcher.WatchFile(newPath)
	}
	if oldPath != "" && oldPath != newPath {
		m.clearClosedExternalPathState(oldPath)
	}

	var cmds []tea.Cmd
	if m.lspMgr != nil {
		if oldPath != "" && oldPath != newPath {
			m.lspMgr.CloseDocument(oldPath)
		}
		uri := lsp.FileURI(newPath)
		if client := m.lspMgr.ClientForFile(newPath); client == nil {
			cmds = append(cmds, m.lspDidOpen(m.editors[tabIndex].Buffer))
		} else if _, isOpen := client.DocumentVersion(uri); !isOpen {
			cmds = append(cmds, m.lspDidOpen(m.editors[tabIndex].Buffer))
		}
	}

	if oldPath == "" {
		cmds = append(cmds, m.triggerPluginEvents(
			m.pluginEvent(plugin.EventBufNew, newPath),
			m.pluginEvent(plugin.EventFileType, newPath),
			m.pluginEvent(plugin.EventBufEnter, newPath),
		))
	}
	m.status = fmt.Sprintf("Saved as %s", newPath)
	return tea.Batch(cmds...)
}
