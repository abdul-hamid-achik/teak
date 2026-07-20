package app

import (
	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/filetree"
	"teak/internal/lsp"
)

// treeLoadedMsg transfers the initial directory listing to the model after
// startup. NewModel intentionally creates only an empty, render-safe tree.
type treeLoadedMsg struct {
	Tree       filetree.Model
	Generation uint64
}

// treeRefreshDebounceMsg and treeRefreshResultMsg keep filesystem rescans out
// of Bubble Tea's Update loop. Generation makes a late read from a burst of
// watcher events harmless.
type treeRefreshDebounceMsg struct {
	Generation uint64
}

type treeRefreshResultMsg struct {
	Generation uint64
	Refresh    filetree.RefreshResult
	Err        error
}

// settingsSaveResultMsg returns Settings persistence to the root model. Values
// are applied only after the write succeeds.
type settingsSaveResultMsg struct {
	Config config.Config
	Err    error
}

// settingsDiscardMsg is emitted only after the user confirms that edits made
// in the Settings overlay should be abandoned.
type settingsDiscardMsg struct{}

// settingsKeepEditingMsg keeps Settings open after its discard confirmation.
type settingsKeepEditingMsg struct{}

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

// lspCodeActionPickerMsg preserves the request identity until the user makes a
// selection. The document is revalidated at selection time so a delayed picker
// cannot apply edits computed for an older buffer version.
type lspCodeActionPickerMsg struct {
	Action   lsp.CodeAction
	Metadata lsp.DocumentRequestMetadata
}

// lspCodeActionCommandResultMsg returns a user-selected server command to the
// Bubble Tea loop. Generation plus document metadata make late responses
// harmless after another action, edit, or tab change supersedes it.
type lspCodeActionCommandResultMsg struct {
	Generation uint64
	Metadata   lsp.DocumentRequestMetadata
	Title      string
	Err        error
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
	Generation uint64
	Frames     []dap.StackFrame
	Variables  []dap.Variable
}

// debugStartResultMsg and debugStopResultMsg return blocking DAP lifecycle
// operations to the Bubble Tea event loop.
type debugStartResultMsg struct {
	Generation uint64
	Err        error
}

type debugStopResultMsg struct {
	Generation uint64
	Status     string
}

// debugActionResultMsg returns a Continue/Step request to the event loop.
type debugActionResultMsg struct {
	Generation uint64
	Action     debugAction
	Err        error
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
	Files      []string
	Generation int
	Err        error
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
