package lsp

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
