package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestIsExpectedShutdownError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "client not running",
			err:  errClientNotRunning,
			want: true,
		},
		{
			name: "wrapped client not running",
			err:  fmt.Errorf("wrap: %w", errClientNotRunning),
			want: true,
		},
		{
			name: "io eof",
			err:  io.EOF,
			want: true,
		},
		{
			name: "os closed",
			err:  os.ErrClosed,
			want: true,
		},
		{
			name: "broken pipe string",
			err:  errors.New("write: broken pipe"),
			want: true,
		},
		{
			name: "closed pipe string",
			err:  errors.New("io: read/write on closed pipe"),
			want: true,
		},
		{
			name: "unexpected error",
			err:  errors.New("timeout while waiting for response"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExpectedShutdownError(tt.err)
			if got != tt.want {
				t.Fatalf("isExpectedShutdownError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientShutdownReturnsBeforeAStuckServerExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLSPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_LSP_SHUTDOWN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		pending:     make(map[int]chan callResult),
		running:     true,
		cancelRead:  func() {},
		processDone: make(chan struct{}),
	}
	go client.reapProcess()

	started := time.Now()
	client.Shutdown()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Shutdown() blocked for %s", elapsed)
	}
	if !lspWaitForDone(client.processDone, 4*time.Second) {
		t.Fatal("server was not killed and reaped")
	}
}

func TestManagerWaitForShutdownReapsTrackedServers(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLSPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_LSP_SHUTDOWN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		pending:     make(map[int]chan callResult),
		running:     true,
		cancelRead:  func() {},
		processDone: make(chan struct{}),
	}
	go client.reapProcess()

	manager := NewManager(t.TempDir(), nil)
	manager.clients["helper"] = client
	manager.ShutdownAll()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if !manager.WaitForShutdown(ctx) {
		t.Fatal("WaitForShutdown() returned before the server was reaped")
	}
	if cmd.ProcessState == nil {
		t.Fatal("server process was not reaped")
	}
}

func TestLSPShutdownHelperProcess(t *testing.T) {
	if os.Getenv("TEAK_LSP_SHUTDOWN_HELPER") != "1" {
		return
	}
	select {}
}
