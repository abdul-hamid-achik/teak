package acp

import (
	"errors"
	"testing"

	sdk "github.com/coder/acp-go-sdk"
)

func TestAgentModeChangedMsg_HasModeId(t *testing.T) {
	msg := AgentModeChangedMsg{ModeId: sdk.SessionModeId("auto")}
	if msg.ModeId != "auto" {
		t.Errorf("ModeId = %q, want %q", msg.ModeId, "auto")
	}
}

func TestAgentErrorMsg_HasError(t *testing.T) {
	msg := AgentErrorMsg{Err: nil}
	if msg.Err != nil {
		t.Errorf("Err = %v, want nil", msg.Err)
	}
}

func TestNewManager_InitializesDoneChanAsNil(t *testing.T) {
	mgr := NewManager("/tmp", "echo", []string{"hello"})
	if mgr.done != nil {
		t.Error("done channel should be nil before Start()")
	}
}

func TestManager_StopBeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", []string{"hello"})
	// Stop before Start should not panic
	mgr.Stop()
	if mgr.running {
		t.Error("running should be false after Stop()")
	}
}

func TestManager_IsRunning_BeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	if mgr.IsRunning() {
		t.Error("IsRunning() should be false before Start()")
	}
}

// --- New tests below ---

func TestAgentErrorMsg_WithError(t *testing.T) {
	err := errors.New("something went wrong")
	msg := AgentErrorMsg{Err: err}
	if msg.Err == nil {
		t.Fatal("Err should not be nil")
	}
	if msg.Err.Error() != "something went wrong" {
		t.Errorf("Err.Error() = %q, want 'something went wrong'", msg.Err.Error())
	}
}

func TestAgentModeChangedMsg_EmptyModeId(t *testing.T) {
	msg := AgentModeChangedMsg{ModeId: sdk.SessionModeId("")}
	if msg.ModeId != "" {
		t.Errorf("ModeId = %q, want empty string", msg.ModeId)
	}
}

func TestAgentModelChangedMsg(t *testing.T) {
	msg := AgentModelChangedMsg{ModelId: sdk.ModelId("claude-3")}
	if msg.ModelId != "claude-3" {
		t.Errorf("ModelId = %q, want 'claude-3'", msg.ModelId)
	}
}

func TestAgentStartedMsg(t *testing.T) {
	// Verify zero-value struct works
	msg := AgentStartedMsg{}
	_ = msg
}

func TestAgentStoppedMsg_NilErr(t *testing.T) {
	msg := AgentStoppedMsg{Err: nil}
	if msg.Err != nil {
		t.Errorf("Err = %v, want nil", msg.Err)
	}
}

func TestAgentStoppedMsg_WithErr(t *testing.T) {
	msg := AgentStoppedMsg{Err: errors.New("process killed")}
	if msg.Err == nil {
		t.Fatal("Err should not be nil")
	}
	if msg.Err.Error() != "process killed" {
		t.Errorf("Err.Error() = %q, want 'process killed'", msg.Err.Error())
	}
}

func TestAgentTextMsg(t *testing.T) {
	msg := AgentTextMsg{Text: "hello agent"}
	if msg.Text != "hello agent" {
		t.Errorf("Text = %q, want 'hello agent'", msg.Text)
	}
}

func TestAgentThoughtMsg(t *testing.T) {
	msg := AgentThoughtMsg{Text: "I think therefore I am"}
	if msg.Text != "I think therefore I am" {
		t.Errorf("Text = %q, want 'I think therefore I am'", msg.Text)
	}
}

func TestAgentPromptResponseMsg_Success(t *testing.T) {
	msg := AgentPromptResponseMsg{StopReason: sdk.StopReason("end_turn"), Err: nil}
	if msg.Err != nil {
		t.Errorf("Err = %v, want nil", msg.Err)
	}
	if msg.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want 'end_turn'", msg.StopReason)
	}
}

func TestAgentPromptResponseMsg_Error(t *testing.T) {
	msg := AgentPromptResponseMsg{Err: errors.New("timeout")}
	if msg.Err == nil {
		t.Fatal("Err should not be nil")
	}
}

func TestAgentSessionInfoMsg(t *testing.T) {
	msg := AgentSessionInfoMsg{
		Models: []sdk.ModelInfo{
			{ModelId: "m1", Name: "Model 1"},
			{ModelId: "m2", Name: "Model 2"},
		},
		CurrentModel: sdk.ModelId("m1"),
		Modes: []sdk.SessionMode{
			{Id: "auto", Name: "Auto"},
		},
		CurrentMode: sdk.SessionModeId("auto"),
	}
	if len(msg.Models) != 2 {
		t.Errorf("len(Models) = %d, want 2", len(msg.Models))
	}
	if msg.CurrentModel != "m1" {
		t.Errorf("CurrentModel = %q, want 'm1'", msg.CurrentModel)
	}
	if len(msg.Modes) != 1 {
		t.Errorf("len(Modes) = %d, want 1", len(msg.Modes))
	}
	if msg.CurrentMode != "auto" {
		t.Errorf("CurrentMode = %q, want 'auto'", msg.CurrentMode)
	}
}

func TestNewManager_Fields(t *testing.T) {
	mgr := NewManager("/home/user/project", "claude-agent", []string{"--verbose"})
	if mgr.rootDir != "/home/user/project" {
		t.Errorf("rootDir = %q, want '/home/user/project'", mgr.rootDir)
	}
	if mgr.command != "claude-agent" {
		t.Errorf("command = %q, want 'claude-agent'", mgr.command)
	}
	if len(mgr.args) != 1 || mgr.args[0] != "--verbose" {
		t.Errorf("args = %v, want ['--verbose']", mgr.args)
	}
	if mgr.msgChan == nil {
		t.Error("msgChan should be initialized")
	}
	if mgr.running {
		t.Error("running should be false initially")
	}
	if mgr.conn != nil {
		t.Error("conn should be nil initially")
	}
}

func TestManager_MsgChan(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	ch := mgr.MsgChan()
	if ch == nil {
		t.Error("MsgChan() should not return nil")
	}
}

func TestManager_StartWithInvalidCommand(t *testing.T) {
	mgr := NewManager("/tmp", "nonexistent_command_that_does_not_exist_xyz", nil)
	err := mgr.Start()
	if err == nil {
		t.Fatal("Start() should fail with invalid command")
	}
}

func TestManager_DoubleStop(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	// Double stop should not panic
	mgr.Stop()
	mgr.Stop()
	if mgr.running {
		t.Error("running should be false after Stop()")
	}
}

func TestManager_CancelBeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	// Cancel before Start should not panic
	mgr.Cancel()
}

func TestManager_PromptBeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	cmd := mgr.Prompt("hello", nil)
	if cmd == nil {
		t.Fatal("Prompt() should return a non-nil Cmd even when not running")
	}
	// Execute the cmd and verify it returns an error message
	msg := cmd()
	resp, ok := msg.(AgentPromptResponseMsg)
	if !ok {
		t.Fatalf("expected AgentPromptResponseMsg, got %T", msg)
	}
	if resp.Err == nil {
		t.Error("expected error when agent not running")
	}
}

func TestManager_SetModelBeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	cmd := mgr.SetModel("some-model")
	if cmd != nil {
		t.Error("SetModel() should return nil when not running")
	}
}

func TestManager_SetModeBeforeStart(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	cmd := mgr.SetMode("some-mode")
	if cmd != nil {
		t.Error("SetMode() should return nil when not running")
	}
}
