package app

import (
	"errors"
	"strings"
	"testing"

	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/agent"
)

func TestDirectAgentPromptResponseCommitsStream(t *testing.T) {
	m := newViewTestModel(t, false)
	m.agentPanel.SetConnected(true)
	m.agentPanel.SetSize(48, 16)
	m.agentPanel, _ = m.agentPanel.Update(acp.AgentTextMsg{Text: "completed answer"})
	if m.agentPanel.State() != agent.AgentThinking {
		t.Fatal("stream fixture did not enter the thinking state")
	}

	updatedAny, cmd := m.Update(acp.AgentPromptResponseMsg{StopReason: sdk.StopReason("end_turn")})
	if cmd == nil {
		t.Fatal("direct prompt completion did not schedule finalization")
	}
	updated := updatedAny.(Model)
	if updated.agentPanel.State() != agent.AgentThinking {
		t.Fatalf("agent state = %v, want thinking while finalizing", updated.agentPanel.State())
	}
	updatedAny, cmd = updated.Update(cmd())
	if cmd != nil {
		t.Fatal("prepared prompt completion emitted an unexpected command")
	}
	updated = updatedAny.(Model)
	if updated.agentPanel.State() != agent.AgentIdle {
		t.Fatalf("agent state = %v, want idle after direct prompt completion", updated.agentPanel.State())
	}
	if view := updated.agentPanel.View(); !strings.Contains(view, "completed answer") {
		t.Fatalf("completed response was not committed to the panel: %q", view)
	}
}

func TestDirectAgentModelAndModeResultsReachPanelAndCoordinator(t *testing.T) {
	m := newViewTestModel(t, false)
	m.agentPanel.SetConnected(true)

	modelAny, cmd := m.Update(acp.AgentModelChangedMsg{ModelId: sdk.ModelId("model-new")})
	if cmd != nil {
		t.Fatal("direct model result emitted an unexpected command")
	}
	modelUpdated := modelAny.(Model)
	if got := modelUpdated.agentPanel.CurrentModel(); got != sdk.ModelId("model-new") {
		t.Fatalf("panel model = %q, want model-new", got)
	}
	_, coordinatorModel, _ := modelUpdated.coordinator.GetACPCoordinator().GetSessionInfo()
	if coordinatorModel != "model-new" {
		t.Fatalf("coordinator model = %q, want model-new", coordinatorModel)
	}

	modeAny, cmd := modelUpdated.Update(acp.AgentModeChangedMsg{ModeId: sdk.SessionModeId("mode-new")})
	if cmd != nil {
		t.Fatal("direct mode result emitted an unexpected command")
	}
	modeUpdated := modeAny.(Model)
	if got := modeUpdated.agentPanel.CurrentMode(); got != sdk.SessionModeId("mode-new") {
		t.Fatalf("panel mode = %q, want mode-new", got)
	}
	_, _, coordinatorMode := modeUpdated.coordinator.GetACPCoordinator().GetSessionInfo()
	if coordinatorMode != "mode-new" {
		t.Fatalf("coordinator mode = %q, want mode-new", coordinatorMode)
	}
}

func TestDirectAgentErrorIsVisible(t *testing.T) {
	m := newViewTestModel(t, false)
	m.agentPanel.SetConnected(true)
	m.agentPanel.SetSize(48, 16)

	updatedAny, cmd := m.Update(acp.AgentErrorMsg{Err: errors.New("prompt already active")})
	if cmd != nil {
		t.Fatal("direct agent error emitted an unexpected command")
	}
	updated := updatedAny.(Model)
	if updated.status != "Error: prompt already active" {
		t.Fatalf("status = %q, want direct ACP error", updated.status)
	}
	if view := updated.agentPanel.View(); !strings.Contains(view, "prompt already active") {
		t.Fatalf("direct ACP error was not added to the panel: %q", view)
	}
}
