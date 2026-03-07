package app

import (
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
)

// TestAppLSPMessageHandling tests LSP message handling through coordinator
func TestAppLSPMessageHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test diagnostics message
	msg := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{{Severity: 1, Message: "test error"}},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Verify diagnostics stored
	diags := lspCoord.GetDiagnostics("/test.go")
	if len(diags) != 1 {
		t.Errorf("Expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Message != "test error" {
		t.Errorf("Expected message 'test error', got %q", diags[0].Message)
	}
}

// TestAppLSPMultipleDiagnostics tests multiple LSP diagnostics
func TestAppLSPMultipleDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Add diagnostics for multiple files
	files := []string{"/test1.go", "/test2.go", "/test3.go"}
	for i, file := range files {
		msg := lsp.DiagnosticsMsg{
			URI:         "file://" + file,
			Diagnostics: []lsp.Diagnostic{{Severity: lsp.DiagSeverity(i + 1)}},
		}
		lspCoord.HandleMessage(msg)
	}

	// Verify all stored
	for _, file := range files {
		diags := lspCoord.GetDiagnostics(file)
		if len(diags) != 1 {
			t.Errorf("Expected 1 diagnostic for %s, got %d", file, len(diags))
		}
	}

	// Aggregate should return all
	allDiags := lspCoord.AggregateDiagnostics()
	if len(allDiags) != 3 {
		t.Errorf("Expected 3 diagnostics, got %d", len(allDiags))
	}
}

// TestAppLSPCompletionMessage tests LSP completion message handling
func TestAppLSPCompletionMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test completion message
	msg := lsp.CompletionResultMsg{
		Items: []lsp.CompletionItem{
			{Label: "test1", InsertText: "test1"},
			{Label: "test2", InsertText: "test2"},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPHoverMessage tests LSP hover message handling
func TestAppLSPHoverMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test hover message
	msg := lsp.HoverResultMsg{
		Content: "test hover content",
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPDefinitionMessage tests LSP definition message handling
func TestAppLSPDefinitionMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test definition message
	msg := lsp.DefinitionResultMsg{
		Locations: []lsp.Location{
			{URI: "file:///test.go", StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPReferencesMessage tests LSP references message handling
func TestAppLSPReferencesMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test references message
	msg := lsp.ReferencesResultMsg{
		Locations: []lsp.Location{
			{URI: "file:///test.go", StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPFormatMessage tests LSP format message handling
func TestAppLSPFormatMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test format message
	msg := lsp.FormatResultMsg{
		Edits: []lsp.TextEdit{
			{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5, NewText: "test"},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPCodeActionMessage tests LSP code action message handling
func TestAppLSPCodeActionMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test code action message
	msg := lsp.CodeActionResultMsg{
		Actions: []lsp.CodeAction{
			{Title: "test action", Kind: "quickfix"},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPRenameMessage tests LSP rename message handling
func TestAppLSPRenameMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test rename message
	msg := lsp.RenameResultMsg{
		Edits: map[string][]lsp.TextEdit{
			"/test.go": {{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5, NewText: "test"}},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPDocumentSymbolMessage tests LSP document symbol message handling
func TestAppLSPDocumentSymbolMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test document symbol message
	msg := lsp.DocumentSymbolResultMsg{
		Symbols: []lsp.DocumentSymbol{
			{Name: "test", Kind: 1},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPSignatureHelpMessage tests LSP signature help message handling
func TestAppLSPSignatureHelpMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test signature help message
	msg := lsp.SignatureHelpResultMsg{
		Help: &lsp.SignatureHelp{
			Signatures: []lsp.SignatureInformation{
				{Label: "test()"},
			},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPFoldingRangeMessage tests LSP folding range message handling
func TestAppLSPFoldingRangeMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test folding range message
	msg := lsp.FoldingRangeResultMsg{
		Ranges: []lsp.FoldingRange{
			{StartLine: 1, EndLine: 10},
		},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPEmptyDiagnostics tests LSP empty diagnostics handling
func TestAppLSPEmptyDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty diagnostics
	msg := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should store empty diagnostics
	diags := lspCoord.GetDiagnostics("/test.go")
	if diags == nil {
		t.Error("Expected diagnostics to be stored (even if empty)")
	}
}

// TestAppLSPClearDiagnostics tests LSP clear diagnostics
func TestAppLSPClearDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Add diagnostics
	msg := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{{Severity: 1}},
	}
	lspCoord.HandleMessage(msg)

	// Clear diagnostics
	lspCoord.ClearDiagnostics("/test.go")

	// Verify cleared
	diags := lspCoord.GetDiagnostics("/test.go")
	if len(diags) != 0 {
		t.Errorf("Expected 0 diagnostics after clear, got %d", len(diags))
	}
}

// TestAppLSPTriggerChars tests LSP trigger characters
func TestAppLSPTriggerChars(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Set trigger characters
	chars := []string{".", "(", "["}
	lspCoord.SetTriggerChars("/test.go", chars)

	// Get trigger characters
	retrieved := lspCoord.GetTriggerChars("/test.go")
	if len(retrieved) != 3 {
		t.Errorf("Expected 3 trigger chars, got %d", len(retrieved))
	}

	// Verify characters
	for i, c := range chars {
		if retrieved[i] != c {
			t.Errorf("Expected char %d to be %q, got %q", i, c, retrieved[i])
		}
	}
}

// TestAppLSPTriggerCharsNonExistent tests LSP trigger characters for non-existent file
func TestAppLSPTriggerCharsNonExistent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Get trigger characters for non-existent file
	chars := lspCoord.GetTriggerChars("/nonexistent.go")
	if chars != nil {
		t.Errorf("Expected nil for non-existent file, got %v", chars)
	}
}

// TestAppLSPAggregateDiagnosticsEmpty tests aggregate diagnostics with no diagnostics
func TestAppLSPAggregateDiagnosticsEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Aggregate with no diagnostics
	allDiags := lspCoord.AggregateDiagnostics()
	if len(allDiags) != 0 {
		t.Errorf("Expected 0 diagnostics, got %d", len(allDiags))
	}
}

// TestAppLSPMultipleDiagnosticsPerFile tests multiple diagnostics per file
func TestAppLSPMultipleDiagnosticsPerFile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Add multiple diagnostics for same file
	msg := lsp.DiagnosticsMsg{
		URI: "file:///test.go",
		Diagnostics: []lsp.Diagnostic{
			{Severity: 1, Message: "error1"},
			{Severity: 2, Message: "warning1"},
			{Severity: 3, Message: "info1"},
		},
	}

	lspCoord.HandleMessage(msg)

	// Verify all stored
	diags := lspCoord.GetDiagnostics("/test.go")
	if len(diags) != 3 {
		t.Errorf("Expected 3 diagnostics, got %d", len(diags))
	}
}

// TestAppLSPUpdateDiagnostics tests LSP update diagnostics (replace existing)
func TestAppLSPUpdateDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Add initial diagnostics
	msg1 := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{{Severity: 1, Message: "error1"}},
	}
	lspCoord.HandleMessage(msg1)

	// Update diagnostics (should replace)
	msg2 := lsp.DiagnosticsMsg{
		URI: "file:///test.go",
		Diagnostics: []lsp.Diagnostic{
			{Severity: 2, Message: "warning1"},
			{Severity: 3, Message: "info1"},
		},
	}
	lspCoord.HandleMessage(msg2)

	// Verify updated
	diags := lspCoord.GetDiagnostics("/test.go")
	if len(diags) != 2 {
		t.Errorf("Expected 2 diagnostics after update, got %d", len(diags))
	}
}

// TestAppLSPDiagnosticsForMultipleFiles tests diagnostics for multiple files
func TestAppLSPDiagnosticsForMultipleFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Add diagnostics for 5 files
	for i := 0; i < 5; i++ {
		msg := lsp.DiagnosticsMsg{
			URI:         "file:///test" + string(rune('0'+i)) + ".go",
			Diagnostics: []lsp.Diagnostic{{Severity: lsp.DiagSeverity(i + 1)}},
		}
		lspCoord.HandleMessage(msg)
	}

	// Aggregate should return all
	allDiags := lspCoord.AggregateDiagnostics()
	if len(allDiags) != 5 {
		t.Errorf("Expected 5 diagnostics, got %d", len(allDiags))
	}
}

// TestAppLSPCompletionEmptyItems tests LSP completion with empty items
func TestAppLSPCompletionEmptyItems(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty completion items
	msg := lsp.CompletionResultMsg{
		Items: []lsp.CompletionItem{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPHoverEmptyContent tests LSP hover with empty content
func TestAppLSPHoverEmptyContent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty hover content
	msg := lsp.HoverResultMsg{
		Content: "",
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPDefinitionEmptyLocations tests LSP definition with empty locations
func TestAppLSPDefinitionEmptyLocations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty definition locations
	msg := lsp.DefinitionResultMsg{
		Locations: []lsp.Location{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPReferencesEmptyLocations tests LSP references with empty locations
func TestAppLSPReferencesEmptyLocations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty reference locations
	msg := lsp.ReferencesResultMsg{
		Locations: []lsp.Location{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPFormatEmptyEdits tests LSP format with empty edits
func TestAppLSPFormatEmptyEdits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty format edits
	msg := lsp.FormatResultMsg{
		Edits: []lsp.TextEdit{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPCodeActionEmptyActions tests LSP code action with empty actions
func TestAppLSPCodeActionEmptyActions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty code actions
	msg := lsp.CodeActionResultMsg{
		Actions: []lsp.CodeAction{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPRenameEmptyEdits tests LSP rename with empty edits
func TestAppLSPRenameEmptyEdits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty rename edits
	msg := lsp.RenameResultMsg{
		Edits: map[string][]lsp.TextEdit{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPDocumentSymbolEmptySymbols tests LSP document symbol with empty symbols
func TestAppLSPDocumentSymbolEmptySymbols(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty document symbols
	msg := lsp.DocumentSymbolResultMsg{
		Symbols: []lsp.DocumentSymbol{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPSignatureHelpNilHelp tests LSP signature help with nil help
func TestAppLSPSignatureHelpNilHelp(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test nil signature help
	msg := lsp.SignatureHelpResultMsg{
		Help: nil,
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}

// TestAppLSPFoldingRangeEmptyRanges tests LSP folding range with empty ranges
func TestAppLSPFoldingRangeEmptyRanges(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	lspCoord := model.coordinator.GetLSPCoordinator()

	// Test empty folding ranges
	msg := lsp.FoldingRangeResultMsg{
		Ranges: []lsp.FoldingRange{},
	}

	cmds := lspCoord.HandleMessage(msg)
	_ = cmds

	// Should not crash
}
