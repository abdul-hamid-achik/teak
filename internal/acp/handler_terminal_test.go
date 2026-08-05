package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
	agentruntime "teak/internal/agent/runtime"
	"teak/internal/execpolicy"
)

func TestCreateTerminalRejectsPathsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	handler := newClientHandler(make(chan tea.Msg), root)

	for _, request := range []sdk.CreateTerminalRequest{
		{Command: "sh", Cwd: &outside},
		{Command: "/bin/sh"},
	} {
		if _, err := handler.CreateTerminal(context.Background(), request); err == nil {
			t.Fatalf("CreateTerminal(%+v) succeeded for a path outside workspace", request)
		}
	}
}

func TestCreateTerminalRequiredExecutionPolicyFailsClosed(t *testing.T) {
	root := t.TempDir()
	handler := newClientHandler(make(chan tea.Msg), root)
	handler.setExecutionPolicy(execpolicy.Policy{
		Root:              root,
		Mode:              execpolicy.ModeRequired,
		SandboxExecutable: filepath.Join(root, "missing-sandbox-exec"),
	})

	_, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh"})
	if !errors.Is(err, execpolicy.ErrSandboxUnavailable) {
		t.Fatalf("CreateTerminal() error = %v, want ErrSandboxUnavailable", err)
	}
}

func TestACPHandlersTreatNilContextAsBackground(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	handler := newClientHandler(make(chan tea.Msg), root)
	//nolint:staticcheck // This test verifies the documented nil-context normalization contract.
	terminal, err := handler.CreateTerminal(nil, sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", "printf ready"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal(nil) error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: terminal.TerminalId})
	})
	//nolint:staticcheck // This test verifies the documented nil-context normalization contract.
	if _, err := handler.WaitForTerminalExit(nil, sdk.WaitForTerminalExitRequest{TerminalId: terminal.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit(nil) error = %v", err)
	}
	//nolint:staticcheck // This test verifies the documented nil-context normalization contract.
	output, err := handler.TerminalOutput(nil, sdk.TerminalOutputRequest{TerminalId: terminal.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput(nil) error = %v", err)
	}
	if output.Output != "ready" {
		t.Fatalf("TerminalOutput(nil) = %q, want ready", output.Output)
	}

	//nolint:staticcheck // This test verifies the documented nil-context normalization contract.
	content, err := ReadFileFromDisk(nil, root, "notes.txt", nil, nil)
	if err != nil {
		t.Fatalf("ReadFileFromDisk(nil) error = %v", err)
	}
	if content != "hello\n" {
		t.Fatalf("ReadFileFromDisk(nil) = %q, want hello newline", content)
	}
}

func TestACPHandlerEnforcesRuntimeCapabilities(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	readOnly, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "read only",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	readOnlyHandler := newClientHandler(make(chan tea.Msg), root)
	readOnlyHandler.setRuntime(runManager, func() agentruntime.RunID { return readOnly.ID })
	if _, err := readOnlyHandler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh"}); !errors.Is(err, agentruntime.ErrCapabilityDenied) {
		t.Fatalf("read-only run terminal error = %v, want capability denial", err)
	}
	readOnlyRecord, err := runManager.Get(readOnly.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(readOnlyRecord.Audit) != 1 || readOnlyRecord.Audit[0].Outcome != "denied" || readOnlyRecord.Audit[0].Detail != "shell_capability" {
		t.Fatalf("read-only terminal audit = %#v, want denied shell capability", readOnlyRecord.Audit)
	}
	if _, err := readOnlyHandler.WriteTextFile(context.Background(), sdk.WriteTextFileRequest{Path: "main.go", Content: "package main"}); !errors.Is(err, agentruntime.ErrCapabilityDenied) {
		t.Fatalf("read-only run write error = %v, want capability denial", err)
	}
	if err := runManager.Cancel(readOnly.ID); err != nil {
		t.Fatal(err)
	}

	shellRun, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "run a bounded command",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true, Shell: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	shellHandler := newClientHandler(make(chan tea.Msg), root)
	shellHandler.setRuntime(runManager, func() agentruntime.RunID { return shellRun.ID })
	response, err := shellHandler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", "printf ready"},
	})
	if err != nil {
		t.Fatalf("shell-capable run CreateTerminal() error = %v", err)
	}
	if _, err := shellHandler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatal(err)
	}
	if _, err := shellHandler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatal(err)
	}
	shellRecord, err := runManager.Get(shellRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shellRecord.Audit) != 3 {
		t.Fatalf("shell terminal audit = %#v, want authorization, start, and exit", shellRecord.Audit)
	}
	for i, want := range []string{"authorized", "started", "exited"} {
		if shellRecord.Audit[i].Outcome != want {
			t.Fatalf("shell terminal audit[%d] = %#v, want outcome %q", i, shellRecord.Audit[i], want)
		}
	}
	if _, err := shellHandler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatal(err)
	}
	if err := runManager.Cancel(shellRun.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTerminalStopsWhenRuntimeRunIsCancelled(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "run a cancellable terminal",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true, Shell: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newClientHandler(make(chan tea.Msg), root)
	handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })
	response, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: response.TerminalId})
		_ = runManager.Cancel(run.ID)
	})

	if err := runManager.Cancel(run.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if _, err := handler.WaitForTerminalExit(waitCtx, sdk.WaitForTerminalExitRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() after run cancellation error = %v, want terminal exit", err)
	}
}

func TestCreateTerminalHonorsRuntimeOutputBudget(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "capture bounded output",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true, Shell: true},
		Budget:                agentruntime.Budget{MaxOutputBytes: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newClientHandler(make(chan tea.Msg), root)
	handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })
	requestLimit := 128
	response, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command:         "sh",
		Args:            []string{"-c", "printf '0123456789abcdefghijklmnopqrstuvwxyz'"},
		OutputByteLimit: &requestLimit,
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: response.TerminalId})
		_ = runManager.Cancel(run.ID)
	})
	if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
	output, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: response.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput() error = %v", err)
	}
	if len(output.Output) > 12 {
		t.Fatalf("captured %d bytes, want runtime budget <= 12", len(output.Output))
	}
	if !output.Truncated {
		t.Fatal("TerminalOutput().Truncated = false, want true when runtime budget is exceeded")
	}
}

func TestSessionUpdateHonorsRuntimeOutputBudget(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "bound streamed output",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true},
		Budget:                agentruntime.Budget{MaxOutputBytes: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runManager.Cancel(run.ID) })

	messages := make(chan tea.Msg, 4)
	handler := newClientHandler(messages, root)
	handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })
	handler.beginPromptOutput()
	for i := 0; i < 2; i++ {
		if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{
			SessionId: sdk.SessionId("fixture"),
			Update:    sdk.UpdateAgentMessageText(strings.Repeat("x", 40)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var combined strings.Builder
	for i := 0; i < 2; i++ {
		msg := <-messages
		text, ok := msg.(AgentTextMsg)
		if !ok {
			t.Fatalf("message %T, want AgentTextMsg", msg)
		}
		combined.WriteString(text.Text)
	}
	if got := combined.String(); len(got) > 64 || !strings.Contains(got, acpStreamTruncatedMarker) {
		t.Fatalf("streamed output = %q (%d bytes), want visible truncation within 64 bytes", got, len(got))
	}

	// A new prompt receives a fresh budget rather than inheriting exhaustion
	// from the previous turn.
	handler.beginPromptOutput()
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{
		SessionId: sdk.SessionId("fixture"),
		Update:    sdk.UpdateAgentThoughtText(strings.Repeat("y", 40)),
	}); err != nil {
		t.Fatal(err)
	}
	msg := <-messages
	thought, ok := msg.(AgentThoughtMsg)
	if !ok || thought.Text != strings.Repeat("y", 40) {
		t.Fatalf("new prompt thought = %#v, want full 40-byte chunk", msg)
	}
}

func TestSessionUpdateSplitsLargeTextAndThoughtChunksBeforeQueue(t *testing.T) {
	tests := []struct {
		name   string
		update func(string) sdk.SessionUpdate
		text   func(tea.Msg) (string, bool)
	}{
		{
			name:   "agent text",
			update: sdk.UpdateAgentMessageText,
			text: func(msg tea.Msg) (string, bool) {
				value, ok := msg.(AgentTextMsg)
				return value.Text, ok
			},
		},
		{
			name:   "agent thought",
			update: sdk.UpdateAgentThoughtText,
			text: func(msg tea.Msg) (string, bool) {
				value, ok := msg.(AgentThoughtMsg)
				return value.Text, ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := make(chan tea.Msg, 8)
			handler := newClientHandler(messages, t.TempDir())
			handler.beginPromptOutput()
			input := strings.Repeat("x", MaxAgentStreamChunkBytes-1) +
				"界" + strings.Repeat("y", MaxAgentStreamChunkBytes+17)
			if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{
				SessionId: sdk.SessionId("fixture"),
				Update:    tt.update(input),
			}); err != nil {
				t.Fatal(err)
			}

			var combined strings.Builder
			messageCount := len(messages)
			if messageCount < 2 {
				t.Fatalf("queued messages = %d, want split stream", messageCount)
			}
			for range messageCount {
				part, ok := tt.text(<-messages)
				if !ok {
					t.Fatal("queued stream message has the wrong type")
				}
				if len(part) > MaxAgentStreamChunkBytes {
					t.Fatalf("queued chunk = %d bytes, want at most %d", len(part), MaxAgentStreamChunkBytes)
				}
				if !utf8.ValidString(part) {
					t.Fatalf("queued chunk is not valid UTF-8: %q", part)
				}
				combined.WriteString(part)
			}
			if got := combined.String(); got != input {
				t.Fatalf("combined stream length = %d, want exact %d-byte input", len(got), len(input))
			}
		})
	}
}

func TestSessionUpdateNormalizesInvalidUTF8BeforeQueue(t *testing.T) {
	messages := make(chan tea.Msg, 2)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()
	input := "before" + string([]byte{0xff, 0xfe}) + "after"
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{
		Update: sdk.UpdateAgentMessageText(input),
	}); err != nil {
		t.Fatal(err)
	}
	message := <-messages
	text, ok := message.(AgentTextMsg)
	if !ok {
		t.Fatalf("queued message = %T, want AgentTextMsg", message)
	}
	if !utf8.ValidString(text.Text) || text.Text != strings.ToValidUTF8(input, "�") {
		t.Fatalf("queued text = %q, want normalized UTF-8", text.Text)
	}
}

func TestNormalizeAgentStreamTextCapsRawInputBeforeNormalization(t *testing.T) {
	const want = maxACPStreamOutputBytes + utf8.UTFMax
	input := strings.Repeat("x", want+1024)
	if got := normalizeAgentStreamText(input); len(got) != want || !utf8.ValidString(got) {
		t.Fatalf("normalized input = %d valid=%t, want %d valid bytes", len(got), utf8.ValidString(got), want)
	}
}

func TestSessionUpdateBoundsToolCallDiffBeforeQueue(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()
	handler.mu.Lock()
	handler.streamLimit = 64
	handler.mu.Unlock()

	huge := strings.Repeat("x", 128)
	update := sdk.StartToolCall("call-1", "edit", sdk.WithStartContent([]sdk.ToolCallContent{
		sdk.ToolDiffContent("main.go", huge),
	}))
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: update}); err != nil {
		t.Fatal(err)
	}

	message := <-messages
	call, ok := message.(AgentToolCallMsg)
	if !ok || len(call.Content) != 1 || call.Content[0].Diff == nil {
		t.Fatalf("tool call message = %#v, want one diff content", message)
	}
	content := call.Content[0].Diff.NewText
	if len(content) > 64 || !strings.Contains(content, acpStreamTruncatedMarker) {
		t.Fatalf("queued diff content length=%d value=%q, want bounded visible truncation", len(content), content)
	}
	if content == huge {
		t.Fatal("queued tool-call diff retained the unbounded input")
	}
}

func TestSessionUpdateReplacesOversizedBinaryToolContent(t *testing.T) {
	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()
	handler.mu.Lock()
	handler.streamLimit = 32
	handler.mu.Unlock()

	huge := strings.Repeat("A", 128)
	update := sdk.StartToolCall("call-2", "image", sdk.WithStartContent([]sdk.ToolCallContent{
		sdk.ToolContent(sdk.ImageBlock(huge, "image/png")),
	}))
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: update}); err != nil {
		t.Fatal(err)
	}

	message := <-messages
	call, ok := message.(AgentToolCallMsg)
	if !ok || len(call.Content) != 1 || call.Content[0].Content == nil || call.Content[0].Content.Content.Text == nil {
		t.Fatalf("tool call message = %#v, want a bounded text replacement", message)
	}
	if got := call.Content[0].Content.Content.Text.Text; !strings.Contains(got, "image content truncated") {
		t.Fatalf("binary replacement = %q, want explicit truncation marker", got)
	}
}

func TestSessionUpdateBoundsAllEmbeddedContentVariants(t *testing.T) {
	huge := strings.Repeat("x", maxACPMetadataBytes+128)
	tests := []struct {
		name  string
		block sdk.ContentBlock
		check func(t *testing.T, block sdk.ContentBlock)
	}{
		{
			name:  "audio payload",
			block: sdk.AudioBlock(strings.Repeat("A", 128), "audio/"+huge),
			check: func(t *testing.T, block sdk.ContentBlock) {
				if block.Text == nil || !strings.Contains(block.Text.Text, "audio content truncated") {
					t.Fatalf("audio block = %#v, want bounded text marker", block)
				}
			},
		},
		{
			name: "resource link metadata",
			block: sdk.ContentBlock{ResourceLink: &sdk.ContentBlockResourceLink{
				Name:        huge,
				Uri:         huge,
				Description: &huge,
				Title:       &huge,
				MimeType:    &huge,
				Meta:        map[string]any{"payload": huge},
			}},
			check: func(t *testing.T, block sdk.ContentBlock) {
				if block.ResourceLink == nil || len(block.ResourceLink.Name) > maxACPMetadataBytes || len(block.ResourceLink.Uri) > maxACPMetadataBytes || block.ResourceLink.Meta != nil {
					t.Fatalf("resource link = %#v, want bounded metadata without meta", block)
				}
			},
		},
		{
			name: "text resource",
			block: sdk.ResourceBlock(sdk.EmbeddedResourceResource{TextResourceContents: &sdk.TextResourceContents{
				Text:     strings.Repeat("T", 128),
				Uri:      huge,
				MimeType: &huge,
				Meta:     map[string]any{"payload": huge},
			}}),
			check: func(t *testing.T, block sdk.ContentBlock) {
				resource := block.Resource
				if resource == nil || resource.Resource.TextResourceContents == nil {
					t.Fatalf("text resource = %#v, want text resource", block)
				}
				text := resource.Resource.TextResourceContents
				if len(text.Text) > 32 || len(text.Uri) > maxACPMetadataBytes || text.Meta != nil {
					t.Fatalf("text resource = %#v, want bounded text/metadata", text)
				}
			},
		},
		{
			name: "blob resource",
			block: sdk.ResourceBlock(sdk.EmbeddedResourceResource{BlobResourceContents: &sdk.BlobResourceContents{
				Blob:     strings.Repeat("B", 128),
				Uri:      huge,
				MimeType: &huge,
				Meta:     map[string]any{"payload": huge},
			}}),
			check: func(t *testing.T, block sdk.ContentBlock) {
				if block.Text == nil || !strings.Contains(block.Text.Text, "resource content truncated") {
					t.Fatalf("blob resource = %#v, want bounded text marker", block)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := make(chan tea.Msg, 1)
			handler := newClientHandler(messages, t.TempDir())
			handler.beginPromptOutput()
			handler.mu.Lock()
			handler.streamLimit = 32
			handler.mu.Unlock()
			update := sdk.StartToolCall(sdk.ToolCallId("bounded-"+tt.name), tt.name, sdk.WithStartContent([]sdk.ToolCallContent{sdk.ToolContent(tt.block)}))
			if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: update}); err != nil {
				t.Fatal(err)
			}
			message := <-messages
			call, ok := message.(AgentToolCallMsg)
			if !ok || len(call.Content) != 1 || call.Content[0].Content == nil {
				t.Fatalf("tool call message = %#v, want one content block", message)
			}
			tt.check(t, call.Content[0].Content.Content)
		})
	}
}

func TestSessionUpdateDropsUnboundedTextAnnotationsAndCapsImageURI(t *testing.T) {
	huge := strings.Repeat("x", maxACPMetadataBytes+128)
	imageURI := huge
	update := sdk.StartToolCall(sdk.ToolCallId("metadata-boundary"), "metadata", sdk.WithStartContent([]sdk.ToolCallContent{
		sdk.ToolContent(sdk.ContentBlock{Text: &sdk.ContentBlockText{
			Text:        "safe text",
			Meta:        map[string]any{"payload": huge},
			Annotations: &sdk.Annotations{Meta: map[string]any{"payload": huge}, LastModified: &huge},
		}}),
		sdk.ToolContent(sdk.ContentBlock{Image: &sdk.ContentBlockImage{
			Data:        "small",
			MimeType:    "image/png",
			Uri:         &imageURI,
			Meta:        map[string]any{"payload": huge},
			Annotations: &sdk.Annotations{Meta: map[string]any{"payload": huge}},
		}}),
	}))

	messages := make(chan tea.Msg, 1)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: update}); err != nil {
		t.Fatal(err)
	}
	call, ok := (<-messages).(AgentToolCallMsg)
	if !ok || len(call.Content) != 2 {
		t.Fatalf("tool call message = %#v, want two bounded contents", call)
	}
	textBlock := call.Content[0].Content.Content.Text
	if textBlock == nil || textBlock.Annotations != nil || textBlock.Meta != nil {
		t.Fatalf("text block = %#v, want annotations and meta removed", textBlock)
	}
	imageBlock := call.Content[1].Content.Content.Image
	if imageBlock == nil || imageBlock.Uri == nil || len(*imageBlock.Uri) > maxACPMetadataBytes || imageBlock.Annotations != nil || imageBlock.Meta != nil {
		t.Fatalf("image block = %#v, want bounded URI and no metadata", imageBlock)
	}
}

func TestSessionUpdateBoundsToolCallMetadataAndPlan(t *testing.T) {
	messages := make(chan tea.Msg, 2)
	handler := newClientHandler(messages, t.TempDir())
	handler.beginPromptOutput()

	huge := strings.Repeat("x", maxACPMetadataBytes+128)
	locations := make([]sdk.ToolCallLocation, maxACPLocations+1)
	for i := range locations {
		line := i
		locations[i] = sdk.ToolCallLocation{Path: huge, Line: &line, Meta: map[string]any{"payload": huge}}
	}
	update := sdk.StartToolCall(
		sdk.ToolCallId(huge),
		huge,
		sdk.WithStartLocations(locations),
	)
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: update}); err != nil {
		t.Fatal(err)
	}

	message := <-messages
	call, ok := message.(AgentToolCallMsg)
	if !ok {
		t.Fatalf("message = %T, want AgentToolCallMsg", message)
	}
	if len(call.Locations) != maxACPLocations {
		t.Fatalf("locations = %d, want cap %d", len(call.Locations), maxACPLocations)
	}
	if len(call.ID) > maxACPMetadataBytes || len(call.Title) > maxACPMetadataBytes {
		t.Fatalf("metadata lengths id=%d title=%d, want <= %d", len(call.ID), len(call.Title), maxACPMetadataBytes)
	}
	if len(call.Locations[0].Path) > maxACPMetadataBytes || call.Locations[0].Meta != nil {
		t.Fatalf("location = %#v, want bounded path and nil metadata", call.Locations[0])
	}

	entries := make([]sdk.PlanEntry, maxACPPlanEntries+1)
	for i := range entries {
		entries[i] = sdk.PlanEntry{Content: huge, Meta: map[string]any{"payload": huge}}
	}
	if err := handler.SessionUpdate(context.Background(), sdk.SessionNotification{Update: sdk.UpdatePlan(entries...)}); err != nil {
		t.Fatal(err)
	}

	message = <-messages
	plan, ok := message.(AgentPlanMsg)
	if !ok {
		t.Fatalf("message = %T, want AgentPlanMsg", message)
	}
	if len(plan.Entries) != maxACPPlanEntries {
		t.Fatalf("plan entries = %d, want cap %d", len(plan.Entries), maxACPPlanEntries)
	}
	if len(plan.Entries[0].Content) > maxACPMetadataBytes || plan.Entries[0].Meta != nil {
		t.Fatalf("plan entry = %#v, want bounded content and nil metadata", plan.Entries[0])
	}
}

func TestCreateTerminalRejectsWorkspaceSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	handler := newClientHandler(make(chan tea.Msg), root)

	if _, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Cwd: &escape}); err == nil {
		t.Fatal("CreateTerminal() succeeded with a symlinked cwd outside workspace")
	}
}

func TestCreateTerminalUsesSanitizedEnvironment(t *testing.T) {
	t.Setenv("TEAK_ACP_SECRET", "must-not-leak")
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())

	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", `printf '%s:%s' "$TEAK_ACP_SECRET" "$REQUEST_VALUE"`},
		Env:     []sdk.EnvVariable{{Name: "REQUEST_VALUE", Value: "allowed"}},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})

	if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
	output, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: resp.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput() error = %v", err)
	}
	if output.Output != ":allowed" {
		t.Fatalf("TerminalOutput().Output = %q, want %q", output.Output, ":allowed")
	}
}

func TestCreateTerminalRejectsUnsafeLimitsAndEnvironment(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	zero := 0
	for _, request := range []sdk.CreateTerminalRequest{
		{Command: "sh", OutputByteLimit: &zero},
		{Command: "sh", Env: []sdk.EnvVariable{{Name: "BAD=NAME", Value: "value"}}},
		{Command: "sh", Env: []sdk.EnvVariable{{Name: "DUPLICATE", Value: "one"}, {Name: "DUPLICATE", Value: "two"}}},
	} {
		if _, err := handler.CreateTerminal(context.Background(), request); err == nil {
			t.Fatalf("CreateTerminal(%+v) succeeded, want validation error", request)
		}
	}
}

func TestCreateTerminalBoundsArgumentsAndEnvironment(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	argvTooLarge := make([]string, 9)
	for i := range argvTooLarge {
		argvTooLarge[i] = strings.Repeat("x", maxTerminalArgBytes)
	}

	for name, args := range map[string][]string{
		"too many arguments": make([]string, maxTerminalArgs+1),
		"argument too large": []string{strings.Repeat("x", maxTerminalArgBytes+1)},
		"argv too large":     argvTooLarge,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
				Command: "true",
				Args:    args,
			}); err == nil || !strings.Contains(err.Error(), "terminal arguments") {
				t.Fatalf("CreateTerminal() error = %v, want bounded terminal arguments error", err)
			}
		})
	}

	tooManyEnv := make([]sdk.EnvVariable, maxTerminalEnvVars+1)
	for i := range tooManyEnv {
		tooManyEnv[i] = sdk.EnvVariable{Name: fmt.Sprintf("TEAK_%d", i), Value: "ok"}
	}
	environmentTooLarge := make([]sdk.EnvVariable, 4)
	for i := range environmentTooLarge {
		environmentTooLarge[i] = sdk.EnvVariable{Name: fmt.Sprintf("TEAK_LARGE_%d", i), Value: strings.Repeat("x", maxTerminalEnvValueBytes)}
	}
	for name, env := range map[string][]sdk.EnvVariable{
		"too many variables":    tooManyEnv,
		"value too large":       []sdk.EnvVariable{{Name: "TEAK_LARGE", Value: strings.Repeat("x", maxTerminalEnvValueBytes+1)}},
		"environment too large": environmentTooLarge,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
				Command: "true",
				Env:     env,
			}); err == nil || !strings.Contains(err.Error(), "terminal environment") {
				t.Fatalf("CreateTerminal() error = %v, want bounded terminal environment error", err)
			}
		})
	}

	response, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "true",
		Args:    []string{"ok"},
		Env:     []sdk.EnvVariable{{Name: "TEAK_SMALL", Value: "ok"}},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() rejected bounded invocation: %v", err)
	}
	if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTerminalCapsOutputAtUTF8Boundary(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	limit := 17
	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command:         "sh",
		Args:            []string{"-c", "i=0; while [ $i -lt 200 ]; do printf 'é'; i=$((i+1)); done"},
		OutputByteLimit: &limit,
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})

	if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
	output, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: resp.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput() error = %v", err)
	}
	if len(output.Output) > limit {
		t.Fatalf("captured %d bytes, limit is %d", len(output.Output), limit)
	}
	if !output.Truncated {
		t.Fatal("TerminalOutput().Truncated = false, want true")
	}
	if !utf8.ValidString(output.Output) {
		t.Fatalf("TerminalOutput().Output is invalid UTF-8: %q", output.Output)
	}
}

func TestWaitForTerminalExitHonorsCancellation(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = handler.WaitForTerminalExit(ctx, sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitForTerminalExit() error = %v, want context.Canceled", err)
	}
}

func TestCreateTerminalContextStopsCommand(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := handler.CreateTerminal(ctx, sdk.CreateTerminalRequest{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})

	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if _, err := handler.WaitForTerminalExit(waitCtx, sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
}

func TestCreateTerminalLimitsTrackedTerminals(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	terminals := make([]string, 0, maxTerminalCount)
	t.Cleanup(func() {
		for _, id := range terminals {
			_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: id})
		}
	})

	for range maxTerminalCount {
		resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
			Command: "sleep",
			Args:    []string{"10"},
		})
		if err != nil {
			t.Fatalf("CreateTerminal() error = %v", err)
		}
		terminals = append(terminals, resp.TerminalId)
	}
	if _, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Args: []string{"-c", "true"}}); err == nil || !strings.Contains(err.Error(), "terminal limit") {
		t.Fatalf("CreateTerminal() error = %v, want terminal limit error", err)
	}
}

func TestCreateTerminalPrunesOldestCompletedTerminalUnderPressure(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	ids := make([]string, 0, maxTerminalCount)

	for range maxTerminalCount {
		resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
			Command: "sh",
			Args:    []string{"-c", "printf done"},
		})
		if err != nil {
			t.Fatalf("CreateTerminal() error = %v", err)
		}
		ids = append(ids, resp.TerminalId)
		if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
			t.Fatalf("WaitForTerminalExit() error = %v", err)
		}
	}

	// Completed terminals remain queryable until space is actually needed.
	if _, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: ids[0]}); err != nil {
		t.Fatalf("TerminalOutput() before pressure error = %v", err)
	}

	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", "true"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() under completed-terminal pressure error = %v", err)
	}
	defer func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	}()

	if _, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: ids[0]}); err == nil {
		t.Fatal("oldest completed terminal remained after capacity pruning")
	}
	if output, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: ids[1]}); err != nil || output.Output != "done" {
		t.Fatalf("newer completed terminal = %#v, %v; want retained output", output, err)
	}
}

func TestCreateTerminalPressureNeverPrunesActiveTerminal(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	active, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: active.TerminalId})
	}()

	for range maxTerminalCount - 1 {
		resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Args: []string{"-c", "true"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Args: []string{"-c", "true"}}); err != nil {
		t.Fatalf("CreateTerminal() should reclaim a completed terminal: %v", err)
	}
	if _, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: active.TerminalId}); err != nil {
		t.Fatalf("active terminal was pruned: %v", err)
	}
}

func TestCreateTerminalPruningIsSafeWithConcurrentOutputReaders(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	ids := make([]string, 0, maxTerminalCount)
	for range maxTerminalCount {
		resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Args: []string{"-c", "printf done"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, resp.TerminalId)
		if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
			t.Fatal(err)
		}
	}

	var readers sync.WaitGroup
	for _, id := range ids {
		readers.Add(1)
		go func(id string) {
			defer readers.Done()
			for range 100 {
				// A pressure prune may make a previous terminal unavailable, which
				// is valid; this exercises the concurrent map/state access.
				_, _ = handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: id})
			}
		}(id)
	}

	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: "sh", Args: []string{"-c", "true"}})
	if err != nil {
		t.Fatalf("CreateTerminal() under concurrent readers error = %v", err)
	}
	_, _ = handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId})
	readers.Wait()
}

func TestCreateTerminalAllowsWorkspaceExecutable(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "command.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ok"), 0o700); err != nil {
		t.Fatal(err)
	}
	handler := newClientHandler(make(chan tea.Msg), root)
	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{Command: script})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})
	if _, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
	output, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: resp.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput() error = %v", err)
	}
	if output.Output != "ok" {
		t.Fatalf("TerminalOutput().Output = %q, want ok", output.Output)
	}
}

func TestTerminalOutputAndWaitAreRaceSafe(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	resp, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", "i=0; while [ $i -lt 200 ]; do printf x; i=$((i+1)); done"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: resp.TerminalId})
	})

	done := make(chan error, 1)
	go func() {
		_, err := handler.WaitForTerminalExit(context.Background(), sdk.WaitForTerminalExitRequest{TerminalId: resp.TerminalId})
		done <- err
	}()
	for range 20 {
		if _, err := handler.TerminalOutput(context.Background(), sdk.TerminalOutputRequest{TerminalId: resp.TerminalId}); err != nil {
			t.Fatalf("TerminalOutput() error = %v", err)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("WaitForTerminalExit() error = %v", err)
	}
}
