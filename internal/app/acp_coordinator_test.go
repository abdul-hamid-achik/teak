package app

import (
	"testing"

	"teak/internal/acp"
)

// TestACPCoordinatorCreation tests that the coordinator can be created
func TestACPCoordinatorCreation(t *testing.T) {
	coord := NewACPCoordinator(nil)
	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}

	if coord.chatHistory == nil {
		t.Error("expected chatHistory slice to be initialized")
	}
}

// TestACPCoordinatorHandleAgentText tests agent text message handling
func TestACPCoordinatorHandleAgentText(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentTextMsg{
		Text: "test message",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentThought tests agent thought message handling
func TestACPCoordinatorHandleAgentThought(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentThoughtMsg{
		Text: "thinking...",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentToolCall tests tool call message handling
func TestACPCoordinatorHandleAgentToolCall(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentToolCallMsg{
		Title: "test_tool",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentToolCallUpdate tests tool call update message handling
func TestACPCoordinatorHandleAgentToolCallUpdate(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentToolCallUpdateMsg{
		ID: "123",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentPlan tests agent plan message handling
func TestACPCoordinatorHandleAgentPlan(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentPlanMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentWriteFile tests write file message handling
func TestACPCoordinatorHandleAgentWriteFile(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentWriteFileMsg{
		Path:    "/test.go",
		Content: "test",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentPermissionRequest tests permission request handling
func TestACPCoordinatorHandleAgentPermissionRequest(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentPermissionRequestMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentSessionInfo tests session info message handling
func TestACPCoordinatorHandleAgentSessionInfo(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentSessionInfoMsg{
		SessionID:    "session-456",
		CurrentModel: "model-1",
		CurrentMode:  "mode-1",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
	if coord.sessionID != "session-456" {
		t.Fatalf("expected sessionID to be updated, got %q", coord.sessionID)
	}
	if coord.modelID != "model-1" {
		t.Fatalf("expected modelID to be updated, got %q", coord.modelID)
	}
	if coord.mode != "mode-1" {
		t.Fatalf("expected mode to be updated, got %q", coord.mode)
	}
}

func TestACPCoordinatorHandleWrappedAgentSessionInfo(t *testing.T) {
	coord := NewACPCoordinator(nil)

	cmds := coord.HandleMessage(acpMsg{msg: acp.AgentSessionInfoMsg{
		SessionID:    "session-789",
		CurrentModel: "model-2",
		CurrentMode:  "mode-2",
	}})
	if cmds != nil {
		t.Fatalf("expected wrapped ACP message to update state without forwarding commands, got %d", len(cmds))
	}

	sid, mid, mode := coord.GetSessionInfo()
	if sid != "session-789" || mid != "model-2" || mode != "mode-2" {
		t.Fatalf("unexpected wrapped session info: sid=%q mid=%q mode=%q", sid, mid, mode)
	}
}

// TestACPCoordinatorHandleAgentModelChanged tests model changed message handling
func TestACPCoordinatorHandleAgentModelChanged(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentModelChangedMsg{
		ModelId: "test-model",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentModeChanged tests mode changed message handling
func TestACPCoordinatorHandleAgentModeChanged(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentModeChangedMsg{
		ModeId: "test-mode",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentError tests error message handling
func TestACPCoordinatorHandleAgentError(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentErrorMsg{
		Err: nil,
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentStarted tests started message handling
func TestACPCoordinatorHandleAgentStarted(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentStartedMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentStopped tests stopped message handling
func TestACPCoordinatorHandleAgentStopped(t *testing.T) {
	coord := NewACPCoordinator(nil)
	coord.SetRunning(true)
	coord.SetSessionInfo("session-123", "model-1", "mode-1")

	msg := acp.AgentStoppedMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
	if coord.IsRunning() {
		t.Fatal("expected running to be false after stop")
	}
	sid, mid, mode := coord.GetSessionInfo()
	if sid != "" || mid != "" || mode != "" {
		t.Fatalf("expected session info to be cleared after stop, got sid=%q mid=%q mode=%q", sid, mid, mode)
	}
}

// TestACPCoordinatorHandleFileReadRequest tests file read request handling
func TestACPCoordinatorHandleFileReadRequest(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.FileReadRequestMsg{
		Path: "/test.go",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorHandleAgentPromptResponse tests prompt response handling
func TestACPCoordinatorHandleAgentPromptResponse(t *testing.T) {
	coord := NewACPCoordinator(nil)

	msg := acp.AgentPromptResponseMsg{
		StopReason: "stop",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestACPCoordinatorAddToHistory tests adding to chat history
func TestACPCoordinatorAddToHistory(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.AddToHistory("user", "test message")

	if len(coord.chatHistory) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(coord.chatHistory))
	}
}

// TestACPCoordinatorClearHistory tests clearing chat history
func TestACPCoordinatorClearHistory(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.AddToHistory("user", "test")
	coord.ClearHistory()

	if len(coord.chatHistory) != 0 {
		t.Errorf("expected 0 history entries after clear, got %d", len(coord.chatHistory))
	}
}

// TestACPCoordinatorGetHistory tests getting chat history
func TestACPCoordinatorGetHistory(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.AddToHistory("user", "test1")
	coord.AddToHistory("agent", "test2")

	history := coord.GetHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(history))
	}
}

// TestACPCoordinatorSetSessionInfo tests setting session info
func TestACPCoordinatorSetSessionInfo(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.SetSessionInfo("session-123", "model-1", "mode-1")

	if coord.sessionID != "session-123" {
		t.Errorf("expected sessionID 'session-123', got '%s'", coord.sessionID)
	}
}

// TestACPCoordinatorGetSessionInfo tests getting session info
func TestACPCoordinatorGetSessionInfo(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.SetSessionInfo("session-123", "model-1", "mode-1")

	sid, mid, mode := coord.GetSessionInfo()
	if sid != "session-123" {
		t.Errorf("expected sessionID 'session-123', got '%s'", sid)
	}
	if mid != "model-1" {
		t.Errorf("expected modelID 'model-1', got '%s'", mid)
	}
	if mode != "mode-1" {
		t.Errorf("expected mode 'mode-1', got '%s'", mode)
	}
}

// TestACPCoordinatorIsRunning tests is running check
func TestACPCoordinatorIsRunning(t *testing.T) {
	coord := NewACPCoordinator(nil)

	if coord.IsRunning() {
		t.Error("expected not running initially")
	}

	coord.running = true
	if !coord.IsRunning() {
		t.Error("expected running")
	}
}

// TestACPCoordinatorSetRunning tests set running
func TestACPCoordinatorSetRunning(t *testing.T) {
	coord := NewACPCoordinator(nil)

	coord.SetRunning(true)
	if !coord.running {
		t.Error("expected running to be true")
	}

	coord.SetRunning(false)
	if coord.running {
		t.Error("expected running to be false")
	}
}
