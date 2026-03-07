package agent

import (
	"testing"
	"time"

	"teak/internal/ui"
)

// TestAgentModelCreation tests New function
func TestAgentModelCreation(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.toolCallMap == nil {
		t.Error("Expected toolCallMap to be initialized")
	}
	if model.alwaysAllow == nil {
		t.Error("Expected alwaysAllow to be initialized")
	}
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true")
	}
	if model.state != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.state)
	}
	if model.loading {
		t.Error("Expected loading to be false")
	}
	if model.connected {
		t.Error("Expected connected to be false")
	}
}

// TestAgentIsLoading tests IsLoading method
func TestAgentIsLoading(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.IsLoading() {
		t.Error("Expected IsLoading to be false initially")
	}

	model.loading = true
	if !model.IsLoading() {
		t.Error("Expected IsLoading to be true")
	}
}

// TestAgentSetSize tests SetSize method
func TestAgentSetSize(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetSize(100, 30)

	if model.width != 100 {
		t.Errorf("Expected width 100, got %d", model.width)
	}
	if model.height != 30 {
		t.Errorf("Expected height 30, got %d", model.height)
	}
}

// TestAgentSetSizeWithSmallWidth tests SetSize with small width
func TestAgentSetSizeWithSmallWidth(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetSize(1, 30)

	if model.width != 1 {
		t.Errorf("Expected width 1, got %d", model.width)
	}
}

// TestAgentSetConnected tests SetConnected method
func TestAgentSetConnected(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetConnected(true)
	if !model.connected {
		t.Error("Expected connected to be true")
	}
	if model.State() != AgentIdle {
		t.Errorf("Expected state AgentIdle, got %v", model.State())
	}

	model.SetConnected(false)
	if model.connected {
		t.Error("Expected connected to be false")
	}
	if model.State() != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.State())
	}
}

// TestAgentStateMethod tests State method
func TestAgentStateMethod(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.State() != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.State())
	}

	model.state = AgentThinking
	if model.State() != AgentThinking {
		t.Errorf("Expected state AgentThinking, got %v", model.State())
	}
}

// TestAgentHasPermissionPending tests HasPermissionPending method
func TestAgentHasPermissionPending(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.HasPermissionPending() {
		t.Error("Expected HasPermissionPending to be false")
	}

	model.permission = &PermissionPrompt{}
	if !model.HasPermissionPending() {
		t.Error("Expected HasPermissionPending to be true")
	}
}

// TestAgentHasPendingWrite tests HasPendingWrite method
func TestAgentHasPendingWrite(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.HasPendingWrite() {
		t.Error("Expected HasPendingWrite to be false")
	}
}

// TestAgentInputValue tests InputValue method
func TestAgentInputValue(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	value := model.InputValue()
	if value != "" {
		t.Errorf("Expected empty input value, got %q", value)
	}
}

// TestAgentClearInput tests ClearInput method
func TestAgentClearInput(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.input.SetValue("test")
	model.ClearInput()

	value := model.InputValue()
	if value != "" {
		t.Errorf("Expected empty input after clear, got %q", value)
	}
}

// TestAgentFocus tests Focus method
func TestAgentFocus(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	cmd := model.Focus()
	if cmd == nil {
		t.Error("Expected Focus to return a command")
	}
}

// TestAgentBlur tests Blur method
func TestAgentBlur(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Should not crash
	model.Blur()
}

// TestAgentTaggedFiles tests TaggedFiles method
func TestAgentTaggedFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	files := model.TaggedFiles()
	// taggedFiles is nil initially, which is fine
	if len(files) != 0 {
		t.Errorf("Expected 0 tagged files, got %d", len(files))
	}
}

// TestAgentAddTaggedFile tests AddTaggedFile method
func TestAgentAddTaggedFile(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file, got %d", len(files))
	}
	if files[0].Path != "/test.go" {
		t.Errorf("Expected Path '/test.go', got %q", files[0].Path)
	}
	if files[0].Name != "test.go" {
		t.Errorf("Expected Name 'test.go', got %q", files[0].Name)
	}
}

// TestAgentAddTaggedFilePreventsDuplicates tests AddTaggedFile prevents duplicates
func TestAgentAddTaggedFilePreventsDuplicates(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.AddTaggedFile("/test.go")
	model.AddTaggedFile("/test.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no duplicates), got %d", len(files))
	}
}

// TestAgentAddTaggedFileExtractsFilename tests AddTaggedFile extracts filename
func TestAgentAddTaggedFileExtractsFilename(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/very/long/path/to/file.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(files))
	}
	if files[0].Name != "file.go" {
		t.Errorf("Expected Name 'file.go', got %q", files[0].Name)
	}
}

// TestAgentRemoveTaggedFile tests RemoveTaggedFile method
func TestAgentRemoveTaggedFile(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.AddTaggedFile("/test3.go")

	model.RemoveTaggedFile(1) // Remove middle one

	files := model.TaggedFiles()
	if len(files) != 2 {
		t.Errorf("Expected 2 tagged files, got %d", len(files))
	}
}

// TestAgentRemoveTaggedFileWithInvalidIndex tests RemoveTaggedFile with invalid index
func TestAgentRemoveTaggedFileWithInvalidIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(100) // Out of bounds

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no change), got %d", len(files))
	}
}

// TestAgentRemoveTaggedFileWithNegativeIndex tests RemoveTaggedFile with negative index
func TestAgentRemoveTaggedFileWithNegativeIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(-1) // Negative index

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no change), got %d", len(files))
	}
}

// TestAgentClearTaggedFiles tests ClearTaggedFiles method
func TestAgentClearTaggedFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.ClearTaggedFiles()

	files := model.TaggedFiles()
	if len(files) != 0 {
		t.Errorf("Expected 0 tagged files after clear, got %d", len(files))
	}
}

// TestAgentCurrentModel tests CurrentModel method
func TestAgentCurrentModel(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modelID := model.CurrentModel()
	// Initially should be empty
	if modelID != "" {
		t.Errorf("Expected empty model ID, got %q", modelID)
	}
}

// TestAgentAvailableModels tests AvailableModels method
func TestAgentAvailableModels(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	models := model.AvailableModels()
	// models is nil initially, which is fine
	if len(models) != 0 {
		t.Errorf("Expected 0 models initially, got %d", len(models))
	}
}

// TestAgentAvailableModes tests AvailableModes method
func TestAgentAvailableModes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modes := model.AvailableModes()
	// modes is nil initially, which is fine
	if len(modes) != 0 {
		t.Errorf("Expected 0 modes initially, got %d", len(modes))
	}
}

// TestAgentCurrentMode tests CurrentMode method
func TestAgentCurrentMode(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modeID := model.CurrentMode()
	// Initially should be empty
	if modeID != "" {
		t.Errorf("Expected empty mode ID, got %q", modeID)
	}
}

// TestAgentAddSystemMessage tests AddSystemMessage method
func TestAgentAddSystemMessage(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddSystemMessage("test system message")

	if len(model.messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(model.messages))
	}
	if model.messages[0].Role != RoleSystem {
		t.Errorf("Expected Role RoleSystem, got %d", model.messages[0].Role)
	}
	if model.messages[0].Content != "test system message" {
		t.Errorf("Expected Content 'test system message', got %q", model.messages[0].Content)
	}
}

// TestAgentAddMultipleSystemMessages tests adding multiple system messages
func TestAgentAddMultipleSystemMessages(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddSystemMessage("message 1")
	model.AddSystemMessage("message 2")
	model.AddSystemMessage("message 3")

	if len(model.messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(model.messages))
	}
}

// TestAgentClearHistory tests ClearHistory method
func TestAgentClearHistory(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add some state
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "test"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "test"})
	model.toolCallMap["test"] = &ToolCallState{}
	model.scrollY = 10
	model.autoScroll = false

	model.ClearHistory()

	if len(model.messages) != 0 {
		t.Errorf("Expected 0 messages after clear, got %d", len(model.messages))
	}
	if len(model.streamBlocks) != 0 {
		t.Errorf("Expected 0 stream blocks after clear, got %d", len(model.streamBlocks))
	}
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after clear, got %d", len(model.toolCallMap))
	}
	if model.scrollY != 0 {
		t.Errorf("Expected scrollY 0 after clear, got %d", model.scrollY)
	}
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true after clear")
	}
}

// TestAgentClearHistoryMultipleTimes tests ClearHistory multiple times
func TestAgentClearHistoryMultipleTimes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.ClearHistory()
	model.ClearHistory()
	model.ClearHistory()

	// Should not crash
}

// TestAgentInitialState tests initial state
func TestAgentInitialState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.state != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.state)
	}
	if model.loading {
		t.Error("Expected loading to be false")
	}
	if model.connected {
		t.Error("Expected connected to be false")
	}
	if model.scrollY != 0 {
		t.Errorf("Expected scrollY 0, got %d", model.scrollY)
	}
	if model.maxScroll != 0 {
		t.Errorf("Expected maxScroll 0, got %d", model.maxScroll)
	}
}

// TestAgentScrollManagement tests scroll management
func TestAgentScrollManagement(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.scrollY != 0 {
		t.Errorf("Expected initial scrollY 0, got %d", model.scrollY)
	}

	model.scrollY = 10
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
}

// TestAgentMaxScrollManagement tests maxScroll management
func TestAgentMaxScrollManagement(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.maxScroll != 0 {
		t.Errorf("Expected initial maxScroll 0, got %d", model.maxScroll)
	}

	model.maxScroll = 50
	if model.maxScroll != 50 {
		t.Errorf("Expected maxScroll 50, got %d", model.maxScroll)
	}
}

// TestAgentLoadingToggle tests loading toggle
func TestAgentLoadingToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.loading {
		t.Error("Expected loading to be false initially")
	}

	model.loading = true
	if !model.loading {
		t.Error("Expected loading to be true")
	}

	model.loading = false
	if model.loading {
		t.Error("Expected loading to be false")
	}
}

// TestAgentConnectedToggle tests connected toggle
func TestAgentConnectedToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.connected {
		t.Error("Expected connected to be false initially")
	}

	model.connected = true
	if !model.connected {
		t.Error("Expected connected to be true")
	}

	model.connected = false
	if model.connected {
		t.Error("Expected connected to be false")
	}
}

// TestAgentAutoScrollToggle tests autoScroll toggle
func TestAgentAutoScrollToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if !model.autoScroll {
		t.Error("Expected autoScroll to be true initially")
	}

	model.autoScroll = false
	if model.autoScroll {
		t.Error("Expected autoScroll to be false")
	}

	model.autoScroll = true
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true")
	}
}

// TestAgentStateTransitions tests state transitions
func TestAgentStateTransitions(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Initial state
	if model.State() != AgentDisconnected {
		t.Errorf("Expected AgentDisconnected, got %v", model.State())
	}

	// Transition to Idle
	model.state = AgentIdle
	if model.State() != AgentIdle {
		t.Errorf("Expected AgentIdle, got %v", model.State())
	}

	// Transition to Thinking
	model.state = AgentThinking
	if model.State() != AgentThinking {
		t.Errorf("Expected AgentThinking, got %v", model.State())
	}

	// Transition to Permission
	model.state = AgentPermission
	if model.State() != AgentPermission {
		t.Errorf("Expected AgentPermission, got %v", model.State())
	}
}

// TestAgentAllStates tests all agent states
func TestAgentAllStates(t *testing.T) {
	states := []AgentState{AgentDisconnected, AgentIdle, AgentThinking, AgentPermission}

	for i, state := range states {
		_ = state
		// Just verify states exist and are different
		if i > 0 && state == states[i-1] {
			t.Errorf("Expected state %d to be different from state %d", i, i-1)
		}
	}
}

// TestAgentStateValues tests AgentState values
func TestAgentStateValues(t *testing.T) {
	if AgentDisconnected != 0 {
		t.Errorf("Expected AgentDisconnected 0, got %d", AgentDisconnected)
	}
	if AgentIdle != 1 {
		t.Errorf("Expected AgentIdle 1, got %d", AgentIdle)
	}
	if AgentThinking != 2 {
		t.Errorf("Expected AgentThinking 2, got %d", AgentThinking)
	}
	if AgentPermission != 3 {
		t.Errorf("Expected AgentPermission 3, got %d", AgentPermission)
	}
}

// TestAgentChatRoleValues tests ChatRole values
func TestAgentChatRoleValues(t *testing.T) {
	if RoleUser != 0 {
		t.Errorf("Expected RoleUser 0, got %d", RoleUser)
	}
	if RoleAgent != 1 {
		t.Errorf("Expected RoleAgent 1, got %d", RoleAgent)
	}
	if RoleSystem != 2 {
		t.Errorf("Expected RoleSystem 2, got %d", RoleSystem)
	}
}

// TestAgentStreamBlockKindValues tests StreamBlockKind values
func TestAgentStreamBlockKindValues(t *testing.T) {
	if BlockText != 0 {
		t.Errorf("Expected BlockText 0, got %d", BlockText)
	}
	if BlockThought != 1 {
		t.Errorf("Expected BlockThought 1, got %d", BlockThought)
	}
	if BlockToolCall != 2 {
		t.Errorf("Expected BlockToolCall 2, got %d", BlockToolCall)
	}
}

// TestAgentMessagesSlice tests messages slice operations
func TestAgentMessagesSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add messages
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "msg1"})
	model.messages = append(model.messages, ChatMessage{Role: RoleAgent, Content: "msg2"})

	if len(model.messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(model.messages))
	}

	// Access message
	if model.messages[0].Content != "msg1" {
		t.Errorf("Expected messages[0].Content = 'msg1', got %q", model.messages[0].Content)
	}
}

// TestAgentStreamBlocksSlice tests streamBlocks slice operations
func TestAgentStreamBlocksSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add stream blocks
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "block1"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockThought, Content: "block2"})

	if len(model.streamBlocks) != 2 {
		t.Errorf("Expected 2 stream blocks, got %d", len(model.streamBlocks))
	}

	// Access stream block
	if model.streamBlocks[0].Content != "block1" {
		t.Errorf("Expected streamBlocks[0].Content = 'block1', got %q", model.streamBlocks[0].Content)
	}
}

// TestAgentToolCallMapOperations tests toolCallMap operations
func TestAgentToolCallMapOperations(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add tool call
	model.toolCallMap["test-id"] = &ToolCallState{Title: "Test Tool"}
	if len(model.toolCallMap) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(model.toolCallMap))
	}

	// Access tool call
	if model.toolCallMap["test-id"].Title != "Test Tool" {
		t.Errorf("Expected toolCallMap['test-id'].Title = 'Test Tool', got %q", model.toolCallMap["test-id"].Title)
	}

	// Remove tool call
	delete(model.toolCallMap, "test-id")
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after delete, got %d", len(model.toolCallMap))
	}
}

// TestAgentTaggedFilesSlice tests taggedFiles slice operations
func TestAgentTaggedFilesSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add tagged files
	model.taggedFiles = append(model.taggedFiles, TaggedFile{Path: "/test1.go", Name: "test1.go"})
	model.taggedFiles = append(model.taggedFiles, TaggedFile{Path: "/test2.go", Name: "test2.go"})

	if len(model.taggedFiles) != 2 {
		t.Errorf("Expected 2 tagged files, got %d", len(model.taggedFiles))
	}

	// Access tagged file
	if model.taggedFiles[0].Name != "test1.go" {
		t.Errorf("Expected taggedFiles[0].Name = 'test1.go', got %q", model.taggedFiles[0].Name)
	}
}

// TestAgentAlwaysAllowMapOperations tests alwaysAllow map operations
func TestAgentAlwaysAllowMapOperations(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add entry
	model.alwaysAllow["test"] = true
	if !model.alwaysAllow["test"] {
		t.Error("Expected alwaysAllow['test'] to be true")
	}

	// Remove entry
	delete(model.alwaysAllow, "test")
	if _, ok := model.alwaysAllow["test"]; ok {
		t.Error("Expected alwaysAllow['test'] to be deleted")
	}
}

// TestAgentSpinnerInitialized tests spinner initialization
func TestAgentSpinnerInitialized(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Spinner should be initialized
	_ = model.spinner
}

// TestAgentInputInitialized tests input initialization
func TestAgentInputInitialized(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Input should be initialized
	_ = model.input
}

// TestAgentLastChatLineCount tests lastChatLineCount field
func TestAgentLastChatLineCount(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.lastChatLineCount != 0 {
		t.Errorf("Expected lastChatLineCount 0, got %d", model.lastChatLineCount)
	}

	model.lastChatLineCount = 50
	if model.lastChatLineCount != 50 {
		t.Errorf("Expected lastChatLineCount 50, got %d", model.lastChatLineCount)
	}
}

// TestAgentSpinFrame tests spinFrame field
func TestAgentSpinFrame(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.spinFrame != 0 {
		t.Errorf("Expected spinFrame 0, got %d", model.spinFrame)
	}

	model.spinFrame = 5
	if model.spinFrame != 5 {
		t.Errorf("Expected spinFrame 5, got %d", model.spinFrame)
	}
}

// TestAgentLastEscTime tests lastEscTime field
func TestAgentLastEscTime(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if !model.lastEscTime.IsZero() {
		t.Error("Expected lastEscTime to be zero")
	}

	model.lastEscTime = time.Now()
	if model.lastEscTime.IsZero() {
		t.Error("Expected lastEscTime to be set")
	}
}

// TestAgentWidthHeight tests width and height fields
func TestAgentWidthHeight(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.width != 0 {
		t.Errorf("Expected width 0, got %d", model.width)
	}
	if model.height != 0 {
		t.Errorf("Expected height 0, got %d", model.height)
	}

	model.width = 100
	model.height = 30
	if model.width != 100 {
		t.Errorf("Expected width 100, got %d", model.width)
	}
	if model.height != 30 {
		t.Errorf("Expected height 30, got %d", model.height)
	}
}

// TestAgentScrollYMaxScroll tests scrollY and maxScroll relationship
func TestAgentScrollYMaxScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.maxScroll = 50
	model.scrollY = 100

	// scrollY can exceed maxScroll (will be clamped in rendering)
	if model.scrollY != 100 {
		t.Errorf("Expected scrollY 100, got %d", model.scrollY)
	}
}

// TestAgentMessagesWithDifferentRoles tests messages with different roles
func TestAgentMessagesWithDifferentRoles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "user"})
	model.messages = append(model.messages, ChatMessage{Role: RoleAgent, Content: "agent"})
	model.messages = append(model.messages, ChatMessage{Role: RoleSystem, Content: "system"})

	if len(model.messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(model.messages))
	}

	// Verify roles
	expectedRoles := []ChatRole{RoleUser, RoleAgent, RoleSystem}
	for i, expected := range expectedRoles {
		if model.messages[i].Role != expected {
			t.Errorf("Expected message %d role %d, got %d", i, expected, model.messages[i].Role)
		}
	}
}

// TestAgentStreamBlocksWithDifferentKinds tests streamBlocks with different kinds
func TestAgentStreamBlocksWithDifferentKinds(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "text"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockThought, Content: "thought"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockToolCall, Content: "tool"})

	if len(model.streamBlocks) != 3 {
		t.Errorf("Expected 3 stream blocks, got %d", len(model.streamBlocks))
	}

	// Verify kinds
	expectedKinds := []StreamBlockKind{BlockText, BlockThought, BlockToolCall}
	for i, expected := range expectedKinds {
		if model.streamBlocks[i].Kind != expected {
			t.Errorf("Expected stream block %d kind %d, got %d", i, expected, model.streamBlocks[i].Kind)
		}
	}
}

// TestAgentTaggedFilesWithMultipleFiles tests taggedFiles with multiple files
func TestAgentTaggedFilesWithMultipleFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	files := []string{"/test1.go", "/test2.go", "/test3.go"}
	for _, f := range files {
		model.AddTaggedFile(f)
	}

	taggedFiles := model.TaggedFiles()
	if len(taggedFiles) != 3 {
		t.Errorf("Expected 3 tagged files, got %d", len(taggedFiles))
	}

	// Verify all files are present
	for i, expected := range files {
		if taggedFiles[i].Path != expected {
			t.Errorf("Expected tagged file %d path %q, got %q", i, expected, taggedFiles[i].Path)
		}
	}
}

// TestAgentToolCallMapWithMultipleEntries tests toolCallMap with multiple entries
func TestAgentToolCallMapWithMultipleEntries(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add multiple tool calls
	model.toolCallMap["id1"] = &ToolCallState{Title: "Tool 1"}
	model.toolCallMap["id2"] = &ToolCallState{Title: "Tool 2"}
	model.toolCallMap["id3"] = &ToolCallState{Title: "Tool 3"}

	if len(model.toolCallMap) != 3 {
		t.Errorf("Expected 3 tool calls, got %d", len(model.toolCallMap))
	}
}

// TestAgentAlwaysAllowWithMultipleEntries tests alwaysAllow with multiple entries
func TestAgentAlwaysAllowWithMultipleEntries(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add multiple entries
	model.alwaysAllow["tool1"] = true
	model.alwaysAllow["tool2"] = true
	model.alwaysAllow["tool3"] = true

	if len(model.alwaysAllow) != 3 {
		t.Errorf("Expected 3 alwaysAllow entries, got %d", len(model.alwaysAllow))
	}
}

// TestAgentClearHistoryPreservesToolCallMapInitialization tests ClearHistory preserves toolCallMap initialization
func TestAgentClearHistoryPreservesToolCallMapInitialization(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Clear history
	model.ClearHistory()

	// toolCallMap should still be initialized (not nil)
	if model.toolCallMap == nil {
		t.Error("Expected toolCallMap to be initialized after ClearHistory")
	}
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after ClearHistory, got %d", len(model.toolCallMap))
	}
}

// TestAgentAddTaggedFileWithEmptyPath tests AddTaggedFile with empty path
func TestAgentAddTaggedFileWithEmptyPath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(files))
	}
	if files[0].Path != "" {
		t.Errorf("Expected empty Path, got %q", files[0].Path)
	}
}

// TestAgentRemoveTaggedFilePreservesOtherFiles tests RemoveTaggedFile preserves other files
func TestAgentRemoveTaggedFilePreservesOtherFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.AddTaggedFile("/test3.go")

	model.RemoveTaggedFile(1) // Remove middle one

	files := model.TaggedFiles()
	if len(files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(files))
	}
	if files[0].Path != "/test1.go" {
		t.Errorf("Expected first file '/test1.go', got %q", files[0].Path)
	}
	if files[1].Path != "/test3.go" {
		t.Errorf("Expected second file '/test3.go', got %q", files[1].Path)
	}
}

// TestAgentSetConnectedPreservesOtherState tests SetConnected preserves other state
func TestAgentSetConnectedPreservesOtherState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Set some state
	model.loading = true
	model.scrollY = 10
	model.autoScroll = false

	// Change connected
	model.SetConnected(true)

	// Other state should be preserved
	if !model.loading {
		t.Error("Expected loading to be preserved")
	}
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
	if model.autoScroll {
		t.Error("Expected autoScroll to be preserved")
	}
}

// TestAgentAddSystemMessagePreservesOtherMessages tests AddSystemMessage preserves other messages
func TestAgentAddSystemMessagePreservesOtherMessages(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add user message
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "user"})

	// Add system message
	model.AddSystemMessage("system")

	if len(model.messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(model.messages))
	}
	if model.messages[0].Role != RoleUser {
		t.Error("Expected first message to be preserved")
	}
}

// TestAgentClearInputPreservesOtherState tests ClearInput preserves other state
func TestAgentClearInputPreservesOtherState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Set some state
	model.loading = true
	model.scrollY = 10
	model.connected = true

	// Clear input
	model.ClearInput()

	// Other state should be preserved
	if !model.loading {
		t.Error("Expected loading to be preserved")
	}
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
	if !model.connected {
		t.Error("Expected connected to be preserved")
	}
}

// TestAgentFieldAccess tests all Model fields are accessible
func TestAgentFieldAccess(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Access all fields
	_ = model.width
	_ = model.height
	_ = model.theme
	_ = model.messages
	_ = model.streamBlocks
	_ = model.toolCallMap
	_ = model.input
	_ = model.scrollY
	_ = model.maxScroll
	_ = model.loading
	_ = model.connected
	_ = model.state
	_ = model.permission
	_ = model.alwaysAllow
	_ = model.pendingWrite
	_ = model.spinner
	_ = model.spinFrame
	_ = model.lastEscTime
	_ = model.autoScroll
	_ = model.models
	_ = model.currentModel
	_ = model.modes
	_ = model.currentMode
	_ = model.taggedFiles
	_ = model.lastChatLineCount
}

// TestAgentAllTypesExist tests that all agent types exist
func TestAgentAllTypesExist(t *testing.T) {
	// Just verify we can create instances of all types
	_ = ChatMessage{}
	_ = StreamBlock{}
	_ = ToolCallState{}
	_ = PermissionPrompt{}
	_ = TaggedFile{}
	_ = Model{}
}

// TestAgentAllConstantsExist tests that all agent constants exist
func TestAgentAllConstantsExist(t *testing.T) {
	// Just verify we can use all constants
	_ = RoleUser
	_ = RoleAgent
	_ = RoleSystem
	_ = BlockText
	_ = BlockThought
	_ = BlockToolCall
	_ = AgentDisconnected
	_ = AgentIdle
	_ = AgentThinking
	_ = AgentPermission
}

// TestAgentMaxToolOutputLines tests maxToolOutputLines constant
func TestAgentMaxToolOutputLines(t *testing.T) {
	if maxToolOutputLines != 100 {
		t.Errorf("Expected maxToolOutputLines 100, got %d", maxToolOutputLines)
	}
}

// TestAgentTypesCompile tests that all agent types compile
func TestAgentTypesCompile(t *testing.T) {
	// This test just verifies all types compile correctly
	var _ ChatMessage
	var _ StreamBlock
	var _ ToolCallState
	var _ PermissionPrompt
	var _ TaggedFile
	var _ Model
	var _ ChatRole
	var _ StreamBlockKind
	var _ AgentState
}

// TestAgentConstantsCompile tests that all agent constants compile
func TestAgentConstantsCompile(t *testing.T) {
	// This test just verifies all constants compile correctly
	var _ = RoleUser
	var _ = BlockText
	var _ = AgentDisconnected
	var _ = maxToolOutputLines
}

// TestAgentMethodsExist tests that all Model methods exist
func TestAgentMethodsExist(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Verify methods exist and are callable
	_ = model.IsLoading()
	model.SetSize(100, 30)
	model.SetConnected(true)
	_ = model.State()
	_ = model.HasPermissionPending()
	_ = model.HasPendingWrite()
	_ = model.PendingWrite()
	model.AcceptWrite()
	model.RejectWrite()
	_ = model.InputValue()
	model.ClearInput()
	_ = model.Focus()
	model.Blur()
	_ = model.TaggedFiles()
	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(0)
	model.ClearTaggedFiles()
	_ = model.CurrentModel()
	_ = model.AvailableModels()
	_ = model.AvailableModes()
	_ = model.CurrentMode()
	model.AddSystemMessage("test")
	model.ClearHistory()
}

// TestAgentAllFieldsHaveZeroValues tests that all fields have proper zero values
func TestAgentAllFieldsHaveZeroValues(t *testing.T) {
	var model Model

	if model.width != 0 {
		t.Errorf("Expected zero width 0, got %d", model.width)
	}
	if model.height != 0 {
		t.Errorf("Expected zero height 0, got %d", model.height)
	}
	if model.messages != nil {
		t.Error("Expected zero messages to be nil")
	}
	if model.streamBlocks != nil {
		t.Error("Expected zero streamBlocks to be nil")
	}
	if model.toolCallMap != nil {
		t.Error("Expected zero toolCallMap to be nil")
	}
	if model.scrollY != 0 {
		t.Errorf("Expected zero scrollY 0, got %d", model.scrollY)
	}
	if model.maxScroll != 0 {
		t.Errorf("Expected zero maxScroll 0, got %d", model.maxScroll)
	}
	if model.loading {
		t.Error("Expected zero loading to be false")
	}
	if model.connected {
		t.Error("Expected zero connected to be false")
	}
	if model.state != 0 {
		t.Errorf("Expected zero state 0, got %d", model.state)
	}
	if model.permission != nil {
		t.Error("Expected zero permission to be nil")
	}
	if model.alwaysAllow != nil {
		t.Error("Expected zero alwaysAllow to be nil")
	}
	if model.pendingWrite != nil {
		t.Error("Expected zero pendingWrite to be nil")
	}
	if model.spinFrame != 0 {
		t.Errorf("Expected zero spinFrame 0, got %d", model.spinFrame)
	}
	if !model.lastEscTime.IsZero() {
		t.Error("Expected zero lastEscTime to be zero")
	}
	if model.autoScroll {
		t.Error("Expected zero autoScroll to be false")
	}
	if model.models != nil {
		t.Error("Expected zero models to be nil")
	}
	if model.taggedFiles != nil {
		t.Error("Expected zero taggedFiles to be nil")
	}
	if model.lastChatLineCount != 0 {
		t.Errorf("Expected zero lastChatLineCount 0, got %d", model.lastChatLineCount)
	}
}
