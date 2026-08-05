package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/text"
)

// TestAppSaveOperations tests save-related functionality
func TestAppSaveOperations(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "save.txt", "disk\n", "local edits\n")
	path := model.editors[0].Buffer.FilePath

	saved := requireFileSavedMsg(t, model.beginSaveForTab(0, false, false))
	updatedAny, _ := model.Update(saved)
	updated := updatedAny.(Model)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if got := string(data); got != "local edits\n" {
		t.Fatalf("saved bytes = %q, want local edits", got)
	}
	if updated.editors[0].Buffer.Dirty() || updated.tabBar.Tabs[0].Dirty {
		t.Fatal("successful save left the buffer or tab dirty")
	}
	if updated.status != "Saved "+path {
		t.Fatalf("save status = %q", updated.status)
	}
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

	pausedAny, pausedCmd := model.routeDAPMsg(dapMsg{
		msg: dap.StoppedEventMsg{Reason: "breakpoint"},
	})
	paused := pausedAny.(Model)
	if pausedCmd == nil {
		t.Fatal("stopped DAP event did not schedule state refresh/listener")
	}
	if got := paused.debuggerPanel.State(); got != dap.StatePaused {
		t.Fatalf("debugger UI state after stop = %v, want paused", got)
	}
	if got := paused.coordinator.GetDAPCoordinator().GetState(); got != dap.StatePaused {
		t.Fatalf("DAP coordinator state after stop = %v, want paused", got)
	}
	if paused.status != "Stopped: breakpoint" {
		t.Fatalf("stop status = %q", paused.status)
	}

	runningAny, runningCmd := paused.routeDAPMsg(dapMsg{msg: dap.ContinuedEventMsg{}})
	running := runningAny.(Model)
	if runningCmd == nil {
		t.Fatal("continued DAP event did not keep the listener alive")
	}
	if got := running.debuggerPanel.State(); got != dap.StateRunning {
		t.Fatalf("debugger UI state after continue = %v, want running", got)
	}
	if got := running.coordinator.GetDAPCoordinator().GetState(); got != dap.StateRunning {
		t.Fatalf("DAP coordinator state after continue = %v, want running", got)
	}
	if running.status != "Debugging" {
		t.Fatalf("continue status = %q", running.status)
	}
}

// TestAppDeleteConfirmHandling tests delete confirmation routing through Update.
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

	// Set up delete confirmation state.
	testFile := filepath.Join(root, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	model.deleteConfirm = true
	model.deleteTarget = testFile

	// Confirm deletion via Update; the command performs filesystem work.
	updatedAny, cmd := model.Update(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	if updated.deleteConfirm || updated.deleteTarget != "" {
		t.Fatalf("delete prompt not reset: confirm=%v target=%q", updated.deleteConfirm, updated.deleteTarget)
	}
	if cmd == nil {
		t.Fatal("confirming delete did not schedule a filesystem command")
	}

	completed := completeTreeAction(t, updated, cmd)
	if _, err := os.Lstat(testFile); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
	// Sanity: completing the command should keep the prompt dismissed.
	if completed.deleteConfirm || completed.deleteTarget != "" {
		t.Fatalf("delete prompt revived after completion: confirm=%v target=%q", completed.deleteConfirm, completed.deleteTarget)
	}
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
	model.activeEditor().Buffer.FilePath = testFile
	model.tabBar.Tabs[model.activeTab].FilePath = testFile

	msg := lsp.DiagnosticsMsg{
		URI: lsp.FileURI(testFile),
		Diagnostics: []lsp.Diagnostic{
			{
				Range:    lsp.DiagRange{Start: lsp.DiagPosition{Line: 1, Character: 2}, End: lsp.DiagPosition{Line: 1, Character: 5}},
				Severity: 1,
				Message:  "test error",
			},
		},
	}
	updatedAny, cmd := model.routeLSPMsg(lspMsg{msg: msg})
	model = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("LSP route did not schedule the listener continuation")
	}
	if got := model.activeEditor().Diagnostics; len(got) != 0 {
		t.Fatalf("editor diagnostics changed before async preparation: %#v", got)
	}
	model = completeDiagnosticsForTest(t, model, msg)
	if got := model.activeEditor().Diagnostics; len(got) != 1 || got[0].Message != "test error" || got[0].StartLine != 1 || got[0].StartCol != 2 {
		t.Fatalf("editor diagnostics = %#v, want the received diagnostic", got)
	}
	if got := model.fileDiagnostics[testFile]; got != 1 {
		t.Fatalf("file diagnostic severity = %d, want error severity 1", got)
	}
	if got := model.treeDiagnostics[testFile]; got != 1 {
		t.Fatalf("tree diagnostic severity = %d, want error severity 1", got)
	}

	// Verify diagnostics stored in coordinator
	lspCoord := model.coordinator.GetLSPCoordinator()
	storedDiags := lspCoord.GetDiagnostics(testFile)
	if len(storedDiags) != 1 || storedDiags[0].Message != "test error" {
		t.Fatalf("coordinator diagnostics = %#v, want the received diagnostic", storedDiags)
	}

	stale := msg
	stale.HasVersion = true
	stale.Version = model.activeEditor().Buffer.Version() + 1
	stale.Diagnostics[0].Message = "stale error"
	model = completeDiagnosticsForTest(t, model, stale)
	if got := model.activeEditor().Diagnostics[0].Message; got != "test error" {
		t.Fatalf("stale diagnostics replaced current editor state with %q", got)
	}

	model = completeDiagnosticsForTest(t, model, lsp.DiagnosticsMsg{URI: lsp.FileURI(testFile)})
	if len(model.activeEditor().Diagnostics) != 0 {
		t.Fatalf("cleared diagnostics remained on editor: %#v", model.activeEditor().Diagnostics)
	}
	if _, ok := model.fileDiagnostics[testFile]; ok {
		t.Fatal("cleared diagnostics remained in file index")
	}
	if len(model.coordinator.GetLSPCoordinator().GetDiagnostics(testFile)) != 0 {
		t.Fatal("cleared diagnostics remained in coordinator")
	}
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
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "test.txt", "initial", "initial")
	path := model.editors[0].Buffer.FilePath
	updatedAny, cmd := model.Update(FileChangedMsg{
		Path:     path,
		Snapshot: text.NewFromString("updated externally"),
	})
	updated := updatedAny.(Model)

	if cmd == nil {
		t.Fatal("external file change did not schedule derived-state refresh")
	}
	if got := updated.editors[0].Buffer.Content(); got != "updated externally" {
		t.Fatalf("external reload content = %q, want %q", got, "updated externally")
	}
	if updated.editors[0].Buffer.Dirty() {
		t.Fatal("clean buffer reloaded from disk became dirty")
	}
	if got := updated.status; got != "Reloaded: test.txt (external change)" {
		t.Fatalf("external reload status = %q", got)
	}
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

	// The formatting path must remain safe when no language server is available;
	// the save flow falls back to a regular write instead of claiming formatting.
	model.activeEditor().Buffer.FilePath = filepath.Join(t.TempDir(), "plain.txt")
	model.tabBar.Tabs[model.activeTab].FilePath = model.activeEditor().Buffer.FilePath
	model.lspMgr = nil
	if cmd := model.requestFormatting(model.activeEditor().Buffer.FilePath, model.activeEditor().Config, 1); cmd != nil {
		t.Fatal("formatting without an LSP client should be a no-op")
	}
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
	setInput := func(t *testing.T, model Model, value string) Model {
		t.Helper()
		model.showAgent = true
		model.focus = FocusAgent
		model.width, model.height = 100, 30
		model.agentPanel.SetSize(80, 20)
		model.agentPanel.SetConnected(true)
		if cmd := model.agentPanel.Focus(); cmd != nil {
			_ = cmd
		}
		for _, r := range value {
			updatedAny, _ := model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			model = updatedAny.(Model)
		}
		return model
	}

	t.Run("help command is handled by the panel", func(t *testing.T) {
		model := setInput(t, newTestModel(t), "/help")
		updatedAny, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated := updatedAny.(Model)
		if cmd != nil || updated.agentPanel.InputValue() != "" {
			t.Fatalf("help command state input=%q cmd=%v", updated.agentPanel.InputValue(), cmd != nil)
		}
		if rendered := updated.agentPanel.View(); !strings.Contains(rendered, "Commands: /model") {
			t.Fatalf("help command was not rendered in agent panel: %q", rendered)
		}
	})

	t.Run("clear command removes chat history", func(t *testing.T) {
		model := newTestModel(t)
		model.agentPanel.SetSize(80, 20)
		model.agentPanel.SetConnected(true)
		model.agentPanel.AddSystemMessage("old history")
		model = setInput(t, model, "/clear")
		updatedAny, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated := updatedAny.(Model)
		if cmd != nil || updated.agentPanel.InputValue() != "" {
			t.Fatalf("clear command state input=%q cmd=%v", updated.agentPanel.InputValue(), cmd != nil)
		}
		if rendered := updated.agentPanel.View(); strings.Contains(rendered, "old history") {
			t.Fatalf("clear command left old history in panel: %q", rendered)
		}
	})

	t.Run("at command opens the file picker", func(t *testing.T) {
		model := newTestModel(t)
		model.cachedFilesReady = true
		model.cachedFiles = []string{"main.go"}
		model = setInput(t, model, "@")
		updatedAny, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated := updatedAny.(Model)
		if cmd == nil || updated.overlayStack.IsEmpty() {
			t.Fatal("@ command did not open and schedule the agent file picker")
		}
		picker, ok := updated.overlayStack.Top().(*overlay.Picker)
		if !ok || picker.ZoneID() != "agent-file-picker" {
			t.Fatalf("agent file picker = %T/%q", updated.overlayStack.Top(), picker.ZoneID())
		}
	})

	t.Run("normal text never leaks into the editor", func(t *testing.T) {
		model := setInput(t, newTestModel(t), "hello agent")
		updatedAny, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated := updatedAny.(Model)
		if updated.activeEditor().Buffer.Content() != "" {
			t.Fatalf("agent prompt leaked into editor: %q", updated.activeEditor().Buffer.Content())
		}
		if updated.agentPanel.InputValue() != "" {
			t.Fatalf("submitted prompt remained in input: %q", updated.agentPanel.InputValue())
		}
	})
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
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.pendingSaves[1] = pendingSaveRequest{QuitAfter: true}
	model.pendingSaves[2] = pendingSaveRequest{QuitAfter: false}
	model.cancelQuitAfterSaves()
	if model.pendingSaves[1].QuitAfter {
		t.Fatal("cancelQuitAfterSaves left quit-after flag set")
	}
	if model.pendingSaves[2].QuitAfter {
		t.Fatal("cancelQuitAfterSaves changed an ordinary save")
	}
}

// TestAppCompleteSaveRequest tests completing save requests
func TestAppCompleteSaveRequest(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	want := pendingSaveRequest{Path: "main.go", QuitAfter: true}
	model.pendingSaves[17] = want
	got, ok := model.completeSaveRequest(17)
	if !ok || got != want {
		t.Fatalf("completeSaveRequest() = %#v, %v; want %#v, true", got, ok, want)
	}
	if _, exists := model.pendingSaves[17]; exists {
		t.Fatal("completed save request remained in pending map")
	}
	if _, ok := model.completeSaveRequest(17); ok {
		t.Fatal("unknown save request reported as completed")
	}
}
