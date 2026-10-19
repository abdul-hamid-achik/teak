package lsp

import (
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
)

type failingWriteCloser struct{ err error }

func (w failingWriteCloser) Write([]byte) (int, error) { return 0, w.err }
func (w failingWriteCloser) Close() error              { return nil }

// A server that has already exited fails a write with a broken or closed pipe,
// or fails the read that follows with EOF; which one wins is a race. Both must
// read the same, or `teak doctor` reports a different handshake error from one
// run to the next.
func TestSendEncodedReportsExitedServerAsNotRunning(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"broken pipe", &os.PathError{Op: "write", Path: "|1", Err: syscall.EPIPE}},
		{"closed pipe", io.ErrClosedPipe},
		{"closed file", &os.PathError{Op: "write", Path: "|1", Err: os.ErrClosed}},
		{"windows pipe ended", errors.New("write |1: The pipe is being closed.")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{stdin: failingWriteCloser{err: tt.err}}
			err := c.sendEncoded([]byte(`{}`))
			if !errors.Is(err, ErrClientNotRunning) {
				t.Fatalf("sendEncoded() error = %v, want ErrClientNotRunning", err)
			}
			if err.Error() != ErrClientNotRunning.Error() {
				t.Fatalf("sendEncoded() error text = %q, want %q", err, ErrClientNotRunning)
			}
		})
	}
}

func TestSendEncodedKeepsOtherWriteErrors(t *testing.T) {
	want := errors.New("disk quota exceeded")
	c := &Client{stdin: failingWriteCloser{err: want}}
	if err := c.sendEncoded([]byte(`{}`)); !errors.Is(err, want) || errors.Is(err, ErrClientNotRunning) {
		t.Fatalf("sendEncoded() error = %v, want %v unchanged", err, want)
	}
}
