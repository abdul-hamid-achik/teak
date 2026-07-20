//go:build windows

package app

import (
	"context"
	"os"
	"testing"
)

func TestAgentWriteWindowsSupportsConfinedReplacement(t *testing.T) {
	if !agentWriteAtomicSupported {
		t.Fatal("Windows agent writes must support confined replacement")
	}

	rootDir := t.TempDir()
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatalf("OpenRoot() error = %v", err)
	}
	t.Cleanup(func() {
		_ = root.Close()
	})

	if err := root.WriteFile("target.txt", []byte("before"), 0o600); err != nil {
		t.Fatalf("WriteFile(target.txt) error = %v", err)
	}
	if err := writeAgentFileAtomicRoot(context.Background(), root, "target.txt", []byte("after")); err != nil {
		t.Fatalf("writeAgentFileAtomicRoot() error = %v", err)
	}

	data, err := root.ReadFile("target.txt")
	if err != nil {
		t.Fatalf("ReadFile(target.txt) error = %v", err)
	}
	if got := string(data); got != "after" {
		t.Fatalf("target content = %q, want %q", got, "after")
	}
}

func TestAgentWriteWindowsRenameStaysWithinRoot(t *testing.T) {
	rootDir := t.TempDir()
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatalf("OpenRoot() error = %v", err)
	}
	t.Cleanup(func() {
		_ = root.Close()
	})

	if err := root.WriteFile("old.txt", []byte("source"), 0o600); err != nil {
		t.Fatalf("WriteFile(old.txt) error = %v", err)
	}
	if err := renameWorkspacePath(root, "old.txt", "new.txt"); err != nil {
		t.Fatalf("renameWorkspacePath() error = %v", err)
	}

	if _, err := root.Stat("old.txt"); !os.IsNotExist(err) {
		t.Fatalf("Stat(old.txt) error = %v, want not exist", err)
	}
	data, err := root.ReadFile("new.txt")
	if err != nil {
		t.Fatalf("ReadFile(new.txt) error = %v", err)
	}
	if got := string(data); got != "source" {
		t.Fatalf("new content = %q, want %q", got, "source")
	}

	if err := renameWorkspacePath(root, "new.txt", "../escaped.txt"); err == nil {
		t.Fatal("renameWorkspacePath() error = nil for an escaping destination")
	}
}
