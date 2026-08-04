package acp

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
	agentruntime "teak/internal/agent/runtime"
)

func TestRequestPermissionBoundsPayloadBeforeQueue(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()

	huge := strings.Repeat("x", maxACPMetadataBytes+128)
	locations := make([]sdk.ToolCallLocation, maxACPLocations+1)
	for i := range locations {
		line := i
		locations[i] = sdk.ToolCallLocation{Path: huge, Line: &line, Meta: map[string]any{"payload": huge}}
	}
	title := huge
	kind := sdk.ToolKind(huge)
	status := sdk.ToolCallStatus(huge)
	options := []sdk.PermissionOption{{
		Kind:     sdk.PermissionOptionKindAllowOnce,
		Name:     "Allow once",
		OptionId: "allow-once",
		Meta:     map[string]any{"payload": huge},
	}}

	resultCh := make(chan error, 1)
	go func() {
		_, err := handler.RequestPermission(context.Background(), sdk.RequestPermissionRequest{
			Options: options,
			ToolCall: sdk.RequestPermissionToolCall{
				ToolCallId: "call-1",
				Title:      &title,
				Kind:       &kind,
				Status:     &status,
				Locations:  locations,
				Content: []sdk.ToolCallContent{
					sdk.ToolDiffContent("main.go", strings.Repeat("y", maxACPStreamOutputBytes+128)),
				},
				Meta:      map[string]any{"payload": huge},
				RawInput:  map[string]any{"payload": huge},
				RawOutput: map[string]any{"payload": huge},
			},
		})
		resultCh <- err
	}()

	message := <-messages
	prompt, ok := message.(AgentPermissionRequestMsg)
	if !ok {
		t.Fatalf("queued message = %T, want AgentPermissionRequestMsg", message)
	}
	if prompt.ToolCall.Meta != nil || prompt.ToolCall.RawInput != nil || prompt.ToolCall.RawOutput != nil {
		t.Fatal("permission payload retained unbounded extension metadata")
	}
	if prompt.ToolCall.Title == nil || len(*prompt.ToolCall.Title) > maxACPMetadataBytes {
		if prompt.ToolCall.Title == nil {
			t.Fatal("permission title was lost")
		}
		t.Fatalf("permission title length = %d, want <= %d", len(*prompt.ToolCall.Title), maxACPMetadataBytes)
	}
	if len(prompt.ToolCall.Locations) != maxACPLocations || prompt.ToolCall.Locations[0].Meta != nil {
		t.Fatalf("permission locations = %d/%#v, want bounded copies without metadata", len(prompt.ToolCall.Locations), prompt.ToolCall.Locations[0])
	}
	if len(prompt.ToolCall.Content) != 1 || prompt.ToolCall.Content[0].Diff == nil || len(prompt.ToolCall.Content[0].Diff.NewText) > maxACPStreamOutputBytes {
		t.Fatalf("permission content = %#v, want one bounded diff", prompt.ToolCall.Content)
	}
	if len(prompt.Options) != 1 || prompt.Options[0].Meta != nil {
		t.Fatalf("permission options = %#v, want copied option without metadata", prompt.Options)
	}

	prompt.ResponseCh <- sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()}
	if err := <-resultCh; err != nil {
		t.Fatalf("RequestPermission() error = %v", err)
	}
}

func TestRequestPermissionAuditsDecisionWithoutPersistingPayload(t *testing.T) {
	tests := []struct {
		name        string
		kind        sdk.PermissionOptionKind
		response    sdk.RequestPermissionResponse
		wantOutcome string
		wantDetail  string
	}{
		{
			name:        "allow once",
			kind:        sdk.PermissionOptionKindAllowOnce,
			response:    sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeSelected("choice")},
			wantOutcome: "allowed_once",
			wantDetail:  "scope=once",
		},
		{
			name:        "allow always",
			kind:        sdk.PermissionOptionKindAllowAlways,
			response:    sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeSelected("choice")},
			wantOutcome: "allowed_always",
			wantDetail:  "scope=always",
		},
		{
			name:        "reject once",
			kind:        sdk.PermissionOptionKindRejectOnce,
			response:    sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeSelected("choice")},
			wantOutcome: "rejected_once",
			wantDetail:  "scope=once",
		},
		{
			name:        "cancelled",
			kind:        sdk.PermissionOptionKindAllowOnce,
			response:    sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()},
			wantOutcome: "cancelled",
			wantDetail:  "user_or_session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			run, err := runManager.Start(agentruntime.RunSpec{
				Objective:             "audit permission decision",
				Workspace:             root,
				RequestedCapabilities: agentruntime.Capabilities{Read: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			messages := make(chan tea.Msg, 1)
			handler := newClientHandler(messages, root)
			handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })
			title := "private/project-secret.txt"
			result := make(chan error, 1)
			go func() {
				_, requestErr := handler.RequestPermission(context.Background(), sdk.RequestPermissionRequest{
					ToolCall: sdk.RequestPermissionToolCall{
						ToolCallId: "sensitive-call-id",
						Title:      &title,
					},
					Options: []sdk.PermissionOption{{Kind: tt.kind, Name: "choice", OptionId: "choice"}},
				})
				result <- requestErr
			}()

			prompt := (<-messages).(AgentPermissionRequestMsg)
			prompt.ResponseCh <- tt.response
			if err := <-result; err != nil {
				t.Fatalf("RequestPermission() error = %v", err)
			}
			record, err := runManager.Get(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(record.Audit) != 1 {
				t.Fatalf("permission audit = %#v, want one decision", record.Audit)
			}
			entry := record.Audit[0]
			if entry.Operation != "permission" || entry.Outcome != tt.wantOutcome || entry.Detail != tt.wantDetail {
				t.Fatalf("permission audit = %#v, want %s/%s", entry, tt.wantOutcome, tt.wantDetail)
			}
			if strings.Contains(entry.Detail, "secret") || strings.Contains(entry.Detail, "private") {
				t.Fatalf("permission audit retained request payload: %#v", entry)
			}
			if err := runManager.Cancel(run.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRequestPermissionRejectsTooManyOptions(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	options := make([]sdk.PermissionOption, maxPermissionOptions+1)
	for i := range options {
		options[i] = sdk.PermissionOption{Kind: sdk.PermissionOptionKindAllowOnce, Name: "allow", OptionId: sdk.PermissionOptionId("allow-") + sdk.PermissionOptionId(string(rune('a'+i)))}
	}

	_, err := handler.RequestPermission(context.Background(), sdk.RequestPermissionRequest{Options: options})
	if err == nil || !strings.Contains(err.Error(), "permission options") {
		t.Fatalf("RequestPermission() error = %v, want bounded options error", err)
	}
	select {
	case message := <-messages:
		t.Fatalf("oversized permission request queued %T", message)
	default:
	}
}

func TestRequestPermissionRejectsTerminalRuntime(t *testing.T) {
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{Objective: "permission", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := runManager.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}

	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })
	_, err = handler.RequestPermission(context.Background(), sdk.RequestPermissionRequest{
		Options: []sdk.PermissionOption{{Kind: sdk.PermissionOptionKindAllowOnce, Name: "allow", OptionId: "allow"}},
	})
	if !errors.Is(err, agentruntime.ErrRunTerminal) {
		t.Fatalf("RequestPermission() error = %v, want terminal runtime rejection", err)
	}
	select {
	case message := <-messages:
		t.Fatalf("terminal runtime queued permission message %T", message)
	default:
	}
}
