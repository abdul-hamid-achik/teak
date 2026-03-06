package lsp

// LSP Error Codes (per JSON-RPC and LSP specifications)
const (
	// JSON-RPC 2.0 standard error codes
	ParseError         = -32700
	InvalidRequest     = -32600
	MethodNotFound     = -32601
	InvalidParams      = -32602
	InternalError      = -32603
	
	// LSP reserved error codes
	ServerNotInitialized = -32002
	UnknownErrorCode     = -32001
	
	// LSP request cancellation codes
	RequestCancelled = -32800
	ContentModified  = -32801
	ServerCancelled  = -32802
	RequestFailed    = -32803
)

// DiagSeverity represents the severity of a diagnostic.
type DiagSeverity int

const (
	SeverityError   DiagSeverity = 1
	SeverityWarning DiagSeverity = 2
	SeverityInfo    DiagSeverity = 3
	SeverityHint    DiagSeverity = 4
)

// DiagPosition is a 0-based line/character position from LSP.
type DiagPosition struct {
	Line      int
	Character int
}

// DiagRange represents a range within a document.
type DiagRange struct {
	Start DiagPosition
	End   DiagPosition
}

// Diagnostic represents a diagnostic from the language server.
type Diagnostic struct {
	Range    DiagRange
	Severity DiagSeverity
	Message  string
	Source   string
}

// CompletionItem represents a completion suggestion.
type CompletionItem struct {
	Label      string
	Detail     string
	InsertText string
	Kind       int
}

// HoverResult represents hover information.
type HoverResult struct {
	Content string
}

// Location represents a source code location.
type Location struct {
	URI       string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// ServerCapabilities represents the capabilities of an LSP server.
type ServerCapabilities struct {
	TextDocumentSync       any    `json:"textDocumentSync,omitempty"` // int or TextDocumentSyncOptions
	CompletionProvider     *struct {
		ResolveProvider   bool     `json:"resolveProvider,omitempty"`
		TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	} `json:"completionProvider,omitempty"`
	HoverProvider         bool `json:"hoverProvider,omitempty"`
	DefinitionProvider    bool `json:"definitionProvider,omitempty"`
	ReferencesProvider    bool `json:"referencesProvider,omitempty"`
	RenameProvider        bool `json:"renameProvider,omitempty"`
	DocumentSymbolProvider bool `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider    any  `json:"codeActionProvider,omitempty"` // bool or CodeActionOptions
	FormattingProvider      bool `json:"documentFormattingProvider,omitempty"`
	RangeFormattingProvider bool `json:"documentRangeFormattingProvider,omitempty"`
	FoldingRangeProvider    bool `json:"foldingRangeProvider,omitempty"`
	SignatureHelpProvider *struct {
		TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	} `json:"signatureHelpProvider,omitempty"`
}

// InitializeResult represents the response from the initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	} `json:"serverInfo,omitempty"`
}

// DiagnosticsMsg is sent when new diagnostics arrive from the LSP server.
type DiagnosticsMsg struct {
	URI         string
	Diagnostics []Diagnostic
}

// CompletionResultMsg is sent when completion results arrive.
type CompletionResultMsg struct {
	Items []CompletionItem
}

// HoverResultMsg is sent when hover information arrives.
type HoverResultMsg struct {
	Content string
}

// DefinitionResultMsg is sent when go-to-definition results arrive.
type DefinitionResultMsg struct {
	Locations []Location
}

// ReferencesResultMsg is sent when find-references results arrive.
type ReferencesResultMsg struct {
	Locations []Location
}

// TextEdit represents a text edit from an LSP workspace edit.
type TextEdit struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	NewText   string
}

// RenameResultMsg is sent when a rename result arrives.
type RenameResultMsg struct {
	Edits map[string][]TextEdit // uri -> []TextEdit
}

// LspErrorMsg is sent when an LSP request returns an error.
type LspErrorMsg struct {
	Method  string
	Code    int
	Message string
}

// SignatureHelp represents signature help information.
type SignatureHelp struct {
	Signatures []SignatureInformation `json:"signatures"`
	ActiveSignature int `json:"activeSignature,omitempty"`
	ActiveParameter int `json:"activeParameter,omitempty"`
}

// SignatureInformation represents a function signature.
type SignatureInformation struct {
	Label         string         `json:"label"`
	Documentation string         `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// ParameterInformation represents a parameter in a signature.
type ParameterInformation struct {
	Label       any    `json:"label"` // string or [start, end]
	Documentation string `json:"documentation,omitempty"`
}

// SignatureHelpResultMsg is sent when signature help arrives.
type SignatureHelpResultMsg struct {
	Help *SignatureHelp
}

// FoldingRange represents a folding range from the server.
type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter int    `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   int    `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"` // "comment", "imports", "region"
}

// FoldingRangeResultMsg is sent when folding ranges arrive.
type FoldingRangeResultMsg struct {
	FilePath string
	Ranges   []FoldingRange
}

// FormatResultMsg is sent when formatting result arrives.
type FormatResultMsg struct {
	Edits []TextEdit
}

// CodeActionResultMsg is sent when code actions arrive.
type CodeActionResultMsg struct {
	Actions []CodeAction
}

// CodeAction represents a code action from the server.
type CodeAction struct {
	Title       string `json:"title"`
	Kind        string `json:"kind,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *struct {
		Title     string `json:"title"`
		Command   string `json:"command"`
		Arguments []any  `json:"arguments,omitempty"`
	} `json:"command,omitempty"`
}

// WorkspaceEdit represents a workspace edit.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes,omitempty"`
}

// DocumentSymbolResultMsg is sent when document symbols arrive.
type DocumentSymbolResultMsg struct {
	Symbols []DocumentSymbol
}

// DocumentSymbol represents a symbol in a document.
type DocumentSymbol struct {
	Name           string             `json:"name"`
	Detail         string             `json:"detail,omitempty"`
	Kind           int                `json:"kind"`
	Range          Range              `json:"range"`
	SelectionRange Range              `json:"selectionRange"`
	Children       []DocumentSymbol   `json:"children,omitempty"`
}

// Range represents a range in a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position represents a position in a document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// LspShowMessageMsg is sent when the server wants to show a message.
type LspShowMessageMsg struct {
	Type    int    // 1=Error, 2=Warning, 3=Info, 4=Log
	Message string
}

// LspProgressMsg is sent for progress notifications.
type LspProgressMsg struct {
	Token any
	Value any
}
