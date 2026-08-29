package app

import (
	"fmt"
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

func TestLSPCoordinatorRelocateFilePath(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	coord.diagnostics["/src/main.go"] = []lsp.Diagnostic{{Message: "stale"}}
	coord.triggerChars["/src/main.go"] = []string{"."}

	coord.RelocateFilePath("/src/main.go", "/dest/main.go")

	if got := coord.GetDiagnostics("/src/main.go"); len(got) != 0 {
		t.Fatalf("old diagnostics = %#v, want empty", got)
	}
	if got := coord.GetDiagnostics("/dest/main.go"); len(got) != 1 || got[0].Message != "stale" {
		t.Fatalf("new diagnostics = %#v, want relocated diagnostics", got)
	}
	if got := coord.GetTriggerChars("/dest/main.go"); len(got) != 1 || got[0] != "." {
		t.Fatalf("new trigger chars = %#v, want relocated trigger", got)
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

// publishDiagnostics publishes one diagnostic for the nth test file.
func publishDiagnostics(t *testing.T, coord *LSPCoordinator, fileIndex int) {
	t.Helper()
	coord.HandleMessage(lsp.DiagnosticsMsg{
		URI:         lsp.FileURI(fmt.Sprintf("/evict/file%04d.go", fileIndex)),
		Diagnostics: []lsp.Diagnostic{{Severity: 1, Message: "error"}},
	})
}

// TestDiagnosticsEvictionRemovesOldestFiles pins the eviction policy: the
// first-inserted files are dropped when the cache exceeds its cap. Map
// iteration order is random, so anything else evicts an arbitrary file.
func TestDiagnosticsEvictionRemovesOldestFiles(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	const overflow = 3
	for i := 0; i < maxLSPDiagnosticsFiles+overflow; i++ {
		publishDiagnostics(t, coord, i)
	}

	for i := 0; i < overflow; i++ {
		path := fmt.Sprintf("/evict/file%04d.go", i)
		if got := coord.GetDiagnostics(path); len(got) != 0 {
			t.Errorf("oldest file %s survived eviction with %d diagnostics", path, len(got))
		}
	}
	for i := overflow; i < maxLSPDiagnosticsFiles+overflow; i++ {
		path := fmt.Sprintf("/evict/file%04d.go", i)
		if got := coord.GetDiagnostics(path); len(got) != 1 {
			t.Errorf("newest file %s was evicted (got %d diagnostics, want 1)", path, len(got))
		}
	}
	if total := len(coord.AggregateDiagnostics()); total != maxLSPDiagnosticsFiles {
		t.Errorf("aggregate diagnostics = %d, want cap %d", total, maxLSPDiagnosticsFiles)
	}
}

// Re-publishing an existing file updates its diagnostics without refreshing
// its insertion position: the file remains the oldest and is evicted first.
func TestDiagnosticsEvictionKeepsInsertionOrderOnUpdate(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	for i := 0; i < maxLSPDiagnosticsFiles; i++ {
		publishDiagnostics(t, coord, i)
	}
	publishDiagnostics(t, coord, 0)
	publishDiagnostics(t, coord, maxLSPDiagnosticsFiles)

	if got := coord.GetDiagnostics("/evict/file0000.go"); len(got) != 0 {
		t.Errorf("re-published oldest file survived eviction with %d diagnostics", len(got))
	}
	if got := coord.GetDiagnostics("/evict/file0001.go"); len(got) != 1 {
		t.Errorf("second-oldest file = %d diagnostics, want 1", len(got))
	}
	if got := coord.GetDiagnostics(fmt.Sprintf("/evict/file%04d.go", maxLSPDiagnosticsFiles)); len(got) != 1 {
		t.Errorf("newest file = %d diagnostics, want 1", len(got))
	}
}

// A cleared file must leave the eviction order too, or a later eviction
// "removes" a file that is already gone and leaves the cache above its cap.
func TestDiagnosticsEvictionSkipsClearedFiles(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	for i := 0; i < maxLSPDiagnosticsFiles; i++ {
		publishDiagnostics(t, coord, i)
	}
	coord.ClearDiagnostics("/evict/file0000.go")
	for i := maxLSPDiagnosticsFiles; i < maxLSPDiagnosticsFiles+2; i++ {
		publishDiagnostics(t, coord, i)
	}

	if total := len(coord.AggregateDiagnostics()); total != maxLSPDiagnosticsFiles {
		t.Errorf("aggregate diagnostics = %d, want cap %d", total, maxLSPDiagnosticsFiles)
	}
	if got := coord.GetDiagnostics("/evict/file0001.go"); len(got) != 0 {
		t.Errorf("oldest remaining file = %d diagnostics, want evicted", len(got))
	}
	if got := coord.GetDiagnostics(fmt.Sprintf("/evict/file%04d.go", maxLSPDiagnosticsFiles+1)); len(got) != 1 {
		t.Errorf("newest file = %d diagnostics, want 1", len(got))
	}
}

// StorePreparedDiagnostics shares the same cache and must evict by age too.
func TestPreparedDiagnosticsEvictionRemovesOldestFiles(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	for i := 0; i <= maxLSPDiagnosticsFiles; i++ {
		coord.StorePreparedDiagnostics(fmt.Sprintf("/evict/prepared%04d.go", i), []lsp.Diagnostic{{Message: "error"}})
	}

	if got := coord.GetDiagnostics("/evict/prepared0000.go"); len(got) != 0 {
		t.Errorf("oldest prepared file survived eviction with %d diagnostics", len(got))
	}
	if got := coord.GetDiagnostics(fmt.Sprintf("/evict/prepared%04d.go", maxLSPDiagnosticsFiles)); len(got) != 1 {
		t.Errorf("newest prepared file = %d diagnostics, want 1", len(got))
	}
}
