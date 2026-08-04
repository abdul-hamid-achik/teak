package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"

	agentruntime "teak/internal/agent/runtime"
)

func TestReadTextFileRoutesThroughBubbleTeaAndHonorsCancellation(t *testing.T) {
	t.Run("returns queued result", func(t *testing.T) {
		messages := make(chan tea.Msg, 1)
		handler := newClientHandler(messages, t.TempDir())
		result := make(chan struct {
			response sdk.ReadTextFileResponse
			err      error
		}, 1)

		go func() {
			response, err := handler.ReadTextFile(context.Background(), sdk.ReadTextFileRequest{Path: "/workspace/main.go"})
			result <- struct {
				response sdk.ReadTextFileResponse
				err      error
			}{response: response, err: err}
		}()

		message := <-messages
		request, ok := message.(FileReadRequestMsg)
		if !ok {
			t.Fatalf("queued message = %T, want FileReadRequestMsg", message)
		}
		if request.Path != "/workspace/main.go" || request.RootDir != handler.rootDir || request.ResultCh == nil {
			t.Fatalf("file read request = %#v, want path/root/result channel", request)
		}
		request.ResultCh <- FileReadResult{Content: "package main"}

		select {
		case got := <-result:
			if got.err != nil || got.response.Content != "package main" {
				t.Fatalf("ReadTextFile() = %#v, want content", got)
			}
		case <-time.After(time.Second):
			t.Fatal("ReadTextFile() did not return after the UI response")
		}
	})

	t.Run("propagates UI error", func(t *testing.T) {
		messages := make(chan tea.Msg, 1)
		handler := newClientHandler(messages, t.TempDir())
		result := make(chan error, 1)
		go func() {
			_, err := handler.ReadTextFile(context.Background(), sdk.ReadTextFileRequest{Path: "main.go"})
			result <- err
		}()
		request := (<-messages).(FileReadRequestMsg)
		want := errors.New("read failed")
		request.ResultCh <- FileReadResult{Err: want}
		if got := <-result; !errors.Is(got, want) {
			t.Fatalf("ReadTextFile() error = %v, want %v", got, want)
		}
	})

	t.Run("cancels before enqueue", func(t *testing.T) {
		messages := make(chan tea.Msg, 1)
		handler := newClientHandler(messages, t.TempDir())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := handler.ReadTextFile(ctx, sdk.ReadTextFileRequest{Path: "main.go"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadTextFile() error = %v, want context.Canceled", err)
		}
		select {
		case message := <-messages:
			t.Fatalf("cancelled read queued %T", message)
		default:
		}
	})

	t.Run("cancels while waiting for UI", func(t *testing.T) {
		messages := make(chan tea.Msg, 1)
		handler := newClientHandler(messages, t.TempDir())
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := handler.ReadTextFile(ctx, sdk.ReadTextFileRequest{Path: "main.go"})
			result <- err
		}()
		request := (<-messages).(FileReadRequestMsg)
		cancel()
		if got := <-result; !errors.Is(got, context.Canceled) {
			t.Fatalf("ReadTextFile() error = %v, want context.Canceled", got)
		}
		// The channel is buffered so a late UI response cannot block cleanup.
		request.ResultCh <- FileReadResult{Content: "late"}
	})
}

func TestACPFileOperationsRecordSafeRuntimeAudit(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "audit file operations",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true, Write: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	hMessages := make(chan tea.Msg, 1)
	handler := newClientHandler(hMessages, root)
	handler.setRuntime(runManager, func() agentruntime.RunID { return run.ID })

	readResult := make(chan struct {
		response sdk.ReadTextFileResponse
		err      error
	}, 1)
	go func() {
		response, readErr := handler.ReadTextFile(context.Background(), sdk.ReadTextFileRequest{Path: "private/secret.txt"})
		readResult <- struct {
			response sdk.ReadTextFileResponse
			err      error
		}{response: response, err: readErr}
	}()
	readRequest := (<-hMessages).(FileReadRequestMsg)
	readRequest.ResultCh <- FileReadResult{Content: "model-secret"}
	if result := <-readResult; result.err != nil || result.response.Content != "model-secret" {
		t.Fatalf("ReadTextFile() = %#v, want successful response", result)
	}

	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := handler.WriteTextFile(context.Background(), sdk.WriteTextFileRequest{
			Path:    "private/secret.txt",
			Content: "model-secret",
		})
		writeResult <- writeErr
	}()
	writeRequest := (<-hMessages).(AgentWriteFileMsg)
	writeRequest.ResponseCh <- nil
	if err := <-writeResult; err != nil {
		t.Fatalf("WriteTextFile() error = %v", err)
	}

	record, err := runManager.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Audit) != 2 {
		t.Fatalf("file audit = %#v, want read and write entries", record.Audit)
	}
	for index, want := range []struct {
		operation string
		detail    string
	}{
		{operation: "file_read", detail: "bytes=12"},
		{operation: "file_write", detail: "bytes=12"},
	} {
		entry := record.Audit[index]
		if entry.Operation != want.operation || entry.Outcome != "completed" || entry.Detail != want.detail {
			t.Fatalf("audit[%d] = %#v, want completed %s/%s", index, entry, want.operation, want.detail)
		}
	}
	encoded, err := json.Marshal(record.Audit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private/secret.txt") || strings.Contains(string(encoded), "model-secret") {
		t.Fatalf("file audit retained path or content: %s", encoded)
	}
	if err := runManager.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestKillTerminalCommandTerminatesTrackedProcess(t *testing.T) {
	handler := newClientHandler(make(chan tea.Msg), t.TempDir())
	response, err := handler.CreateTerminal(context.Background(), sdk.CreateTerminalRequest{
		Command: "sh",
		Args:    []string{"-c", "sleep 10"},
	})
	if err != nil {
		t.Fatalf("CreateTerminal() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.ReleaseTerminal(context.Background(), sdk.ReleaseTerminalRequest{TerminalId: response.TerminalId})
	})

	if _, err := handler.KillTerminalCommand(context.Background(), sdk.KillTerminalCommandRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatalf("KillTerminalCommand() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := handler.WaitForTerminalExit(ctx, sdk.WaitForTerminalExitRequest{TerminalId: response.TerminalId}); err != nil {
		t.Fatalf("WaitForTerminalExit() after kill = %v", err)
	}
	if _, err := handler.KillTerminalCommand(context.Background(), sdk.KillTerminalCommandRequest{TerminalId: "missing"}); err == nil || !strings.Contains(err.Error(), "unknown terminal") {
		t.Fatalf("KillTerminalCommand(missing) error = %v, want unknown terminal", err)
	}
}

func TestACPMetadataCapPreservesUTF8AndNil(t *testing.T) {
	if capACPMetadata(nil) != nil {
		t.Fatal("capACPMetadata(nil) returned a non-nil pointer")
	}
	short := "title"
	if got := capACPMetadata(&short); got == nil || *got != short {
		t.Fatalf("short metadata = %#v, want %q", got, short)
	}
	long := strings.Repeat("界", maxACPMetadataBytes)
	got := capACPMetadata(&long)
	if got == nil || len(*got) > maxACPMetadataBytes {
		t.Fatalf("long metadata length = %d, want <= %d", len(*got), maxACPMetadataBytes)
	}
	if !utf8.ValidString(*got) {
		t.Fatal("metadata cap split a UTF-8 sequence")
	}
}

func TestStaleACPConnectionCannotReuseNewRunCapability(t *testing.T) {
	root := t.TempDir()
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runManager.Start(agentruntime.RunSpec{
		Objective:             "stale connection fixture",
		Workspace:             root,
		RequestedCapabilities: agentruntime.Capabilities{Read: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runManager.Cancel(run.ID) })

	manager := NewManagerWithRuntime(root, "echo", nil, runManager)
	manager.mu.Lock()
	manager.activeRunID = run.ID
	manager.connectionGeneration = 1
	manager.mu.Unlock()

	handler := newClientHandler(make(chan tea.Msg), root)
	handler.setRuntime(runManager, func() agentruntime.RunID {
		return manager.runtimeRunIDForConnection(1)
	})
	if err := handler.authorize(context.Background(), agentruntime.Capabilities{Read: true}); err != nil {
		t.Fatalf("current ACP connection authorization = %v, want success", err)
	}

	manager.mu.Lock()
	manager.connectionGeneration = 2
	manager.mu.Unlock()
	if err := handler.authorize(context.Background(), agentruntime.Capabilities{Read: true}); err == nil {
		t.Fatal("stale ACP connection reused the current run capability")
	}
}
