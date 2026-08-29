package app

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
)

type overlayRequestKind uint8

const (
	overlayRequestCompletion overlayRequestKind = iota
	overlayRequestHover
	overlayRequestSignature
	overlayRequestKindCount
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
	generations [overlayRequestKindCount]uint64
	cancels     [overlayRequestKindCount]context.CancelFunc
}

func (tracker *overlayRequestTracker) next(kind overlayRequestKind) uint64 {
	if kind >= overlayRequestKindCount {
		return 0
	}
	tracker.generations[kind]++
	return tracker.generations[kind]
}

func (tracker overlayRequestTracker) current(kind overlayRequestKind) uint64 {
	if kind >= overlayRequestKindCount {
		return 0
	}
	return tracker.generations[kind]
}

func (tracker *overlayRequestTracker) start(kind overlayRequestKind) context.Context {
	if kind >= overlayRequestKindCount {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	if tracker.cancels[kind] != nil {
		tracker.cancels[kind]()
	}
	ctx, cancel := context.WithCancel(context.Background())
	tracker.cancels[kind] = cancel
	return ctx
}

func (tracker *overlayRequestTracker) invalidate(kind overlayRequestKind) {
	if kind >= overlayRequestKindCount {
		return
	}
	if tracker.cancels[kind] != nil {
		tracker.cancels[kind]()
		tracker.cancels[kind] = nil
		tracker.generations[kind]++
	}
}

func (tracker *overlayRequestTracker) invalidateAll() {
	for kind := overlayRequestKind(0); kind < overlayRequestKindCount; kind++ {
		tracker.invalidate(kind)
	}
}

func (tracker *overlayRequestTracker) cancelAll() {
	for kind := overlayRequestKind(0); kind < overlayRequestKindCount; kind++ {
		if tracker.cancels[kind] != nil {
			tracker.cancels[kind]()
			tracker.cancels[kind] = nil
		}
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
		CursorLine: editor.Buffer.Cursor.Line,
		CursorCol:  editor.Buffer.Cursor.Col,
		Generation: m.overlayRequests.next(kind),
	}, true
}

func (m *Model) beginOverlayRequestContext(kind overlayRequestKind) (lsp.OverlayRequestMetadata, context.Context, bool) {
	metadata, ok := m.beginOverlayRequest(kind)
	if !ok {
		return lsp.OverlayRequestMetadata{}, nil, false
	}
	return metadata, m.overlayRequests.start(kind), true
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
		editor.Buffer.Cursor.Line == metadata.CursorLine &&
		editor.Buffer.Cursor.Col == metadata.CursorCol &&
		m.overlayRequests.current(kind) == metadata.Generation
}

// lspRequestRoutineErr reports request outcomes that are routine degradation
// rather than failures the status bar should surface: supersession
// cancellation, and a server that exited mid-request (its restart is already
// reported separately). For requests that fire automatically — while typing
// or on file open — the per-method timeout budget is routine too: a slow
// cold-start server would otherwise print a deadline error on every keystroke.
func lspRequestRoutineErr(err error, auto bool) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, lsp.ErrClientNotRunning) {
		return true
	}
	return auto && errors.Is(err, context.DeadlineExceeded)
}

func (m Model) requestCompletion() (tea.Model, tea.Cmd) {
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return m, nil
	}
	metadata, requestContext, ok := m.beginOverlayRequestContext(overlayRequestCompletion)
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
		items, err := client.CompletionContext(requestContext, lsp.FileURI(filePath), line, column)
		if lspRequestRoutineErr(err, false) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/completion", Message: err.Error()}
		}
		if len(items) == 0 {
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
	metadata, requestContext, ok := m.beginOverlayRequestContext(overlayRequestHover)
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
		result, err := client.HoverContext(requestContext, lsp.FileURI(filePath), line, column)
		if lspRequestRoutineErr(err, false) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/hover", Message: err.Error()}
		}
		if result == nil {
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
	metadata, requestContext, ok := m.beginOverlayRequestContext(overlayRequestSignature)
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
		help, err := client.SignatureHelpContext(requestContext, lsp.FileURI(filePath), line, column)
		if lspRequestRoutineErr(err, true) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/signatureHelp", Message: err.Error()}
		}
		if help == nil {
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
