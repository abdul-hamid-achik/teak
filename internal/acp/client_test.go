package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
		SessionID: sdk.SessionId("session-1"),
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
	if msg.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want 'session-1'", msg.SessionID)
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

func TestManagerEmitDoesNotBlockOnUnreadChannel(t *testing.T) {
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.msgChan = make(chan tea.Msg)
	done := make(chan struct{})
	go func() {
		mgr.emit(AgentStartedMsg{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ACP UI notification blocked on an unread channel")
	}
}

func TestManagerStopReturnsBeforeAStuckAgentExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestACPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_ACP_SHUTDOWN_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.cmd = cmd
	mgr.done = done
	mgr.running = true
	go reapACPProcess(cmd, done, mgr)

	started := time.Now()
	mgr.Stop()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Stop() blocked for %s", elapsed)
	}
	if !waitACPDone(done, 3*time.Second) {
		t.Fatal("agent was not killed and reaped")
	}
}

func TestManagerWaitForShutdownReapsStoppedAgent(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestACPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_ACP_SHUTDOWN_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.cmd = cmd
	mgr.done = done
	mgr.running = true
	go reapACPProcess(cmd, done, mgr)

	mgr.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !mgr.WaitForShutdown(ctx) {
		t.Fatal("WaitForShutdown() returned before the agent was reaped")
	}
	if cmd.ProcessState == nil {
		t.Fatal("agent process was not reaped")
	}
}

func TestACPShutdownHelperProcess(t *testing.T) {
	if os.Getenv("TEAK_ACP_SHUTDOWN_HELPER") != "1" {
		return
	}
	select {}
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

func TestManager_PromptQueuedBeforeStartDeliversErrorThroughMessageChannel(t *testing.T) {
	mgr := NewManager("/tmp", "echo", nil)
	cmd := mgr.PromptQueued("hello", nil)
	if cmd == nil {
		t.Fatal("PromptQueued() returned a nil command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("PromptQueued command returned %T, want channel delivery", msg)
	}
	select {
	case msg := <-mgr.MsgChan():
		response, ok := msg.(AgentPromptResponseMsg)
		if !ok || response.Err == nil {
			t.Fatalf("queued message = %#v (%T), want failed prompt response", msg, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued prompt response")
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

type sessionControllerStub struct {
	model func(context.Context, sdk.SetSessionModelRequest) (sdk.SetSessionModelResponse, error)
	mode  func(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error)
}

func (s sessionControllerStub) SetSessionModel(ctx context.Context, req sdk.SetSessionModelRequest) (sdk.SetSessionModelResponse, error) {
	return s.model(ctx, req)
}

func (s sessionControllerStub) SetSessionMode(ctx context.Context, req sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return s.mode(ctx, req)
}

func TestManagerSetModelUsesBoundedContextAndCancelsWhenProcessExits(t *testing.T) {
	done := make(chan struct{})
	started := make(chan struct{})
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.running = true
	mgr.sessionID = "session"
	mgr.done = done
	mgr.sessionCtl = sessionControllerStub{
		model: func(ctx context.Context, req sdk.SetSessionModelRequest) (sdk.SetSessionModelResponse, error) {
			if req.SessionId != "session" || req.ModelId != "model" {
				t.Fatalf("unexpected request: %#v", req)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("SetModel context has no deadline")
			}
			close(started)
			<-ctx.Done()
			return sdk.SetSessionModelResponse{}, ctx.Err()
		},
		mode: func(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
			return sdk.SetSessionModeResponse{}, nil
		},
	}

	result := make(chan tea.Msg, 1)
	go func() { result <- mgr.SetModel("model")() }()
	<-started
	close(done)

	select {
	case msg := <-result:
		errMsg, ok := msg.(AgentErrorMsg)
		if !ok || !errors.Is(errMsg.Err, context.Canceled) {
			t.Fatalf("SetModel() = %#v, want cancellation error", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("SetModel did not cancel after ACP process exit")
	}
}

func TestManagerSetModeUsesBoundedContext(t *testing.T) {
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.running = true
	mgr.sessionID = "session"
	mgr.sessionCtl = sessionControllerStub{
		model: func(context.Context, sdk.SetSessionModelRequest) (sdk.SetSessionModelResponse, error) {
			return sdk.SetSessionModelResponse{}, nil
		},
		mode: func(ctx context.Context, req sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > acpSessionChangeTimeout {
				t.Fatalf("invalid SetMode deadline: %v, %t", deadline, ok)
			}
			if req.SessionId != "session" || req.ModeId != "plan" {
				t.Fatalf("unexpected request: %#v", req)
			}
			return sdk.SetSessionModeResponse{}, nil
		},
	}

	if msg, ok := mgr.SetMode("plan")().(AgentModeChangedMsg); !ok || msg.ModeId != "plan" {
		t.Fatalf("SetMode() = %#v, want AgentModeChangedMsg", msg)
	}
}
