package app

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/config"
	"teak/internal/overlay"
)

func TestACPAppSurfacesAgentFailuresWithoutPanicking(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()
	model.agentPanel.SetSize(80, 20)

	tests := []struct {
		name string
		msg  any
		want string
	}{
		{
			name: "nil agent error",
			msg:  acp.AgentErrorMsg{},
			want: "Error: unknown agent error",
		},
		{
			name: "stopped agent error",
			msg:  acp.AgentStoppedMsg{Err: errors.New("process died")},
			want: "Agent stopped: process died",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedAny, _ := model.routeACPMsg(acpMsg{msg: tt.msg})
			updated, ok := updatedAny.(Model)
			if !ok {
				t.Fatalf("updated model type = %T, want app.Model", updatedAny)
			}
			if updated.status != tt.want {
				t.Fatalf("status = %q, want %q", updated.status, tt.want)
			}
		})
	}
}

func TestACPAppRouteKeepsCoordinatorAndPanelInSync(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	permission := acp.AgentPermissionRequestMsg{
		ToolCall:   sdk.RequestPermissionToolCall{},
		ResponseCh: make(chan sdk.RequestPermissionResponse, 1),
	}
	updatedAny, cmd := model.routeACPMsg(acpMsg{msg: permission})
	if cmd != nil {
		t.Fatalf("permission route command = %T, want nil without an ACP manager", cmd)
	}
	updated := updatedAny.(Model)
	if !updated.showAgent {
		t.Fatal("permission request did not reveal the agent panel")
	}
	if !updated.agentPanel.HasPermissionPending() {
		t.Fatal("permission request did not reach the agent panel")
	}

	updatedAny, cmd = updated.routeACPMsg(acpMsg{msg: acp.AgentSessionInfoMsg{
		SessionID:    sdk.SessionId("session-1"),
		CurrentModel: sdk.ModelId("model-1"),
		CurrentMode:  sdk.SessionModeId("mode-1"),
	}})
	if cmd != nil {
		t.Fatalf("session info route command = %T, want nil without an ACP manager", cmd)
	}
	updated = updatedAny.(Model)
	sessionID, modelID, mode := updated.coordinator.GetACPCoordinator().GetSessionInfo()
	if sessionID != "session-1" || modelID != "model-1" || mode != "mode-1" {
		t.Fatalf("coordinator session = (%q, %q, %q), want (%q, %q, %q)", sessionID, modelID, mode, "session-1", "model-1", "mode-1")
	}
}

func TestACPAppModelPickerUsesSessionMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	updatedAny, _ := model.routeACPMsg(acpMsg{msg: acp.AgentSessionInfoMsg{
		Models: []sdk.ModelInfo{{ModelId: sdk.ModelId("model-a"), Name: "Model A"}},
		Modes:  []sdk.SessionMode{{Id: sdk.SessionModeId("edit"), Name: "Edit"}},
	}})
	model = updatedAny.(Model)
	model.showAgent = true
	model.focus = FocusAgent
	model.width, model.height = 100, 30
	model.agentPanel.SetSize(80, 20)
	model.agentPanel.SetConnected(true)
	if cmd := model.agentPanel.Focus(); cmd != nil {
		_ = cmd
	}
	for _, r := range "/model" {
		updatedAny, _ = model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		model = updatedAny.(Model)
	}
	updatedAny, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatalf("model picker command = %T, want nil after local picker creation", cmd)
	}
	picker, ok := updated.overlayStack.Top().(*overlay.Picker)
	if !ok || picker.ZoneID() != "agent-model-picker" || picker.FilteredCount() != 1 {
		t.Fatalf("agent model picker = %T/%q count=%d", updated.overlayStack.Top(), picker.ZoneID(), picker.FilteredCount())
	}
	if updated.agentPanel.InputValue() != "" {
		t.Fatalf("model command remained in input: %q", updated.agentPanel.InputValue())
	}
}
