package lsp

import (
	"context"
	"encoding/json"
)

// LSP Error Codes (per JSON-RPC and LSP specifications)
const (
	// JSON-RPC 2.0 standard error codes
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

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

// DiagPosition is a 0-based internal Teak position. Character is always a
// UTF-8 byte offset once a server notification crosses the client boundary;
// the client converts the negotiated LSP encoding first.
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

// Location represents a source code location. Columns are normally UTF-8 byte
// offsets. For an unopened UTF-16/32 target, ProtocolEncoding preserves the
// server coordinates until the app has asynchronously loaded that document and
// can convert against its actual bytes; it is never serialized back to LSP.
type Location struct {
	URI              string
	StartLine        int
	StartCol         int
	EndLine          int
	EndCol           int
	ProtocolEncoding string `json:"-"`
}

// ServerCapabilities represents the capabilities of an LSP server.
type ServerCapabilities struct {
	TextDocumentSync   any `json:"textDocumentSync,omitempty"` // int or TextDocumentSyncOptions
	CompletionProvider *struct {
		ResolveProvider   bool     `json:"resolveProvider,omitempty"`
		TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	} `json:"completionProvider,omitempty"`
	PositionEncoding        string `json:"positionEncoding,omitempty"`                // utf-8 / utf-16 / utf-32
	HoverProvider           any    `json:"hoverProvider,omitempty"`                   // bool or options object
	DefinitionProvider      any    `json:"definitionProvider,omitempty"`              // bool or options object
	ReferencesProvider      any    `json:"referencesProvider,omitempty"`              // bool or options object
	RenameProvider          any    `json:"renameProvider,omitempty"`                  // bool or options object
	DocumentSymbolProvider  any    `json:"documentSymbolProvider,omitempty"`          // bool or options object
	CodeActionProvider      any    `json:"codeActionProvider,omitempty"`              // bool or CodeActionOptions
	FormattingProvider      any    `json:"documentFormattingProvider,omitempty"`      // bool or options object
	RangeFormattingProvider any    `json:"documentRangeFormattingProvider,omitempty"` // bool or options object
	FoldingRangeProvider    any    `json:"foldingRangeProvider,omitempty"`            // bool or options object
	SignatureHelpProvider   *struct {
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

// OverlayRequestMetadata identifies the active-editor state that originated an
// LSP overlay request. Zero values preserve compatibility with callers that do
// not provide request identity.
type OverlayRequestMetadata struct {
	FilePath   string
	Version    int
	Generation uint64
}

// DocumentRequestMetadata identifies the document snapshot that originated a
// non-overlay LSP request. Zero values preserve compatibility with legacy
// callers while app-generated requests always include full identity.
type DocumentRequestMetadata struct {
	FilePath   string
	Version    int
	Generation uint64
}

// CompletionResultMsg is sent when completion results arrive.
type CompletionResultMsg struct {
	OverlayRequestMetadata
	Items []CompletionItem
}

// HoverResultMsg is sent when hover information arrives.
type HoverResultMsg struct {
	OverlayRequestMetadata
	Content string
}

// DefinitionResultMsg is sent when go-to-definition results arrive.
type DefinitionResultMsg struct {
	DocumentRequestMetadata
	Locations []Location
}

// ReferencesResultMsg is sent when find-references results arrive.
type ReferencesResultMsg struct {
	DocumentRequestMetadata
	Locations []Location
}

// TextEdit represents an LSP workspace edit after protocol conversion. Its
// columns are always Teak UTF-8 byte offsets when delivered to the app.
type TextEdit struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	NewText   string
}

// FormattingOptions configures document formatting requests.
type FormattingOptions struct {
	TabSize      int
	InsertSpaces bool
}

type WorkspaceFileOperationKind string

const (
	FileOpCreate WorkspaceFileOperationKind = "create"
	FileOpRename WorkspaceFileOperationKind = "rename"
	FileOpDelete WorkspaceFileOperationKind = "delete"
)

type WorkspaceFileOperation struct {
	Kind   WorkspaceFileOperationKind
	URI    string
	OldURI string
	NewURI string
}

type WorkspaceDocumentChange struct {
	URI string
	// Version is the LSP document version the server used to compute Edits.
	// A nil value means the server did not provide one (as is allowed by the
	// protocol). Clients must reject a non-nil version that does not match an
	// open buffer before applying the edit.
	Version       *int
	Edits         []TextEdit
	FileOperation *WorkspaceFileOperation
}

// ApplyEditRequestMsg carries a server-initiated workspace/applyEdit request
// to the UI event loop. Respond must be called exactly once by the UI after it
// has accepted or rejected the edit; it never mutates editor state itself.
//
// The callback intentionally lives on the protocol message rather than in the
// model so the JSON-RPC reader never touches Bubble Tea state.
type ApplyEditRequestMsg struct {
	RequestID int
	Label     string
	Edit      WorkspaceEdit
	// Context is cancelled while the request is pending when the protocol
	// timeout, process shutdown, or an earlier response wins. Once Claim
	// succeeds, the UI owns the short commit phase and must send the final
	// response; the five-second timer is an admission deadline, not a deadline
	// that can revoke an already-started atomic mutation.
	Context context.Context
	// Claim atomically reserves the request immediately before a UI mutation.
	// It returns false if the LSP timeout already rejected the request.
	Claim   func() bool
	Respond func(applied bool, failureReason string)
}

// RenameResultMsg is sent when a rename result arrives.
type RenameResultMsg struct {
	DocumentRequestMetadata
	Edit WorkspaceEdit
}

// LspErrorMsg is sent when an LSP request returns an error.
type LspErrorMsg struct {
	Method  string
	Code    int
	Message string
}

// SignatureHelp represents signature help information.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

// SignatureInformation represents a function signature.
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// ParameterInformation represents a parameter in a signature.
type ParameterInformation struct {
	Label         any    `json:"label"` // string or [start, end]
	Documentation string `json:"documentation,omitempty"`
}

// SignatureHelpResultMsg is sent when signature help arrives.
type SignatureHelpResultMsg struct {
	OverlayRequestMetadata
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
	DocumentRequestMetadata
	FilePath string
	Ranges   []FoldingRange
}

// FormatStatus describes the outcome of a formatting request.
type FormatStatus string

const (
	FormatApplied     FormatStatus = "applied"
	FormatNoOp        FormatStatus = "noop"
	FormatUnsupported FormatStatus = "unsupported"
	FormatError       FormatStatus = "error"
)

// FormatResultMsg is sent when formatting result arrives.
type FormatResultMsg struct {
	RequestID int
	FilePath  string
	// BaseVersion is the document version used to request formatting. A
	// request with HasBaseVersion set lets the app reject edits computed for an
	// older snapshot. The explicit flag preserves compatibility with legacy
	// callers whose zero value did not carry version identity.
	BaseVersion    int
	HasBaseVersion bool
	Status         FormatStatus
	Edits          []TextEdit
	Err            error
}

// CodeActionResultMsg is sent when code actions arrive.
type CodeActionResultMsg struct {
	DocumentRequestMetadata
	Actions []CodeAction
}

// CodeAction represents a code action from the server.
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *struct {
		Title     string `json:"title"`
		Command   string `json:"command"`
		Arguments []any  `json:"arguments,omitempty"`
	} `json:"command,omitempty"`
}

// WorkspaceEdit represents a workspace edit.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit     `json:"changes,omitempty"`
	DocumentChanges []WorkspaceDocumentChange `json:"-"`
}

func (w *WorkspaceEdit) UnmarshalJSON(data []byte) error {
	type rawTextEdit struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		NewText string `json:"newText"`
	}
	type textDocumentEdit struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version *int   `json:"version"`
		} `json:"textDocument"`
		Edits []rawTextEdit `json:"edits"`
	}
	type fileOperation struct {
		Kind   WorkspaceFileOperationKind `json:"kind"`
		URI    string                     `json:"uri"`
		OldURI string                     `json:"oldUri"`
		NewURI string                     `json:"newUri"`
	}
	var raw struct {
		Changes         map[string][]rawTextEdit `json:"changes"`
		DocumentChanges []json.RawMessage        `json:"documentChanges"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	w.Changes = make(map[string][]TextEdit)
	w.DocumentChanges = nil

	appendEdits := func(uri string, version *int, edits []rawTextEdit, keepOrder bool) {
		if uri == "" || len(edits) == 0 {
			return
		}
		converted := make([]TextEdit, 0, len(edits))
		for _, edit := range edits {
			converted = append(converted, TextEdit{
				StartLine: edit.Range.Start.Line,
				StartCol:  edit.Range.Start.Character,
				EndLine:   edit.Range.End.Line,
				EndCol:    edit.Range.End.Character,
				NewText:   edit.NewText,
			})
		}
		w.Changes[uri] = append(w.Changes[uri], converted...)
		if keepOrder {
			w.DocumentChanges = append(w.DocumentChanges, WorkspaceDocumentChange{
				URI:     uri,
				Version: version,
				Edits:   converted,
			})
		}
	}

	for uri, edits := range raw.Changes {
		appendEdits(uri, nil, edits, false)
	}
	for _, rawChange := range raw.DocumentChanges {
		var op fileOperation
		if err := json.Unmarshal(rawChange, &op); err == nil && op.Kind != "" {
			w.DocumentChanges = append(w.DocumentChanges, WorkspaceDocumentChange{
				FileOperation: &WorkspaceFileOperation{
					Kind:   op.Kind,
					URI:    op.URI,
					OldURI: op.OldURI,
					NewURI: op.NewURI,
				},
			})
			continue
		}

		var change textDocumentEdit
		if err := json.Unmarshal(rawChange, &change); err != nil {
			continue
		}
		appendEdits(change.TextDocument.URI, change.TextDocument.Version, change.Edits, true)
	}

	if len(w.Changes) == 0 {
		w.Changes = nil
	}
	return nil
}

// DocumentSymbolResultMsg is sent when document symbols arrive.
type DocumentSymbolResultMsg struct {
	DocumentRequestMetadata
	Symbols []DocumentSymbol
}

// DocumentSymbol represents a symbol in a document.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
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
	Type    int // 1=Error, 2=Warning, 3=Info, 4=Log
	Message string
}

// LspProgressMsg is sent for progress notifications.
type LspProgressMsg struct {
	Token any
	Value any
}

// ServerExitedMsg is sent when an LSP server process exits unexpectedly.
type ServerExitedMsg struct {
	Command string
	Err     error
}
