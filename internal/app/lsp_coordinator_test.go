package app

import (
	"testing"

	"teak/internal/lsp"
)

// TestLSPCoordinatorCreation tests that the coordinator can be created
func TestLSPCoordinatorCreation(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}

	if coord.diagnostics == nil {
		t.Error("expected diagnostics map to be initialized")
	}

	if coord.triggerChars == nil {
		t.Error("expected triggerChars map to be initialized")
	}
}

// TestLSPCoordinatorHandleDiagnostics tests diagnostic message handling
func TestLSPCoordinatorHandleDiagnostics(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{},
	}

	cmds := coord.HandleMessage(msg)
	// Diagnostics are stored, no commands needed
	if cmds != nil {
		t.Error("expected nil commands for diagnostics")
	}

	// Verify diagnostics were stored
	if _, ok := coord.diagnostics["/test.go"]; !ok {
		t.Error("expected diagnostics to be stored")
	}
}

// TestLSPCoordinatorHandleCompletion tests completion message handling
func TestLSPCoordinatorHandleCompletion(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.CompletionResultMsg{
		Items: []lsp.CompletionItem{
			{Label: "test", InsertText: "test"},
		},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleHover tests hover message handling
func TestLSPCoordinatorHandleHover(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.HoverResultMsg{
		Content: "test hover content",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleDefinition tests definition message handling
func TestLSPCoordinatorHandleDefinition(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.DefinitionResultMsg{
		Locations: []lsp.Location{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleReferences tests references message handling
func TestLSPCoordinatorHandleReferences(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.ReferencesResultMsg{
		Locations: []lsp.Location{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleFormat tests format message handling
func TestLSPCoordinatorHandleFormat(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.FormatResultMsg{
		Edits: []lsp.TextEdit{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleCodeAction tests code action message handling
func TestLSPCoordinatorHandleCodeAction(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.CodeActionResultMsg{
		Actions: []lsp.CodeAction{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleDocumentSymbol tests document symbol message handling
func TestLSPCoordinatorHandleDocumentSymbol(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.DocumentSymbolResultMsg{
		Symbols: []lsp.DocumentSymbol{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleRename tests rename message handling
func TestLSPCoordinatorHandleRename(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.RenameResultMsg{
		Edit: lsp.WorkspaceEdit{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleFoldingRange tests folding range message handling
func TestLSPCoordinatorHandleFoldingRange(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.FoldingRangeResultMsg{
		FilePath: "/test.go",
		Ranges:   []lsp.FoldingRange{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleSignatureHelp tests signature help message handling
func TestLSPCoordinatorHandleSignatureHelp(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.SignatureHelpResultMsg{
		Help: &lsp.SignatureHelp{},
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleError tests error message handling
func TestLSPCoordinatorHandleError(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.LspErrorMsg{
		Method:  "test",
		Message: "test error",
		Code:    1,
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleProgress tests progress message handling
func TestLSPCoordinatorHandleProgress(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.LspProgressMsg{
		Token: "test",
		Value: "progress",
	}

	cmds := coord.HandleMessage(msg)
	// Progress returns nil (just acknowledges)
	if cmds != nil {
		t.Error("expected nil commands for progress")
	}
}

// TestLSPCoordinatorHandleShowMessage tests show message handling
func TestLSPCoordinatorHandleShowMessage(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	msg := lsp.LspShowMessageMsg{
		Type:    1,
		Message: "test message",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestLSPCoordinatorHandleLspReady tests LSP ready message handling
func TestLSPCoordinatorHandleLspReady(t *testing.T) {
	// Test with nil manager (should not crash)
	coord := NewLSPCoordinator(nil)

	msg := LspReadyMsg{
		FilePath: "/test.go",
	}

	cmds := coord.HandleMessage(msg)
	// With nil manager, returns nil
	if cmds != nil {
		t.Error("expected nil commands with nil manager")
	}
}

// TestLSPCoordinatorGetDiagnostics tests getting stored diagnostics
func TestLSPCoordinatorGetDiagnostics(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	// Store some diagnostics
	coord.diagnostics["/test.go"] = []lsp.Diagnostic{
		{Severity: 1, Message: "error"},
		{Severity: 2, Message: "warning"},
	}

	diags := coord.GetDiagnostics("/test.go")
	if len(diags) != 2 {
		t.Errorf("expected 2 diagnostics, got %d", len(diags))
	}
}

// TestLSPCoordinatorSetTriggerChars tests setting trigger characters
func TestLSPCoordinatorSetTriggerChars(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	chars := []string{".", "(", "["}
	coord.SetTriggerChars("/test.go", chars)

	if _, ok := coord.triggerChars["/test.go"]; !ok {
		t.Error("expected trigger chars to be stored")
	}
}

// TestLSPCoordinatorGetTriggerChars tests getting trigger characters
func TestLSPCoordinatorGetTriggerChars(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	// Non-existent file should return nil
	chars := coord.GetTriggerChars("/nonexistent.go")
	if chars != nil {
		t.Error("expected nil for non-existent file")
	}

	// Set and get
	coord.SetTriggerChars("/test.go", []string{"."})
	chars = coord.GetTriggerChars("/test.go")
	if len(chars) != 1 {
		t.Errorf("expected 1 trigger char, got %d", len(chars))
	}
}

// TestLSPCoordinatorClearDiagnostics tests clearing diagnostics
func TestLSPCoordinatorClearDiagnostics(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	// Store diagnostics
	coord.diagnostics["/test.go"] = []lsp.Diagnostic{{}}

	// Clear
	coord.ClearDiagnostics("/test.go")

	if _, ok := coord.diagnostics["/test.go"]; ok {
		t.Error("expected diagnostics to be cleared")
	}
}

// TestLSPCoordinatorAggregateDiagnostics tests aggregating diagnostics from all files
func TestLSPCoordinatorAggregateDiagnostics(t *testing.T) {
	coord := NewLSPCoordinator(nil)

	// Store diagnostics for multiple files
	coord.diagnostics["/file1.go"] = []lsp.Diagnostic{
		{Severity: 1, Message: "error1"},
	}
	coord.diagnostics["/file2.go"] = []lsp.Diagnostic{
		{Severity: 2, Message: "warning1"},
		{Severity: 1, Message: "error2"},
	}

	allDiags := coord.AggregateDiagnostics()
	if len(allDiags) != 3 {
		t.Errorf("expected 3 total diagnostics, got %d", len(allDiags))
	}
}
