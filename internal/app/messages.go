package app

import (
	"teak/internal/dap"
	"teak/internal/lsp"
)

// ============================================================================
// LSP Messages
// ============================================================================

// lspMsg wraps LSP messages from the manager.
type lspMsg struct {
	msg any
}

// lspLocationPickerMsg is the Value payload for an LSP location picker item.
type lspLocationPickerMsg struct {
	Location lsp.Location
}

// lspSymbolPickerMsg is the Value payload for an LSP document symbol picker item.
type lspSymbolPickerMsg struct {
	Symbol lsp.DocumentSymbol
}

// ============================================================================
// DAP (Debug Adapter Protocol) Messages
// ============================================================================

// dapMsg wraps DAP messages from the manager.
type dapMsg struct {
	msg any
}

// debugStateMsg carries fetched debug state back to Update.
type debugStateMsg struct {
	Frames    []dap.StackFrame
	Variables []dap.Variable
}

// ============================================================================
// ACP (Agent Communication Protocol) Messages
// ============================================================================

// acpMsg wraps ACP messages from the manager.
type acpMsg struct {
	msg any
}

// toggleAgentMsg toggles the agent panel visibility.
type toggleAgentMsg struct{}

// focusAgentMsg focuses the agent panel.
type focusAgentMsg struct{}

// agentCancelMsg cancels the current agent operation.
type agentCancelMsg struct{}

// agentModelPickerSelectMsg is emitted when a model is selected in the agent picker.
type agentModelPickerSelectMsg struct {
	ModelId string
}

// agentFilePickerSelectMsg is emitted when a file is selected in the agent file picker.
type agentFilePickerSelectMsg struct {
	Path string
}

// agentWriteErrorMsg is emitted when the agent writes an error message.
type agentWriteErrorMsg struct {
	Path string
	Err  error
}

// ============================================================================
// Search Messages
// ============================================================================

// FileListMsg is emitted when the file list for quick open is ready.
type FileListMsg struct {
	Files []string
}

// ============================================================================
// Editor Messages
// ============================================================================

// ============================================================================
// Session Messages
// ============================================================================

// ============================================================================
// Command Palette Messages
// ============================================================================

// ============================================================================
// UI Messages
// ============================================================================

// ============================================================================
// File Watcher Messages
// FileChangedMsg and TreeChangedMsg are defined in watcher.go with different fields
// ============================================================================

// ============================================================================
// Editor Trigger Messages
// ============================================================================

// RequestCompletionCmd triggers completion from the app layer.
type RequestCompletionCmd struct{}

// RetokenizeMsg triggers syntax re-tokenization after edits.
type RetokenizeMsg struct {
	Version      int
	ViewportOnly bool
}

// TokenizeCompleteMsg carries the result of async tokenization.
type TokenizeCompleteMsg struct {
	Version int
	Lines   [][]any // StyledToken slices
	Partial bool
}

// BreakpointClickMsg is emitted when the user clicks the line number gutter.
type BreakpointClickMsg struct{ Line int }

// JumpToFrameMsg is emitted when the user clicks a stack frame.
type JumpToFrameMsg struct {
	FilePath string
	Line     int // 0-based
}

