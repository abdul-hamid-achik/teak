package agent

import (
	"fmt"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/ui"
)

// assertAgentLinesFitPanelWidth fails when any rendered panel line is wider
// than the panel, which would break the layout the panel is joined into.
func assertAgentLinesFitPanelWidth(t *testing.T, label string, lines []string, width int) {
	t.Helper()
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("%s: line %d width = %d, want at most %d: %q", label, i, got, width, line)
		}
	}
}

func TestAgentPermissionPromptWrapsLongTitleToPanelWidth(t *testing.T) {
	zone.NewGlobal()
	const width = 40
	longURL := "https://example.com/" + strings.Repeat("segment", 30)
	toolKind := sdk.ToolKindFetch
	model := New(ui.DefaultTheme())
	model.SetSize(width, 20)
	model.connected = true
	model.permission = &PermissionPrompt{
		ToolCall: sdk.RequestPermissionToolCall{
			ToolCallId: "fetch-long",
			Kind:       &toolKind,
			Title:      &longURL,
		},
		Options: []sdk.PermissionOption{
			{Kind: sdk.PermissionOptionKindAllowOnce, OptionId: "once"},
		},
		ResponseCh: make(chan sdk.RequestPermissionResponse, 1),
	}

	assertAgentLinesFitPanelWidth(t, "permission", model.renderPermission(width-2), width)
}

func TestAgentWriteProposalWrapsLongPathToPanelWidth(t *testing.T) {
	const width = 40
	model := New(ui.DefaultTheme())
	model.SetSize(width, 20)
	model.connected = true
	model.pendingWrite = &acp.AgentWriteFileMsg{
		Path:    "/very/long/" + strings.Repeat("directory_name_", 8) + "file.go",
		Content: "x\n",
	}

	assertAgentLinesFitPanelWidth(t, "write proposal", model.renderWriteProposal(width-2), width)
}

func TestAgentViewNeverExceedsTinyPanelWidth(t *testing.T) {
	zone.NewGlobal()
	for _, size := range []struct{ w, h int }{{1, 6}, {3, 8}, {5, 8}, {6, 9}} {
		model := New(ui.DefaultTheme())
		model.SetSize(size.w, size.h)
		model.connected = true
		model.currentModel = sdk.ModelId("claude-sonnet-4-5-with-a-very-long-model-name")
		model.messages = []ChatMessage{{Role: RoleAgent, Content: "wide 界界界界 and " + strings.Repeat("x", 20)}}
		tc := &ToolCallState{ID: "t", Title: "bash: ls", Kind: sdk.ToolKindExecute, Status: sdk.ToolCallStatusCompleted, Expanded: true,
			Content: []sdk.ToolCallContent{{Content: &sdk.ToolCallContentContent{Content: sdk.ContentBlock{Text: &sdk.ContentBlockText{Text: "output " + strings.Repeat("y", 30)}}}}}}
		model.messages[0].ToolCalls = []*ToolCallState{tc}

		view := model.View()
		assertAgentLinesFitPanelWidth(t, fmt.Sprintf("view %dx%d", size.w, size.h), strings.Split(view, "\n"), size.w)
	}
}
