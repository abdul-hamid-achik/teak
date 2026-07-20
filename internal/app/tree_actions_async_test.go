package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTreeCreateDefersFilesystemMutationUntilCommandCompletion(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.newFileMode = true
	m.newItemDir = root
	m.newItemInput = "created.go"

	updatedAny, cmd := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	path := filepath.Join(root, "created.go")
	if cmd == nil {
		t.Fatal("new tree item did not return an asynchronous command")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Update created the file before its command ran: %v", err)
	}

	result, ok := cmd().(treeActionResultMsg)
	if !ok {
		t.Fatalf("command result = %T, want treeActionResultMsg", result)
	}
	if result.Err != nil {
		t.Fatalf("create result error = %v", result.Err)
	}
	completedAny, _ := updated.Update(result)
	completed := completedAny.(Model)
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("command did not create file: %v", err)
	}
	if completed.status != "Created: created.go" {
		t.Fatalf("status = %q, want create success", completed.status)
	}
}

func TestTreeDeleteDefersFilesystemMutationUntilCommandCompletion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "delete.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = path

	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("delete did not return an asynchronous command")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Update removed the file before its command ran: %v", err)
	}

	result, ok := cmd().(treeActionResultMsg)
	if !ok {
		t.Fatalf("command result = %T, want treeActionResultMsg", result)
	}
	if result.Err != nil {
		t.Fatalf("delete result error = %v", result.Err)
	}
	completedAny, _ := updated.Update(result)
	completed := completedAny.(Model)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("command did not remove file: %v", err)
	}
	if completed.status != "Deleted: delete.go" {
		t.Fatalf("status = %q, want delete success", completed.status)
	}
}

func TestTreeActionRejectsNewMutationWhilePreviousActionIsInFlight(t *testing.T) {
	previous := runTreeAction
	t.Cleanup(func() { runTreeAction = previous })

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	runTreeAction = func(ctx context.Context, request treeActionRequest) treeActionOutcome {
		started <- struct{}{}
		<-release
		return treeActionOutcome{Path: request.Path, Committed: true}
	}

	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.status = "unchanged"
	m.newFileMode = true
	m.newItemDir = root
	m.newItemInput = "slow.go"
	firstAny, firstCmd := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	first := firstAny.(Model)
	if firstCmd == nil {
		t.Fatal("slow create did not return a command")
	}

	results := make(chan tea.Msg, 1)
	go func() { results <- firstCmd() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow tree command did not start")
	}

	// A newer destructive request must return immediately, but it must not
	// cancel or supersede the operation that may already have committed.
	first.newFileMode = true
	first.newItemDir = root
	first.newItemInput = "newer.go"
	currentAny, currentCmd := first.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	current := currentAny.(Model)
	if currentCmd != nil {
		t.Fatal("newer create started while a mutation was in flight")
	}
	if current.treeActionCancel == nil {
		t.Fatal("in-flight action was cancelled by the rejected request")
	}
	if current.status != "Another file operation is already in progress" {
		t.Fatalf("status = %q, want rejection", current.status)
	}
	close(release)
	select {
	case result := <-results:
		completedAny, _ := current.Update(result)
		if completed := completedAny.(Model); completed.status != "Created: slow.go" {
			t.Fatalf("completed status = %q, want first action success", completed.status)
		}
	case <-time.After(time.Second):
		t.Fatal("first tree action did not complete")
	}
}

func TestTreeActionCommandChecksCancellationBeforeFilesystemMutation(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.newFileMode = true
	m.newItemDir = root
	m.newItemInput = "cancelled.go"
	updatedAny, cmd := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	if updated.treeActionCancel == nil || cmd == nil {
		t.Fatal("create did not retain a cancellable action")
	}
	updated.treeActionCancel()
	result := cmd().(treeActionResultMsg)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cancelled result error = %v, want context.Canceled", result.Err)
	}
	if _, err := os.Lstat(filepath.Join(root, "cancelled.go")); !os.IsNotExist(err) {
		t.Fatalf("cancelled action created a file: %v", err)
	}
}
