//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadWorkspaceEditFileRejectsFIFOWIthoutBlocking(t *testing.T) {
	rootPath := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(rootPath, "stream.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	result := make(chan error, 1)
	go func() {
		_, err := readWorkspaceEditFile(context.Background(), root, "stream.go", maxWorkspaceEditAggregateBytes)
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, errEditorFileNotRegular) {
			t.Fatalf("readWorkspaceEditFile(FIFO) error = %v, want %v", err, errEditorFileNotRegular)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("workspace edit read blocked on FIFO")
	}
}
