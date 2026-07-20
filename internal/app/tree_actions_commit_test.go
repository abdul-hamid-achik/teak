package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTreeCreateReportsCommittedWhenCancelledAfterMutation(t *testing.T) {
	previous := treeActionAfterCommit
	t.Cleanup(func() { treeActionAfterCommit = previous })

	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.newFileMode = true
	m.newItemDir = root
	m.newItemInput = "created.go"
	updatedAny, cmd := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	treeActionAfterCommit = func(treeActionRequest) { updated.treeActionCancel() }

	result := cmd().(treeActionResultMsg)
	if !result.Committed || errors.Is(result.Err, context.Canceled) {
		t.Fatalf("create result = %#v, want committed non-cancelled outcome", result)
	}
	if _, err := os.Stat(filepath.Join(root, "created.go")); err != nil {
		t.Fatalf("committed create missing from disk: %v", err)
	}
	completedAny, _ := updated.Update(result)
	if completed := completedAny.(Model); !strings.HasPrefix(completed.status, "Created: created.go") {
		t.Fatalf("status = %q, want committed create", completed.status)
	}
}

func TestTreeDeleteReportsCommittedWhenCancelledAfterRename(t *testing.T) {
	previous := treeActionAfterCommit
	t.Cleanup(func() { treeActionAfterCommit = previous })

	root := t.TempDir()
	path := filepath.Join(root, "removed.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = path
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	treeActionAfterCommit = func(treeActionRequest) {
		updated.treeActionCancel()
		// The command has already opened an independent Root. Closing the
		// model-owned root here simulates shutdown during post-commit cleanup.
		if updated.agentWriteRoot != nil {
			_ = updated.agentWriteRoot.Close()
		}
	}

	result := cmd().(treeActionResultMsg)
	if !result.Committed || errors.Is(result.Err, context.Canceled) {
		t.Fatalf("delete result = %#v, want committed non-cancelled outcome", result)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("committed delete left target in workspace: %v", err)
	}
	assertNoTreeTrashArtifacts(t, root)
	completedAny, _ := updated.Update(result)
	if completed := completedAny.(Model); !strings.HasPrefix(completed.status, "Deleted: removed.go") {
		t.Fatalf("status = %q, want committed delete", completed.status)
	}
}

func TestTreeDeleteKeepsBufferThatBecomesDirtyDuringFlight(t *testing.T) {
	previous := runTreeAction
	t.Cleanup(func() { runTreeAction = previous })

	root := t.TempDir()
	path := filepath.Join(root, "active.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	editorIndex := addDirtyEditor(t, &m, "active.go", "package p\n", "package p\n")

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	runTreeAction = func(ctx context.Context, request treeActionRequest) treeActionOutcome {
		started <- struct{}{}
		<-release
		return executeTreeAction(ctx, request)
	}
	m.deleteConfirm = true
	m.deleteTarget = path
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	results := make(chan treeActionResultMsg, 1)
	go func() { results <- cmd().(treeActionResultMsg) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delete command did not start")
	}
	updated.editors[editorIndex].Buffer.InsertAtCursor([]byte("// unsaved\n"))
	if !updated.editors[editorIndex].Buffer.Dirty() {
		t.Fatal("test did not make buffer dirty")
	}
	close(release)
	select {
	case result := <-results:
		completedAny, _ := updated.Update(result)
		completed := completedAny.(Model)
		if completed.tabBar.FindTab(path) < 0 || !completed.editors[editorIndex].Buffer.Dirty() {
			t.Fatal("delete completion closed a buffer modified during the operation")
		}
		if !strings.Contains(completed.status, "kept 1 modified buffer") {
			t.Fatalf("status = %q, want preserved-buffer warning", completed.status)
		}
	case <-time.After(time.Second):
		t.Fatal("delete command did not complete")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("delete did not commit: %v", err)
	}
}

func TestTreeDeleteLargeDirectoryUsesIterativeCleanupBudget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "large")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < treeCleanupNodeBudget*2+1; i++ {
		if err := os.WriteFile(filepath.Join(target, fmt.Sprintf("file-%03d", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = target
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	completed := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("large target survived delete: %v", err)
	}
	assertNoTreeTrashArtifacts(t, root)
	if !strings.HasPrefix(completed.status, "Deleted: large") {
		t.Fatalf("status = %q, want successful logical delete", completed.status)
	}
}

func TestTreeDeleteDeepDirectoryUsesBoundedIterativeCleanup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "deep")
	current := target
	for i := 0; i < 64; i++ {
		current = filepath.Join(current, "d")
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "leaf.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = target
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	completed := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("deep target survived delete: %v", err)
	}
	assertNoTreeTrashArtifacts(t, root)
	if !strings.HasPrefix(completed.status, "Deleted: deep") {
		t.Fatalf("status = %q, want successful logical delete", completed.status)
	}
}

func TestTreeDeleteDeepDirectoryBeyondLegacyFrameLimitCompletes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "very-deep")
	current := target
	// Keep every component short so the test remains below common PATH_MAX
	// limits while exceeding the former descriptor-frame cap of 256.
	for i := 0; i < 288; i++ {
		current = filepath.Join(current, "d")
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "leaf.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = target
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	completed := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("deep target survived delete: %v", err)
	}
	assertNoTreeTrashArtifacts(t, root)
	if !strings.HasPrefix(completed.status, "Deleted: very-deep") {
		t.Fatalf("status = %q, want successful logical delete", completed.status)
	}
}

func TestTreeDeleteRejectsWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = root
	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	if cmd != nil || !strings.Contains(updated.status, "workspace root") {
		t.Fatalf("root delete = cmd %v status %q, want rejection", cmd != nil, updated.status)
	}
}

func assertNoTreeTrashArtifacts(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".teak-trash-") {
			t.Fatalf("delete left trash artifact %q", entry.Name())
		}
	}
}
