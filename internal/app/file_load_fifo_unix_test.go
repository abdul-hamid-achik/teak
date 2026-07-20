//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadEditorFileRejectsFIFOWIthoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.go")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := readEditorFile(context.Background(), path)
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, errEditorFileNotRegular) {
			t.Fatalf("readEditorFile(FIFO) error = %v, want %v", err, errEditorFileNotRegular)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("readEditorFile(FIFO) blocked waiting for a writer")
	}
}
