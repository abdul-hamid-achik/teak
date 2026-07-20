//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/unix"
)

func TestTreeDeleteFIFOCompletesWithoutOpeningIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stream")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = path
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("FIFO delete did not return a command")
	}

	resultCh := make(chan tea.Msg, 1)
	go func() { resultCh <- cmd() }()
	select {
	case result := <-resultCh:
		completedAny, _ := updated.Update(result)
		completed := completedAny.(Model)
		if completed.status != "Deleted: stream" {
			t.Fatalf("status = %q, want delete success", completed.status)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("FIFO delete blocked; tree actions must use lstat/remove without opening the stream")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("FIFO still exists after delete: %v", err)
	}
}
