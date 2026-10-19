package lsp

import (
	"os/exec"
	"testing"
	"time"
)

func TestReapProcessDeliversServerExitWhenReaderIsBrieflyBusy(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() error = %v", err)
	}

	// Unbuffered and not yet being received: the exit happens before the UI
	// loop is ready. A non-blocking send would drop the only exit report.
	msgChan := make(chan any)
	c := &Client{
		cmd:         cmd,
		msgChan:     msgChan,
		processDone: make(chan struct{}),
		readDone:    make(chan struct{}),
		running:     true,
	}
	go c.reapProcess()

	time.Sleep(100 * time.Millisecond)

	select {
	case msg := <-msgChan:
		exited, ok := msg.(ServerExitedMsg)
		if !ok {
			t.Fatalf("message = %T, want ServerExitedMsg", msg)
		}
		if exited.Command != "/bin/sh" {
			t.Fatalf("Command = %q, want %q", exited.Command, "/bin/sh")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ServerExitedMsg was dropped: no message delivered after the reader became ready")
	}
}
