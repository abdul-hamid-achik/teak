package app

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
)

type overlayRequestKind uint8

const (
	overlayRequestCompletion overlayRequestKind = iota
	overlayRequestHover
	overlayRequestSignature
)

func (kind overlayRequestKind) String() string {
	switch kind {
	case overlayRequestCompletion:
		return "completion"
	case overlayRequestHover:
		return "hover"
	case overlayRequestSignature:
		return "signature"
	default:
		return "unknown"
	}
}

type overlayRequestTracker struct {
	completion uint64
	hover      uint64
	signature  uint64
}

func (tracker *overlayRequestTracker) next(kind overlayRequestKind) uint64 {
	switch kind {
	case overlayRequestCompletion:
		tracker.completion++
		return tracker.completion
	case overlayRequestHover:
		tracker.hover++
		return tracker.hover
	case overlayRequestSignature:
		tracker.signature++
		return tracker.signature
	default:
		return 0
	}
}

func (tracker overlayRequestTracker) current(kind overlayRequestKind) uint64 {
	switch kind {
	case overlayRequestCompletion:
		return tracker.completion
	case overlayRequestHover:
		return tracker.hover
	case overlayRequestSignature:
		return tracker.signature
	default:
		return 0
	}
}

func (m *Model) beginOverlayRequest(kind overlayRequestKind) (lsp.OverlayRequestMetadata, bool) {
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return lsp.OverlayRequestMetadata{}, false
	}
	return lsp.OverlayRequestMetadata{
		FilePath:   editor.Buffer.FilePath,
		Version:    editor.Buffer.Version(),
		Generation: m.overlayRequests.next(kind),
	}, true
}

func (m Model) acceptsOverlayResult(kind overlayRequestKind, metadata lsp.OverlayRequestMetadata) bool {
	if metadata == (lsp.OverlayRequestMetadata{}) {
		return true
	}
	if metadata.FilePath == "" || metadata.Generation == 0 {
		return false
	}

	editor := m.activeEditor()
	if editor == nil {
		return false
	}
	return editor.Buffer.FilePath == metadata.FilePath &&
		editor.Buffer.Version() == metadata.Version &&
		m.overlayRequests.current(kind) == metadata.Generation
}

func (m Model) requestCompletion() (tea.Model, tea.Cmd) {
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return m, nil
	}
	metadata, ok := m.beginOverlayRequest(overlayRequestCompletion)
	if !ok {
		return m, nil
	}
	manager := m.lspMgr
	filePath := editor.Buffer.FilePath
	line := editor.Buffer.Cursor.Line
	column := editor.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := manager.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		items, err := client.Completion(lsp.FileURI(filePath), line, column)
		if err != nil || len(items) == 0 {
			return nil
		}
		return lsp.CompletionResultMsg{
			OverlayRequestMetadata: metadata,
			Items:                  items,
		}
	}
}

func (m Model) requestHover() (tea.Model, tea.Cmd) {
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return m, nil
	}
	metadata, ok := m.beginOverlayRequest(overlayRequestHover)
	if !ok {
		return m, nil
	}
	manager := m.lspMgr
	filePath := editor.Buffer.FilePath
	line := editor.Buffer.Cursor.Line
	column := editor.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := manager.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		result, err := client.Hover(lsp.FileURI(filePath), line, column)
		if err != nil || result == nil {
			return nil
		}
		return lsp.HoverResultMsg{
			OverlayRequestMetadata: metadata,
			Content:                result.Content,
		}
	}
}

func (m Model) requestSignatureHelp() (Model, tea.Cmd) {
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return m, nil
	}
	metadata, ok := m.beginOverlayRequest(overlayRequestSignature)
	if !ok {
		return m, nil
	}
	manager := m.lspMgr
	filePath := editor.Buffer.FilePath
	line := editor.Buffer.Cursor.Line
	column := editor.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := manager.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		help, err := client.SignatureHelp(lsp.FileURI(filePath), line, column)
		if err != nil || help == nil {
			return nil
		}
		return lsp.SignatureHelpResultMsg{
			OverlayRequestMetadata: metadata,
			Help:                   help,
		}
	}
}

func signatureHelpTrigger(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyPressMsg)
	return ok && (key.Text == "(" || key.Text == ",")
}

type hoverTriggerMsg struct{}
