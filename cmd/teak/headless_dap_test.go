package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestHeadlessDAPStatusIsReadOnlyAndStructured(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := runHeadlessCLI([]string{"dap", "status", "--json", "--root", root}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("runHeadlessCLI() code = %d, stderr = %q", code, stderr.String())
	}
	var response headlessDAPResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, stdout.String())
	}
	if response.Workspace != root || len(response.Adapters) != 1 {
		t.Fatalf("response = %#v", response)
	}
	adapter := response.Adapters[0]
	if adapter.Type != "go" || adapter.Command != "dlv" || adapter.State == "" {
		t.Fatalf("adapter = %#v", adapter)
	}
}

func TestHeadlessDAPProbeReportsMissingAdapter(t *testing.T) {
	response := collectHeadlessDAPProbe(t.TempDir(), "/definitely/missing/teak-dap", nil)
	if response.State != "missing" || response.Ready || response.Detail == "" {
		t.Fatalf("probe response = %#v", response)
	}
}

func TestRunHeadlessDAPStageReportsTimeout(t *testing.T) {
	err, timedOut := runHeadlessDAPStage(time.Millisecond, func() error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	if !timedOut || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runHeadlessDAPStage() = err:%v timedOut:%t, want deadline/true", err, timedOut)
	}
}

func TestHeadlessDAPProbePreservesParentCancellation(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "hanging-dap")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hanging-dap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	result := make(chan headlessDAPProbeResponse, 1)
	go func() {
		result <- collectHeadlessDAPProbeContext(ctx, root, "hanging-dap", nil)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case response := <-result:
		if response.State != "cancelled" || response.Ready {
			t.Fatalf("cancelled DAP probe = %#v, want cancelled and not ready", response)
		}
		if response.Detail != "debug adapter initialization was cancelled" {
			t.Fatalf("cancelled DAP probe detail = %q, want structured cancellation", response.Detail)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("cancelled DAP probe did not return promptly")
	}
}

func TestHeadlessDAPProbePreCancelledContextIsStructured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLIContext(ctx, []string{
		"dap", "probe", "--json", "--root", t.TempDir(), "--adapter", "missing-dap",
	}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("pre-cancelled DAP probe code = %d, want 1; stderr=%q", code, stderr.String())
	}
	var response headlessDAPProbeResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode pre-cancelled DAP probe: %v; stdout=%q", err, stdout.String())
	}
	if response.State != "cancelled" || response.Ready {
		t.Fatalf("pre-cancelled DAP probe = %#v, want cancelled and not ready", response)
	}
	if stderr.Len() != 0 {
		t.Fatalf("pre-cancelled DAP probe wrote stderr: %q", stderr.String())
	}
}

func TestHeadlessDAPStatusPreservesCancelledVersionProbe(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "dlv")
	marker := filepath.Join(dir, "version-probe-started")
	script := "#!/bin/sh\n: > " + marker + "\nif [ \"$1\" = \"version\" ]; then sleep 10; fi\n"
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"dlv": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	result := make(chan headlessDAPResponse, 1)
	go func() {
		result <- collectHeadlessDAPStatusContext(ctx, root)
	}()
	// The full cmd/teak package starts many subprocess fixtures; allow the
	// marker observation to tolerate scheduler contention without weakening the
	// cancellation assertion below.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("version probe did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	response := <-result
	if len(response.Adapters) != 1 {
		t.Fatalf("adapters = %#v", response.Adapters)
	}
	entry := response.Adapters[0]
	if entry.State != "cancelled" || entry.VersionProbe != "cancelled" || !entry.Available {
		t.Fatalf("cancelled DAP status entry = %#v, want cancelled probe", entry)
	}
	if entry.Hint != "version probe was cancelled" {
		t.Fatalf("cancelled DAP status hint = %q, want structured cancellation", entry.Hint)
	}
}

func TestHeadlessDAPProbeCompletesInitialize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses the checked-in Python adapter")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 is required by the integration fixture: %v", err)
	}
	fixture := filepath.Join("..", "..", "testdata", "glyphrun", "dap-probe-fixture.py")
	response := collectHeadlessDAPProbe(t.TempDir(), python, []string{fixture})
	if response.State != "ready" || !response.Ready || response.Detail == "" {
		t.Fatalf("probe response = %#v, want successful initialize", response)
	}
}

func TestHeadlessDAPProbeReportsAdapterExitDuringInitialize(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "exiting-dap")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"exiting-dap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	response := collectHeadlessDAPProbe(t.TempDir(), "exiting-dap", nil)
	if response.State != "failed" || response.Ready || response.Detail == "" {
		t.Fatalf("probe response = %#v, want failed initialize", response)
	}
}

func TestHeadlessDAPStatusReportsAdapterVersion(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "dlv")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'dlv version fixture'; exit 0; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"dlv": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	response := collectHeadlessDAPStatus(t.TempDir())
	if len(response.Adapters) != 1 {
		t.Fatalf("adapters = %#v", response.Adapters)
	}
	entry := response.Adapters[0]
	if entry.Version != "dlv version fixture" || entry.VersionProbe != "ready" {
		t.Fatalf("adapter = %#v, want verified version", entry)
	}
}

func TestHeadlessDAPStatusDoesNotClaimAvailableAfterVersionProbeFailure(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "dlv")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'broken dlv' >&2; exit 7; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"dlv": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	entry := collectHeadlessDAPStatus(t.TempDir()).Adapters[0]
	if entry.State != "failed" || !entry.Available || entry.VersionProbe != "failed" {
		t.Fatalf("adapter = %#v, want failed probe", entry)
	}
}
