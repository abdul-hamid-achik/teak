package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
)

type pendingSaveRequest struct {
	TabIndex         int
	Path             string
	CloseAfter       bool
	QuitAfter        bool
	StatusNote       string
	FormattingTried  bool
}

func formattingOptions(cfg editor.Config) lsp.FormattingOptions {
	return lsp.FormattingOptions{
		TabSize:      cfg.TabSize,
		InsertSpaces: !cfg.InsertTabs,
	}
}

func (m *Model) nextSaveID() int {
	requestID := m.nextSaveRequestID
	m.nextSaveRequestID++
	return requestID
}

func (m *Model) beginSaveForTab(tabIndex int, closeAfter, quitAfter bool) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) {
		return nil
	}

	path := m.editors[tabIndex].Buffer.FilePath
	if path == "" {
		return nil
	}

	requestID := m.nextSaveID()
	m.pendingSaves[requestID] = pendingSaveRequest{
		TabIndex:   tabIndex,
		Path:       path,
		CloseAfter: closeAfter,
		QuitAfter:  quitAfter,
	}
	return m.startSaveRequest(requestID)
}

func (m *Model) startSaveRequest(requestID int) tea.Cmd {
	req, ok := m.pendingSaves[requestID]
	if !ok {
		return nil
	}

	tabIndex := req.TabIndex
	if tabIndex < 0 || tabIndex >= len(m.editors) || m.editors[tabIndex].Buffer.FilePath != req.Path {
		tabIndex = m.findEditorByPath(req.Path)
		if tabIndex < 0 {
			delete(m.pendingSaves, requestID)
			return nil
		}
		req.TabIndex = tabIndex
		m.pendingSaves[requestID] = req
	}

	ed := m.editors[tabIndex]
	if m.appCfg.Editor.FormatOnSave && !req.FormattingTried {
		req.FormattingTried = true
		m.pendingSaves[requestID] = req
		return m.requestFormatting(req.Path, ed.Config, requestID)
	}

	return SaveFileCmd(ed.Buffer.Save, req.Path, requestID)
}

func (m *Model) completeSaveRequest(requestID int) (pendingSaveRequest, bool) {
	req, ok := m.pendingSaves[requestID]
	if ok {
		delete(m.pendingSaves, requestID)
	}
	return req, ok
}

func (m Model) hasPendingQuitAfterSaves() bool {
	for _, req := range m.pendingSaves {
		if req.QuitAfter {
			return true
		}
	}
	return false
}

func (m *Model) cancelQuitAfterSaves() {
	for requestID, req := range m.pendingSaves {
		if req.QuitAfter {
			req.QuitAfter = false
			m.pendingSaves[requestID] = req
		}
	}
}

func (m *Model) setPendingSaveNote(requestID int, note string) {
	req, ok := m.pendingSaves[requestID]
	if !ok {
		return
	}
	req.StatusNote = note
	req.FormattingTried = true
	m.pendingSaves[requestID] = req
}

func saveSuccessStatus(path, note string) string {
	status := fmt.Sprintf("Saved %s", path)
	if note == "" {
		return status
	}
	return fmt.Sprintf("%s (%s)", status, note)
}

func formatResultNote(status lsp.FormatStatus, err error) string {
	switch status {
	case lsp.FormatNoOp:
		return "no formatting changes"
	case lsp.FormatUnsupported:
		return "formatting not supported"
	case lsp.FormatError:
		if err != nil {
			return fmt.Sprintf("formatting failed: %v", err)
		}
		return "formatting failed"
	default:
		return ""
	}
}

func (m Model) ensureFormattingDocumentState(filePath string, client *lsp.Client) {
	idx := m.findEditorByPath(filePath)
	if idx < 0 {
		return
	}

	buf := m.editors[idx].Buffer
	uri := lsp.FileURI(filePath)
	version := buf.Version()
	content := buf.Content()

	if _, ok := client.DocumentVersion(uri); !ok {
		langID := ""
		if serverCfg := m.lspMgr.ConfigForFile(filePath); serverCfg != nil {
			langID = serverCfg.LanguageID
		}
		client.DidOpen(uri, langID, version, content)
		return
	}

	if syncedVersion, _ := client.DocumentVersion(uri); syncedVersion != version {
		client.DidChange(uri, version, content)
	}
}

func (m Model) requestFormatting(filePath string, cfg editor.Config, requestID int) tea.Cmd {
	if filePath == "" {
		return nil
	}

	mgr := m.lspMgr
	options := formattingOptions(cfg)
	return func() tea.Msg {
		client, err := mgr.EnsureClient(filePath)
		if err != nil {
			return lsp.FormatResultMsg{
				RequestID: requestID,
				FilePath:  filePath,
				Status:    lsp.FormatError,
				Err:       err,
			}
		}
		if client == nil {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, Status: lsp.FormatUnsupported}
		}
		if !client.SupportsFormatting() {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, Status: lsp.FormatUnsupported}
		}

		m.ensureFormattingDocumentState(filePath, client)

		edits, err := client.Formatting(lsp.FileURI(filePath), options)
		if err != nil {
			return lsp.FormatResultMsg{
				RequestID: requestID,
				FilePath:  filePath,
				Status:    lsp.FormatError,
				Err:       err,
			}
		}
		if len(edits) == 0 {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, Status: lsp.FormatNoOp}
		}
		return lsp.FormatResultMsg{
			RequestID: requestID,
			FilePath:  filePath,
			Status:    lsp.FormatApplied,
			Edits:     edits,
		}
	}
}
