package acp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"

	agentruntime "teak/internal/agent/runtime"
)

const fakeACPEnv = "TEAK_ACP_FIXTURE"

// TestManagerEndToEndWithFakeACP exercises the same subprocess transport used
// by a configured agent. The fixture is deliberately tiny, deterministic, and
// implemented with the SDK's agent-side connection rather than a hand-written
// JSON-RPC transcript, so protocol changes fail loudly at the integration
// boundary.
func TestManagerEndToEndWithFakeACP(t *testing.T) {
	t.Setenv(fakeACPEnv, "1")
	root := t.TempDir()
	runs, err := agentruntime.NewManager(agentruntime.ManagerConfig{
		Store:          agentruntime.FileStore{Path: filepath.Join(root, "runs.json")},
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewManagerWithRuntime(root, os.Args[0], []string{"-test.run=^TestACPAgentFixtureProcess$", "--"}, runs)
	if err := mgr.Start(); err != nil {
		t.Fatalf("start fake ACP agent: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !mgr.WaitForShutdown(ctx) {
			t.Error("fake ACP agent did not shut down")
		}
	}()

	var sessionInfo AgentSessionInfoMsg
	var started bool
	deadline := time.After(2 * time.Second)
	for !started || sessionInfo.SessionID == "" {
		select {
		case msg := <-mgr.MsgChan():
			switch value := msg.(type) {
			case AgentSessionInfoMsg:
				sessionInfo = value
			case AgentStartedMsg:
				started = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for ACP startup messages")
		}
	}
	if sessionInfo.SessionID != "fixture-session" {
		t.Fatalf("session id = %q, want fixture-session", sessionInfo.SessionID)
	}

	msg := mgr.Prompt("inspect this workspace", nil)()
	response, ok := msg.(AgentPromptResponseMsg)
	if !ok {
		t.Fatalf("prompt returned %T, want AgentPromptResponseMsg", msg)
	}
	if response.Err != nil {
		t.Fatalf("prompt failed: %v", response.Err)
	}
	if response.StopReason != sdk.StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want end_turn", response.StopReason)
	}

	var thought, text string
	deadline = time.After(2 * time.Second)
	for thought == "" || text == "" {
		select {
		case incoming := <-mgr.MsgChan():
			switch value := incoming.(type) {
			case AgentThoughtMsg:
				thought += value.Text
			case AgentTextMsg:
				text += value.Text
			}
		case <-deadline:
			t.Fatalf("timed out waiting for streamed output (thought=%q text=%q)", thought, text)
		}
	}
	if thought != "fixture thought" || text != "fixture response" {
		t.Fatalf("streamed output = thought %q/text %q, want fixture messages", thought, text)
	}

	records := runs.List()
	if len(records) != 1 {
		t.Fatalf("runtime records = %d, want 1", len(records))
	}
	if records[0].Status != agentruntime.RunCompleted {
		t.Fatalf("runtime status = %q, want completed", records[0].Status)
	}
	if records[0].Handoff == nil || !records[0].Handoff.Verified {
		t.Fatal("completed ACP run must contain a verified handoff")
	}
}

// TestACPAgentFixtureProcess is launched as a child by the integration test.
// Keeping it in the test binary avoids a repository executable or a dependency
// on a user's installed agent while still exercising real stdio JSON-RPC.
func TestACPAgentFixtureProcess(t *testing.T) {
	if os.Getenv(fakeACPEnv) != "1" {
		return
	}

	agent := &fakeACPAgent{}
	conn := sdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
}

type fakeACPAgent struct {
	conn *sdk.AgentSideConnection
}

var _ sdk.Agent = (*fakeACPAgent)(nil)

func (a *fakeACPAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, nil
}

func (*fakeACPAgent) Initialize(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	return sdk.InitializeResponse{
		ProtocolVersion:   sdk.ProtocolVersionNumber,
		AgentCapabilities: sdk.AgentCapabilities{LoadSession: false},
	}, nil
}

func (*fakeACPAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

func (*fakeACPAgent) NewSession(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	return sdk.NewSessionResponse{SessionId: sdk.SessionId("fixture-session")}, nil
}

func (a *fakeACPAgent) Prompt(ctx context.Context, params sdk.PromptRequest) (sdk.PromptResponse, error) {
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: params.SessionId,
		Update:    sdk.UpdateAgentThoughtText("fixture thought"),
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: params.SessionId,
		Update:    sdk.UpdateAgentMessageText("fixture response"),
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

func (*fakeACPAgent) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, nil
}
