//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package text

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestNewBufferFromFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := NewBufferFromFile(path)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrBufferFileNotRegular) {
			t.Fatalf("NewBufferFromFile(FIFO) error = %v, want ErrBufferFileNotRegular", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("NewBufferFromFile(FIFO) blocked waiting for a writer")
	}
}
