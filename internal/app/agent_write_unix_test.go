//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"teak/internal/acp"
	"teak/internal/agent"
)

func TestAgentWriteDecisionHonorsRestrictiveUmaskForNewFiles(t *testing.T) {
	oldMask := syscall.Umask(0o077)
	t.Cleanup(func() {
		syscall.Umask(oldMask)
	})

	root := t.TempDir()
	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "private.txt",
			Content:    "private",
			ResponseCh: make(chan error, 1),
		},
	}
	if _, err := runAgentWriteDecision(t, testModel(modelState{rootDir: root}), decision); err != nil {
		t.Fatalf("accepted write error = %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "private.txt"))
	if err != nil {
		t.Fatalf("Stat(private.txt) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("private.txt mode = %04o, want 0600 under umask 077", got)
	}
}

func TestAgentWriteDecisionPreservesExistingFileMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatalf("WriteFile(existing.txt) error = %v", err)
	}

	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "existing.txt",
			Content:    "after",
			ResponseCh: make(chan error, 1),
		},
	}
	if _, err := runAgentWriteDecision(t, testModel(modelState{rootDir: root}), decision); err != nil {
		t.Fatalf("accepted write error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(existing.txt) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("existing.txt mode = %04o, want preserved 0600", got)
	}
}
