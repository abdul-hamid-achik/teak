package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type lspNopWriteCloser struct{ bytes.Buffer }

func (lspNopWriteCloser) Close() error { return nil }

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
			err:  ErrClientNotRunning,
			want: true,
		},
		{
			name: "wrapped client not running",
			err:  fmt.Errorf("wrap: %w", ErrClientNotRunning),
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
			name: "bounded shutdown deadline",
			err:  context.DeadlineExceeded,
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

func TestCancelNotificationErrorExpectedOnlyDuringShutdown(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		shuttingDown bool
		want         bool
	}{
		{name: "closed transport during shutdown", err: ErrClientNotRunning, shuttingDown: true, want: true},
		{name: "closed transport while running", err: ErrClientNotRunning},
		{name: "unexpected shutdown error", err: errors.New("permission denied"), shuttingDown: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExpectedCancelNotificationError(tt.err, tt.shuttingDown); got != tt.want {
				t.Fatalf("isExpectedCancelNotificationError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCallReturnsWhenLSPTransportReadLoopCloses(t *testing.T) {
	client := &Client{
		stdin:       &lspNopWriteCloser{},
		pending:     make(map[int]chan callResult),
		running:     true,
		processDone: make(chan struct{}),
		readDone:    make(chan struct{}),
	}
	close(client.readDone)

	result := make(chan error, 1)
	go func() {
		_, err := client.call(context.Background(), "workspace/symbol", nil)
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, ErrClientNotRunning) {
			t.Fatalf("call() error = %v, want client-not-running after transport EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("call() remained blocked after the LSP read loop closed")
	}
	client.mu.RLock()
	pending := len(client.pending)
	client.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("pending requests = %d, want 0 after transport EOF", pending)
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

func TestReapProcessMarksClientNotReady(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLSPReapExitHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_LSP_REAP_EXIT_HELPER=1")
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
		initialized: true,
		processDone: make(chan struct{}),
	}

	go client.reapProcess()
	// The helper is a separate instrumented test binary under -race; startup
	// and teardown can exceed one second on a busy machine even though the
	// child exits immediately.
	if !lspWaitForDone(client.processDone, 5*time.Second) {
		t.Fatal("server was not reaped")
	}
	if client.IsReady() {
		t.Fatal("reaped client reported ready")
	}
}

func TestLSPReapExitHelperProcess(t *testing.T) {
	if os.Getenv("TEAK_LSP_REAP_EXIT_HELPER") != "1" {
		return
	}
}
