package agent

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/ui"
)

func TestAgentToolCallUpdateAppliesKindAndBoundsRenderedPayload(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetSize(48, 16)
	line := 12

	model, _ = model.Update(acp.AgentToolCallMsg{
		ID:     "call-1",
		Title:  strings.Repeat("界", 3000),
		Kind:   sdk.ToolKindExecute,
		Status: sdk.ToolCallStatusPending,
		Locations: []sdk.ToolCallLocation{
			{Path: strings.Repeat("p", 5000), Line: &line},
		},
		Content: []sdk.ToolCallContent{
			sdk.ToolContent(sdk.TextBlock(strings.Repeat("x", maxToolContentBytes+100))),
			sdk.ToolDiffContent("src/main.go", "new body", "old body"),
			sdk.ToolTerminalRef("terminal-1"),
		},
	})

	tc := model.toolCallMap["call-1"]
	if tc == nil {
		t.Fatal("tool call was not added to the active map")
	}
	if len(tc.Title) > 4096 || len(tc.Locations) != 1 || len(tc.Locations[0].Path) > 4096 {
		t.Fatalf("tool call metadata was not bounded: title=%d locations=%#v", len(tc.Title), tc.Locations)
	}
	if len(tc.Content) != 1 || len(extractToolCallText(tc.Content[0])) != maxToolContentBytes {
		t.Fatalf("tool content was not bounded: %d entries, text=%d bytes", len(tc.Content), len(extractToolCallText(tc.Content[0])))
	}

	newTitle := "inspect"
	newKind := sdk.ToolKindRead
	status := sdk.ToolCallStatusCompleted
	model, _ = model.Update(acp.AgentToolCallUpdateMsg{
		ID:     "call-1",
		Title:  &newTitle,
		Kind:   &newKind,
		Status: &status,
		Content: []sdk.ToolCallContent{
			sdk.ToolContent(sdk.TextBlock("result")),
			sdk.ToolDiffContent("src/main.go", ""),
			sdk.ToolTerminalRef("terminal-1"),
		},
	})

	if tc.Kind != newKind || tc.Title != newTitle || tc.Status != status {
		t.Fatalf("tool update = kind %q title %q status %q, want kind %q title %q status %q", tc.Kind, tc.Title, tc.Status, newKind, newTitle, status)
	}
	tc.Expanded = true
	tc.Locations = nil // keep the title visible in the narrow render fixture
	rendered := strings.Join(model.renderToolCall(tc, 48), "\n")
	for _, want := range []string{"inspect", "result", "diff: src/main.go", "terminal: terminal-1"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expanded tool render missing %q: %q", want, rendered)
		}
	}

	// An update for an unknown call is harmless; ACP streams can race with
	// prompt completion and must not recreate stale tool state.
	model, _ = model.Update(acp.AgentToolCallUpdateMsg{ID: "unknown", Status: &status})
	if _, ok := model.toolCallMap["unknown"]; ok {
		t.Fatal("unknown tool update recreated a tool call")
	}
}

func TestAgentPromptResponseCommitsStreamAndResetsTransientState(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.loading = true
	model.state = AgentThinking
	model, _ = model.Update(acp.AgentTextMsg{Text: "answer"})
	model, _ = model.Update(acp.AgentThoughtMsg{Text: "hidden reasoning"})
	model, _ = model.Update(acp.AgentToolCallMsg{
		ID:     "call-1",
		Title:  "read",
		Kind:   sdk.ToolKindRead,
		Status: sdk.ToolCallStatusCompleted,
	})

	model, cmd := model.Update(acp.AgentPromptResponseMsg{Err: errors.New("agent unavailable")})
	if cmd != nil {
		t.Fatal("prompt completion returned an unexpected command")
	}
	if model.loading || model.state != AgentIdle || len(model.streamBlocks) != 0 || len(model.toolCallMap) != 0 {
		t.Fatalf("transient state after prompt response: loading=%t state=%v blocks=%d tools=%d", model.loading, model.state, len(model.streamBlocks), len(model.toolCallMap))
	}
	if len(model.messages) != 2 {
		t.Fatalf("messages = %d, want user-visible agent response plus error", len(model.messages))
	}
	var sawAnswer, sawError bool
	for _, message := range model.messages {
		if strings.Contains(message.Content, "answer") && len(message.ToolCalls) == 1 {
			sawAnswer = true
		}
		if strings.Contains(message.Content, "Agent request failed") {
			sawError = true
		}
	}
	if !sawAnswer || !sawError {
		t.Fatalf("committed messages = %#v, want answer and error", model.messages)
	}
}

func TestAgentHandleKeyContractsForSubmitCancelAndToolExpansion(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetSize(40, 12)

	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || model.loading || len(model.messages) != 0 {
		t.Fatal("empty submit should be ignored")
	}

	model.input.SetValue("  inspect project  ")
	model, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !model.loading || model.state != AgentThinking {
		t.Fatalf("submit state: cmd=%v loading=%t state=%v", cmd != nil, model.loading, model.state)
	}
	if got := model.messages[len(model.messages)-1].Content; got != "inspect project" {
		t.Fatalf("submitted content = %q, want trimmed prompt", got)
	}

	model, cmd = model.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("ctrl+c while loading did not request cancellation")
	}
	if _, ok := cmd().(CancelRequestedMsg); !ok {
		t.Fatalf("cancel command returned %T, want CancelRequestedMsg", cmd())
	}

	model.loading = false
	model.streamBlocks = []StreamBlock{{Kind: BlockToolCall, ToolCall: &ToolCallState{ID: "call-1"}}}
	if model.streamBlocks[0].ToolCall.Expanded {
		t.Fatal("tool call unexpectedly starts expanded")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.streamBlocks[0].ToolCall.Expanded {
		t.Fatal("tab did not expand the most recent tool call")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if model.streamBlocks[0].ToolCall.Expanded {
		t.Fatal("second tab did not collapse the most recent tool call")
	}
}

func TestAgentToolContentHelpersRenderAllSupportedVariants(t *testing.T) {
	contents := []sdk.ToolCallContent{
		sdk.ToolContent(sdk.TextBlock("text")),
		sdk.ToolDiffContent("file.go", "new"),
		sdk.ToolTerminalRef("term-1"),
		{},
	}
	wants := []string{"text", "diff: file.go", "terminal: term-1", ""}
	for i, content := range contents {
		if got := extractToolCallText(content); got != wants[i] {
			t.Errorf("extractToolCallText(%d) = %q, want %q", i, got, wants[i])
		}
	}

	bounded := boundedToolContent(contents)
	if len(bounded) != 3 {
		t.Fatalf("boundedToolContent kept %d entries, want 3 supported entries", len(bounded))
	}
	if got := chatMessageSize(ChatMessage{ToolCalls: []*ToolCallState{{Content: bounded}}}); got <= 0 {
		t.Fatalf("chatMessageSize = %d, want rendered tool payload bytes", got)
	}
}
