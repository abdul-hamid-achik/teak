package app

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
)

// Memory limits for LSP coordinator
const (
	maxLSPDiagnosticsFiles = 100
)

// LSPCoordinator manages LSP client lifecycle and message routing.
type LSPCoordinator struct {
	mu           sync.RWMutex
	mgr          *lsp.Manager
	diagnostics  map[string][]lsp.Diagnostic // file path → diagnostics
	triggerChars map[string][]string         // file path → trigger characters
}

// NewLSPCoordinator creates a new LSP coordinator.
func NewLSPCoordinator(mgr *lsp.Manager) *LSPCoordinator {
	return &LSPCoordinator{
		mgr:          mgr,
		diagnostics:  make(map[string][]lsp.Diagnostic),
		triggerChars: make(map[string][]string),
	}
}

// HandleMessage routes LSP messages to appropriate handlers.
func (c *LSPCoordinator) HandleMessage(msg tea.Msg) []tea.Cmd {
	switch m := msg.(type) {
	case lsp.DiagnosticsMsg:
		return c.handleDiagnostics(m)
	case lsp.CompletionResultMsg:
		return c.handleCompletion(m)
	case lsp.HoverResultMsg:
		return c.handleHover(m)
	case lsp.DefinitionResultMsg:
		return c.handleDefinition(m)
	case lsp.ReferencesResultMsg:
		return c.handleReferences(m)
	case lsp.FormatResultMsg:
		return c.handleFormat(m)
	case lsp.CodeActionResultMsg:
		return c.handleCodeAction(m)
	case lsp.DocumentSymbolResultMsg:
		return c.handleDocumentSymbol(m)
	case lsp.RenameResultMsg:
		return c.handleRename(m)
	case lsp.FoldingRangeResultMsg:
		return c.handleFoldingRange(m)
	case lsp.SignatureHelpResultMsg:
		return c.handleSignatureHelp(m)
	case lsp.LspErrorMsg:
		return c.handleError(m)
	case lsp.LspProgressMsg:
		return c.handleProgress(m)
	case lsp.LspShowMessageMsg:
		return c.handleShowMessage(m)
	case LspReadyMsg:
		return c.handleLspReady(m)
	default:
		return nil
	}
}

// handleDiagnostics stores diagnostics for a file.
func (c *LSPCoordinator) handleDiagnostics(msg lsp.DiagnosticsMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := lsp.URIToPath(msg.URI)
	c.diagnostics[path] = msg.Diagnostics
	
	// Clean old entries if too many files
	if len(c.diagnostics) > maxLSPDiagnosticsFiles {
		// Remove first entry (oldest)
		for oldPath := range c.diagnostics {
			delete(c.diagnostics, oldPath)
			break
		}
	}
	return nil
}

// handleCompletion returns completion items.
func (c *LSPCoordinator) handleCompletion(msg lsp.CompletionResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleHover returns hover content.
func (c *LSPCoordinator) handleHover(msg lsp.HoverResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleDefinition returns definition locations.
func (c *LSPCoordinator) handleDefinition(msg lsp.DefinitionResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleReferences returns reference locations.
func (c *LSPCoordinator) handleReferences(msg lsp.ReferencesResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleFormat returns format edits.
func (c *LSPCoordinator) handleFormat(msg lsp.FormatResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleCodeAction returns code actions.
func (c *LSPCoordinator) handleCodeAction(msg lsp.CodeActionResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleDocumentSymbol returns document symbols.
func (c *LSPCoordinator) handleDocumentSymbol(msg lsp.DocumentSymbolResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleRename returns rename edits.
func (c *LSPCoordinator) handleRename(msg lsp.RenameResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleFoldingRange returns folding ranges.
func (c *LSPCoordinator) handleFoldingRange(msg lsp.FoldingRangeResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleSignatureHelp returns signature help.
func (c *LSPCoordinator) handleSignatureHelp(msg lsp.SignatureHelpResultMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleError returns an error message for the status bar.
func (c *LSPCoordinator) handleError(msg lsp.LspErrorMsg) []tea.Cmd {
	statusMsg := msg.Message
	if msg.Method != "" {
		statusMsg = msg.Method + ": " + msg.Message
	}
	return []tea.Cmd{func() tea.Msg {
		return statusMsg
	}}
}

// handleProgress returns a progress message.
func (c *LSPCoordinator) handleProgress(msg lsp.LspProgressMsg) []tea.Cmd {
	// Progress messages have Token and Value (any type)
	// Just acknowledge the progress
	return nil
}

// handleShowMessage returns a show message notification.
func (c *LSPCoordinator) handleShowMessage(msg lsp.LspShowMessageMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg {
		return msg.Message
	}}
}

// handleLspReady handles LSP initialization completion.
func (c *LSPCoordinator) handleLspReady(msg LspReadyMsg) []tea.Cmd {
	if c.mgr == nil {
		return nil
	}

	if client := c.mgr.ClientForFile(msg.FilePath); client != nil {
		chars := client.GetCompletionTriggerCharacters()
		if len(chars) > 0 {
			c.mu.Lock()
			c.triggerChars[msg.FilePath] = chars
			c.mu.Unlock()
		}
	}

	return nil
}

// GetDiagnostics returns diagnostics for a file.
func (c *LSPCoordinator) GetDiagnostics(path string) []lsp.Diagnostic {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.diagnostics[path]
}

// SetTriggerChars sets trigger characters for a file.
func (c *LSPCoordinator) SetTriggerChars(path string, chars []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.triggerChars[path] = chars
}

// GetTriggerChars returns trigger characters for a file.
func (c *LSPCoordinator) GetTriggerChars(path string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.triggerChars[path]
}

// ClearDiagnostics clears diagnostics for a file.
func (c *LSPCoordinator) ClearDiagnostics(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.diagnostics, path)
}

// AggregateDiagnostics returns all diagnostics from all files.
func (c *LSPCoordinator) AggregateDiagnostics() []lsp.Diagnostic {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	all := make([]lsp.Diagnostic, 0, len(c.diagnostics))
	for _, diags := range c.diagnostics {
		all = append(all, diags...)
	}
	return all
}

// Shutdown shuts down all LSP clients.
func (c *LSPCoordinator) Shutdown() {
	if c.mgr != nil {
		c.mgr.ShutdownAll()
	}
}
