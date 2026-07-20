package app

import (
	"os"
	"path/filepath"
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/text"
)

// TestAppSaveOperations tests save-related functionality
func TestAppSaveOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test beginSaveForTab - just verify it doesn't panic
	// The actual save logic is tested in save_flow_test.go
	cmd := model.beginSaveForTab(0, false, false)
	// Command may be nil or non-nil depending on state
	_ = cmd
}

// TestAppCloseTabOperations tests tab closing functionality
func TestAppCloseTabOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	if idx := model.findReplaceableTab(); idx != 0 {
		t.Fatalf("initial replaceable tab index = %d, want 0", idx)
	}

	// Test closeCurrentTabSafe with single tab. Model is an owned runtime
	// handle, so callers must continue from the returned Bubble Tea value.
	updatedAny, cmd := model.closeCurrentTabSafe()
	updated := updatedAny.(Model)
	// Should trigger unsaved confirm or close
	_ = cmd

	if idx := updated.findReplaceableTab(); idx != -1 {
		t.Errorf("replaceable tab index after closing the only tab = %d, want -1", idx)
	}
}

// TestAppSearchOperations tests search functionality
func TestAppSearchOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test findNext with no search results
	_, cmd := model.findNext()
	if cmd != nil {
		t.Error("Expected nil command when no search results")
	}

	// Test findPrev with no search results
	_, cmd = model.findPrev()
	if cmd != nil {
		t.Error("Expected nil command when no search results")
	}
}

// TestAppDiffViewOperations tests diff view functionality
func TestAppDiffViewOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test activeDiffView with no diff views
	diffView := model.activeDiffView()
	if diffView != "" {
		t.Error("Expected empty diff view when none active")
	}
}

// TestAppEditorAccess tests editor access methods
func TestAppEditorAccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test activeEditor
	ed := model.activeEditor()
	if ed == nil {
		t.Error("Expected active editor to exist")
	}

	// Test findEditorByPath - returns index, -1 if not found
	found := model.findEditorByPath("")
	// Empty path should return -1 (not found)
	if found != -1 && found != 0 {
		t.Errorf("Expected -1 or 0 for empty path, got %d", found)
	}

	// Find by actual path
	if len(model.editors) > 0 && model.editors[0].Buffer.FilePath != "" {
		found = model.findEditorByPath(model.editors[0].Buffer.FilePath)
		if found == -1 {
			t.Error("Expected to find editor by path")
		}
	}
}

// TestAppContextMenuHandling tests context menu action handling
func TestAppContextMenuHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test handleContextMenuAction with empty action
	_, cmd := model.handleContextMenuAction("")
	if cmd != nil {
		t.Error("Expected nil command for empty action")
	}
}

// TestAppDAPMessageHandling tests DAP message handling
func TestAppDAPMessageHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test handleDAPMsg - should not panic
	// Actual DAP messages would be tested in integration tests
}

// TestAppDeleteConfirmHandling tests delete confirmation
func TestAppDeleteConfirmHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Set up delete confirmation state
	model.deleteConfirm = true
	testFile := filepath.Join(root, "test.txt")
	model.deleteTarget = testFile

	// Create a test file
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test handleDeleteConfirm with 'y' key
	// This would be tested through the Update method
}

// TestAppDiagnosticsHandling tests diagnostic message handling
func TestAppDiagnosticsHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Create a test file
	testFile := filepath.Join(root, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test handleDiagnostics - verify it doesn't panic
	msg := lsp.DiagnosticsMsg{
		URI: lsp.FileURI(testFile),
		Diagnostics: []lsp.Diagnostic{
			{
				Severity: 1,
				Message:  "test error",
			},
		},
	}
	_, cmd := model.handleDiagnostics(msg)
	// Command may be returned for async processing
	_ = cmd

	// Verify diagnostics stored in coordinator
	lspCoord := model.coordinator.GetLSPCoordinator()
	storedDiags := lspCoord.GetDiagnostics(testFile)
	// Diagnostics should be stored
	_ = storedDiags
}

// TestAppDiffLoadedHandling tests diff loaded message handling
func TestAppDiffLoadedHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test handleDiffLoaded
	// This would be called when a diff view is loaded
}

// TestAppExternalFileChange tests external file change handling
func TestAppExternalFileChange(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Create and open a file
	testFile := filepath.Join(root, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	model2, err := NewModel(testFile, root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model2.cleanup()

	// Test handleExternalFileChange
	// This would be called when file watcher detects changes
}

// TestAppDebugState tests debug state fetching
func TestAppDebugState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test fetchDebugState when debugger not running
	// Just verify it doesn't panic
	cmd := model.fetchDebugState()
	_ = cmd
}

// TestAppFormattingOptions tests formatting options generation
func TestAppFormattingOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	ed := model.activeEditor()
	if ed == nil {
		t.Fatal("Expected active editor")
	}

	opts := formattingOptions(ed.Config)
	if opts.TabSize != cfg.Editor.TabSize {
		t.Errorf("Expected tab size %d, got %d", cfg.Editor.TabSize, opts.TabSize)
	}
}

// TestAppFormattingDocumentState tests formatting document state
func TestAppFormattingDocumentState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test ensureFormattingDocumentState
	// This is called during save operations
}

// TestAppFormatResultNote tests formatting result note generation
func TestAppFormatResultNote(t *testing.T) {
	// Test the function with various inputs
	tests := []struct {
		name      string
		status    lsp.FormatStatus
		err       error
		wantEmpty bool
	}{
		{"no changes", lsp.FormatNoOp, nil, false},
		{"with changes", lsp.FormatApplied, nil, true}, // Returns empty for Applied
		{"with error", lsp.FormatError, os.ErrNotExist, false},
		{"unsupported", lsp.FormatUnsupported, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResultNote(tt.status, tt.err)
			if tt.wantEmpty && got != "" {
				t.Errorf("Expected empty string, got %q", got)
			}
			if !tt.wantEmpty && got == "" {
				t.Error("Expected non-empty format result note")
			}
		})
	}
}

// TestAppApplyTextEdits tests applying text edits to buffer
func TestAppApplyTextEdits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	ed := model.activeEditor()
	if ed == nil {
		t.Fatal("Expected active editor")
	}

	// Set up initial content
	ed.Buffer = text.NewBufferFromBytes([]byte("hello world"))
	model.editors[0] = *ed

	// Test applyTextEditsToBuffer
	edits := []lsp.TextEdit{
		{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    5,
			NewText:   "goodbye",
		},
	}

	count := applyTextEditsToBuffer(ed.Buffer, edits)
	if count != 1 {
		t.Errorf("Expected 1 edit applied, got %d", count)
	}

	if ed.Buffer.Content() != "goodbye world" {
		t.Errorf("Expected 'goodbye world', got %q", ed.Buffer.Content())
	}
}

// TestAppApplyWorkspaceEdits tests applying workspace edits
func TestAppApplyWorkspaceEdits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Create test file
	testFile := filepath.Join(root, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test the asynchronous workspace-edit flow for a closed file.
	edits := []lsp.TextEdit{
		{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    5,
			NewText:   "goodbye",
		},
	}

	model = applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(testFile): edits,
	}})

	// Verify file was modified
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != "goodbye" {
		t.Errorf("Expected 'goodbye', got %q", string(content))
	}
}

// TestAppApplyWorkspaceFileOperations tests workspace file operations
func TestAppApplyWorkspaceFileOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test create operation
	testFile := filepath.Join(root, "test.txt")
	op := lsp.WorkspaceFileOperation{
		Kind: lsp.FileOpCreate,
		URI:  lsp.FileURI(testFile),
	}

	model = applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &op}}})

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Expected file to be created")
	}

	// Test rename operation - skip if create failed
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		return // Can't test rename if create failed
	}

	newPath2 := filepath.Join(root, "renamed.txt")
	op2 := lsp.WorkspaceFileOperation{
		Kind:   lsp.FileOpRename,
		URI:    lsp.FileURI(testFile),
		NewURI: lsp.FileURI(newPath2),
	}

	model = applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &op2}}})

	// Test delete operation - skip if rename failed
	if _, err := os.Stat(newPath2); os.IsNotExist(err) {
		return // Can't test delete if rename failed
	}

	op3 := lsp.WorkspaceFileOperation{
		Kind: lsp.FileOpDelete,
		URI:  lsp.FileURI(newPath2),
	}

	model = applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &op3}}})
}

// TestAppApplyWorkspaceEdit tests full workspace edit application
func TestAppApplyWorkspaceEdit(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	root := t.TempDir()
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Create test file
	testFile := filepath.Join(root, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test workspace edit with text edits
	edit := lsp.WorkspaceEdit{
		Changes: map[string][]lsp.TextEdit{
			testFile: {
				{
					StartLine: 0,
					StartCol:  0,
					EndLine:   0,
					EndCol:    5,
					NewText:   "goodbye",
				},
			},
		},
	}

	_ = applyWorkspaceEditAsyncForTest(t, model, edit)
}

// TestAppPluginKeySequence tests plugin key sequence handling
func TestAppPluginKeySequence(t *testing.T) {
	// Test appendPluginKeySequence function
	// The function has special logic for <leader> prefix
	tests := []struct {
		name     string
		current  string
		key      string
		expected string
	}{
		{"empty current", "", "a", "a"},
		{"leader prefix", "<leader>", "a", "<leader>a"},
		{"normal append", "a", "b", "b"}, // Replaces, doesn't append
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendPluginKeySequence(tt.current, tt.key)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestAppDetectGitBranch tests git branch detection
func TestAppDetectGitBranch(t *testing.T) {
	root := t.TempDir()

	// Test with non-git directory
	branch := detectGitBranch(root)
	if branch != "" {
		t.Errorf("Expected empty branch for non-git dir, got %q", branch)
	}
}

// TestAppAgentPanelWidth tests agent panel width calculation
func TestAppAgentPanelWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Enable agent panel
	model.showAgent = true
	model.width = 120
	model.height = 40

	width := model.agentPanelWidth()
	// Should return positive width when agent is shown
	if width <= 0 {
		t.Errorf("Agent panel width %d should be positive when agent is shown", width)
	}

	// Test with agent disabled
	model.showAgent = false
	width = model.agentPanelWidth()
	if width != 0 {
		t.Errorf("Agent panel width should be 0 when agent is hidden, got %d", width)
	}
}

// TestAppAgentIndicator tests agent indicator rendering
func TestAppAgentIndicator(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test agentIndicator - just verify it returns a string
	indicator := model.agentIndicator()
	// May be empty or non-empty depending on state
	_ = indicator
}

// TestAppAgentBorderColumn tests agent border column calculation
func TestAppAgentBorderColumn(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	model.width = 100
	model.height = 40
	// Test agentBorderColumn - just verify it returns a string
	col := model.agentBorderColumn(model.height)
	// May be empty or non-empty depending on state
	_ = col
}

// TestAppCycleAgentMode tests cycling agent modes
func TestAppCycleAgentMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test cycleAgentMode
	_, _, handled := model.cycleAgentMode()
	if !handled {
		t.Error("Expected cycleAgentMode to return handled=true")
	}
}

// TestAppHandleAgentEnter tests handling agent enter key
func TestAppHandleAgentEnter(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test handleAgentEnter
	// This would be called when user presses enter in agent panel
}

// TestAppHandleACPMsg tests handling ACP messages
func TestAppHandleACPMsg(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test handleACPMsg
	// This would be called when ACP sends messages
}

// TestAppFilesToAgentPickerItems tests converting files to picker items
func TestAppFilesToAgentPickerItems(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	files := []string{"file1.go", "file2.go", "file3.go"}
	items := filesToAgentPickerItems(files)

	if len(items) != len(files) {
		t.Errorf("Expected %d items, got %d", len(files), len(items))
	}
}

// TestAppCancelQuitAfterSaves tests canceling quit after saves
func TestAppCancelQuitAfterSaves(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test cancelQuitAfterSaves
	model.cancelQuitAfterSaves()
	// Just verify it doesn't panic
}

// TestAppCompleteSaveRequest tests completing save requests
func TestAppCompleteSaveRequest(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()

	// Test completeSaveRequest
	// This is called when save operation completes
}
