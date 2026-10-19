package app

import (
	"context"
	"errors"
	"time"

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

// completionSyncWait bounds how long a completion request waits for the
// client to stage the document version it was computed for. A newer edit
// cancels the wait through the request context long before this expires; the
// bound only matters for a version that never reaches the client. It is a
// variable so tests can exercise expiry without waiting a full second.
var completionSyncWait = time.Second

const completionSyncPoll = 2 * time.Millisecond

// awaitDocumentVersion holds a request until the client has staged version
// for uri. Bubble Tea runs the didChange command and the completion command
// concurrently, so on fast typing the request is ready a keystroke before the
// client's snapshot, and the client then rejects the cursor as outside the
// line. It reports false when the request is superseded: its context was
// cancelled (a newer edit, cursor move or request) or the client already
// holds a newer version. A document the client has not opened is not waited
// on, and an expired wait proceeds, so a version that is never synchronized
// still surfaces as an error instead of silently disabling completion.
func awaitDocumentVersion(ctx context.Context, client *lsp.Client, uri string, version int) bool {
	deadline := time.NewTimer(completionSyncWait)
	defer deadline.Stop()
	poll := time.NewTicker(completionSyncPoll)
	defer poll.Stop()
	for {
		if ctx.Err() != nil {
			return false
		}
		synced, open := client.DocumentVersion(uri)
		if !open || synced == version {
			return true
		}
		if synced > version {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return true
		case <-poll.C:
		}
	}
}

// overlayRequestSuperseded reports whether a failed request belongs to a
// document version or cursor that is no longer current. The client validates
// positions before it observes cancellation, so a superseded request can
// fail with a position error rather than context.Canceled; like a stale
// result, that error must not reach the status bar.
func overlayRequestSuperseded(ctx context.Context, client *lsp.Client, uri string, version int) bool {
	if ctx.Err() != nil {
		return true
	}
	synced, open := client.DocumentVersion(uri)
	return open && synced > version
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
		uri := lsp.FileURI(filePath)
		if !awaitDocumentVersion(requestContext, client, uri, metadata.Version) {
			return nil
		}
		items, err := client.CompletionContext(requestContext, uri, line, column)
		if lspRequestRoutineErr(err, false) {
			return nil
		}
		if err != nil && overlayRequestSuperseded(requestContext, client, uri, metadata.Version) {
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
