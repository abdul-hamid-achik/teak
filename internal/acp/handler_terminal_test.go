package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
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
