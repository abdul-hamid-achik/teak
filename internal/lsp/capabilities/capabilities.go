package capabilities

// SyncKind represents the document synchronization mode
type SyncKind int

const (
	SyncNone SyncKind = iota
	SyncFull
	SyncIncremental
)

// ServerCapabilities represents the capabilities of an LSP server
type ServerCapabilities struct {
	TextDocumentSync        any                   `json:"textDocumentSync,omitempty"`
	CompletionProvider      *CompletionOptions    `json:"completionProvider,omitempty"`
	PositionEncoding        string                `json:"positionEncoding,omitempty"`
	HoverProvider           any                   `json:"hoverProvider,omitempty"`
	DefinitionProvider      any                   `json:"definitionProvider,omitempty"`
	ReferencesProvider      any                   `json:"referencesProvider,omitempty"`
	RenameProvider          any                   `json:"renameProvider,omitempty"`
	DocumentSymbolProvider  any                   `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider      any                   `json:"codeActionProvider,omitempty"`
	FormattingProvider      any                   `json:"documentFormattingProvider,omitempty"`
	RangeFormattingProvider any                   `json:"documentRangeFormattingProvider,omitempty"`
	FoldingRangeProvider    any                   `json:"foldingRangeProvider,omitempty"`
	SignatureHelpProvider   *SignatureHelpOptions `json:"signatureHelpProvider,omitempty"`
}

// CompletionOptions represents completion provider options
type CompletionOptions struct {
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// SignatureHelpOptions represents signature help provider options
type SignatureHelpOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// Checker provides capability checking for an LSP client
type Checker struct {
	caps     ServerCapabilities
	syncKind SyncKind
}

// NewChecker creates a capability checker
func NewChecker(caps ServerCapabilities, syncKind SyncKind) *Checker {
	return &Checker{
		caps:     caps,
		syncKind: syncKind,
	}
}

// SupportsHover returns true if hover is supported
func (c *Checker) SupportsHover() bool {
	return capabilityEnabled(c.caps.HoverProvider)
}

// SupportsCompletion returns true if completion is supported
func (c *Checker) SupportsCompletion() bool {
	return c.caps.CompletionProvider != nil
}

// SupportsDefinition returns true if go-to-definition is supported
func (c *Checker) SupportsDefinition() bool {
	return capabilityEnabled(c.caps.DefinitionProvider)
}

// SupportsReferences returns true if find-references is supported
func (c *Checker) SupportsReferences() bool {
	return capabilityEnabled(c.caps.ReferencesProvider)
}

// SupportsRename returns true if rename is supported
func (c *Checker) SupportsRename() bool {
	return capabilityEnabled(c.caps.RenameProvider)
}

// SupportsFormatting returns true if formatting is supported
func (c *Checker) SupportsFormatting() bool {
	return capabilityEnabled(c.caps.FormattingProvider)
}

// SupportsRangeFormatting returns true if range formatting is supported
func (c *Checker) SupportsRangeFormatting() bool {
	return capabilityEnabled(c.caps.RangeFormattingProvider)
}

// SupportsFoldingRange returns true if folding range is supported
func (c *Checker) SupportsFoldingRange() bool {
	return capabilityEnabled(c.caps.FoldingRangeProvider)
}

// SupportsCodeAction returns true if code action is supported
func (c *Checker) SupportsCodeAction() bool {
	return capabilityEnabled(c.caps.CodeActionProvider)
}

// SupportsDocumentSymbol returns true if document symbol is supported
func (c *Checker) SupportsDocumentSymbol() bool {
	return capabilityEnabled(c.caps.DocumentSymbolProvider)
}

// SupportsSignatureHelp returns true if signature help is supported
func (c *Checker) SupportsSignatureHelp() bool {
	return c.caps.SignatureHelpProvider != nil
}

// GetCompletionTriggerCharacters returns trigger characters for completion
func (c *Checker) GetCompletionTriggerCharacters() []string {
	if c.caps.CompletionProvider != nil {
		return c.caps.CompletionProvider.TriggerCharacters
	}
	return nil
}

// GetSignatureHelpTriggerCharacters returns trigger characters for signature help
func (c *Checker) GetSignatureHelpTriggerCharacters() []string {
	if c.caps.SignatureHelpProvider != nil {
		return c.caps.SignatureHelpProvider.TriggerCharacters
	}
	return nil
}

// GetSyncKind returns the negotiated document sync mode
func (c *Checker) GetSyncKind() SyncKind {
	return c.syncKind
}

func capabilityEnabled(v any) bool {
	switch vv := v.(type) {
	case nil:
		return false
	case bool:
		return vv
	case map[string]any:
		return len(vv) > 0
	default:
		return true
	}
}
