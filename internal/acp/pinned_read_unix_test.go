//go:build unix

package acp

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadFileFromDiskRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "input.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	if _, err := ReadFileFromDisk(context.Background(), root, fifo, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() accepted a FIFO")
	}
	if _, err := os.Stat(fifo); err != nil {
		t.Fatalf("FIFO disappeared after rejected read: %v", err)
	}
}
