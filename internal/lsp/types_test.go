package lsp

import (
	"encoding/json"
	"testing"
)

func TestServerCapabilities(t *testing.T) {
	caps := ServerCapabilities{
		HoverProvider:      true,
		DefinitionProvider: true,
		CompletionProvider: &struct {
			ResolveProvider   bool     `json:"resolveProvider,omitempty"`
			TriggerCharacters []string `json:"triggerCharacters,omitempty"`
		}{
			TriggerCharacters: []string{".", ":"},
		},
	}

	if !capabilityEnabled(caps.HoverProvider) {
		t.Error("HoverProvider should be true")
	}
	if !capabilityEnabled(caps.DefinitionProvider) {
		t.Error("DefinitionProvider should be true")
	}
	if caps.CompletionProvider == nil {
		t.Fatal("CompletionProvider should not be nil")
	}
	if len(caps.CompletionProvider.TriggerCharacters) != 2 {
		t.Errorf("expected 2 trigger characters, got %d", len(caps.CompletionProvider.TriggerCharacters))
	}
}

func TestInitializeResultUnmarshalObjectProviders(t *testing.T) {
	data := []byte(`{
		"capabilities": {
			"hoverProvider": {"workDoneProgress": true},
			"definitionProvider": {"workDoneProgress": true},
			"referencesProvider": {"workDoneProgress": true},
			"renameProvider": {"prepareProvider": true},
			"documentSymbolProvider": {"label": "symbols"},
			"documentFormattingProvider": {"workDoneProgress": true},
			"documentRangeFormattingProvider": {"workDoneProgress": true},
			"foldingRangeProvider": {"lineFoldingOnly": true}
		}
	}`)

	var result InitializeResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if !capabilityEnabled(result.Capabilities.HoverProvider) {
		t.Fatal("hoverProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.DefinitionProvider) {
		t.Fatal("definitionProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.ReferencesProvider) {
		t.Fatal("referencesProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.RenameProvider) {
		t.Fatal("renameProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.DocumentSymbolProvider) {
		t.Fatal("documentSymbolProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.FormattingProvider) {
		t.Fatal("documentFormattingProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.RangeFormattingProvider) {
		t.Fatal("documentRangeFormattingProvider should be treated as supported")
	}
	if !capabilityEnabled(result.Capabilities.FoldingRangeProvider) {
		t.Fatal("foldingRangeProvider should be treated as supported")
	}
}

func TestCapabilityEnabledBoolAndNil(t *testing.T) {
	if capabilityEnabled(nil) {
		t.Fatal("nil capability should be disabled")
	}
	if capabilityEnabled(false) {
		t.Fatal("false capability should be disabled")
	}
	if !capabilityEnabled(true) {
		t.Fatal("true capability should be enabled")
	}
}

func TestErrorCodeConstants(t *testing.T) {
	// Verify error code constants match LSP spec
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"ParseError", ParseError, -32700},
		{"InvalidRequest", InvalidRequest, -32600},
		{"MethodNotFound", MethodNotFound, -32601},
		{"InvalidParams", InvalidParams, -32602},
		{"InternalError", InternalError, -32603},
		{"ServerNotInitialized", ServerNotInitialized, -32002},
		{"RequestCancelled", RequestCancelled, -32800},
		{"ContentModified", ContentModified, -32801},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.expected)
			}
		})
	}
}

func TestDiagnosticSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity DiagSeverity
		expected string
	}{
		{"Error", SeverityError, "error"},
		{"Warning", SeverityWarning, "warning"},
		{"Info", SeverityInfo, "info"},
		{"Hint", SeverityHint, "hint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.severity < 1 || tt.severity > 4 {
				t.Errorf("%s severity %d out of valid range [1-4]", tt.name, tt.severity)
			}
		})
	}
}

func TestTextEdit(t *testing.T) {
	edit := TextEdit{
		StartLine: 1,
		StartCol:  5,
		EndLine:   1,
		EndCol:    10,
		NewText:   "replacement",
	}

	if edit.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", edit.StartLine)
	}
	if edit.StartCol != 5 {
		t.Errorf("StartCol = %d, want 5", edit.StartCol)
	}
	if edit.NewText != "replacement" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "replacement")
	}
}

func TestLocation(t *testing.T) {
	loc := Location{
		URI:       "file:///test.go",
		StartLine: 10,
		StartCol:  5,
		EndLine:   10,
		EndCol:    15,
	}

	if loc.URI != "file:///test.go" {
		t.Errorf("URI = %q, want %q", loc.URI, "file:///test.go")
	}
	if loc.StartLine != 10 {
		t.Errorf("StartLine = %d, want 10", loc.StartLine)
	}
}

func TestCompletionItem(t *testing.T) {
	item := CompletionItem{
		Label:      "Foo",
		Detail:     "func Foo()",
		InsertText: "Foo",
		Kind:       3, // Function
	}

	if item.Label != "Foo" {
		t.Errorf("Label = %q, want %q", item.Label, "Foo")
	}
	if item.Kind != 3 {
		t.Errorf("Kind = %d, want 3", item.Kind)
	}
}

func TestHoverResult(t *testing.T) {
	hover := HoverResult{
		Content: "This is documentation",
	}

	if hover.Content != "This is documentation" {
		t.Errorf("Content = %q, want %q", hover.Content, "This is documentation")
	}
}

func TestSignatureHelpStruct(t *testing.T) {
	help := SignatureHelp{
		Signatures: []SignatureInformation{
			{
				Label:         "func Foo(a, b)",
				Documentation: "Foo documentation",
				Parameters: []ParameterInformation{
					{Label: "a", Documentation: "param a"},
					{Label: "b", Documentation: "param b"},
				},
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 1,
	}

	if len(help.Signatures) != 1 {
		t.Errorf("expected 1 signature, got %d", len(help.Signatures))
	}
	if help.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1", help.ActiveParameter)
	}
}

func TestCodeAction(t *testing.T) {
	action := CodeAction{
		Title: "Quick Fix",
		Kind:  "quickfix",
		Edit: &WorkspaceEdit{
			Changes: map[string][]TextEdit{
				"file:///test.go": {
					{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5, NewText: "fixed"},
				},
			},
		},
	}

	if action.Title != "Quick Fix" {
		t.Errorf("Title = %q, want %q", action.Title, "Quick Fix")
	}
	if action.Edit == nil {
		t.Fatal("Edit should not be nil")
	}
	if len(action.Edit.Changes) != 1 {
		t.Errorf("expected 1 change, got %d", len(action.Edit.Changes))
	}
}

func TestDocumentSymbol(t *testing.T) {
	symbol := DocumentSymbol{
		Name:     "MyFunction",
		Detail:   "func MyFunction()",
		Kind:     12, // Function
		Range:    Range{Start: Position{Line: 10, Character: 0}, End: Position{Line: 20, Character: 1}},
		Children: []DocumentSymbol{},
	}

	if symbol.Name != "MyFunction" {
		t.Errorf("Name = %q, want %q", symbol.Name, "MyFunction")
	}
	if symbol.Kind != 12 {
		t.Errorf("Kind = %d, want 12", symbol.Kind)
	}
}

func TestRangeAndPosition(t *testing.T) {
	pos := Position{Line: 5, Character: 10}
	rng := Range{Start: pos, End: Position{Line: 5, Character: 20}}

	if rng.Start.Line != 5 {
		t.Errorf("Start.Line = %d, want 5", rng.Start.Line)
	}
	if rng.End.Character != 20 {
		t.Errorf("End.Character = %d, want 20", rng.End.Character)
	}
}
