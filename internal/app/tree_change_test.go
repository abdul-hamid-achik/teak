package app

import (
	"os"
	"path/filepath"
	"testing"

	"teak/internal/config"
	"teak/internal/git"
)

func TestHandleTreeChangeDebouncesGitRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", tmpDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.logFile != nil {
		defer model.logFile.Close()
	}
	model.gitPanel.SetIsGitRepo(true)

	updatedModel, _ := model.handleTreeChange(TreeChangedMsg{Dir: tmpDir})
	updated := updatedModel.(Model)
	if updated.gitRefreshGeneration != 1 {
		t.Fatalf("gitRefreshGeneration = %d, want 1", updated.gitRefreshGeneration)
	}

	updatedModel, _ = updated.handleTreeChange(TreeChangedMsg{Dir: tmpDir})
	updated = updatedModel.(Model)
	if updated.gitRefreshGeneration != 2 {
		t.Fatalf("gitRefreshGeneration = %d, want 2", updated.gitRefreshGeneration)
	}

	staleModel, staleCmd := updated.Update(gitRefreshDebounceMsg{generation: 1})
	if staleCmd != nil {
		t.Fatal("expected stale debounce message to be ignored")
	}

	updated = staleModel.(Model)
	_, refreshCmd := updated.Update(gitRefreshDebounceMsg{generation: 2})
	if refreshCmd == nil {
		t.Fatal("expected fresh debounce message to trigger git refresh")
	}
	msg := refreshCmd()
	if _, ok := msg.(git.RefreshMsg); !ok {
		t.Fatalf("refreshCmd() returned %T, want git.RefreshMsg", msg)
	}
}

func TestHandleTreeChangePreservesExpandedTreeState(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "testdir")
	childPath := filepath.Join(dirPath, "child.go")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(childPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", tmpDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.logFile != nil {
		defer model.logFile.Close()
	}

	updatedTree, cmd := model.tree.ToggleEntry(dirPath)
	model.tree = updatedTree
	if cmd == nil {
		t.Fatal("expected directory expansion command")
	}
	expandedTree, followup := model.tree.Update(cmd())
	if followup != nil {
		t.Fatal("expected nil follow-up command after handling directory expansion")
	}
	model.tree = expandedTree

	updatedModel, _ := model.handleTreeChange(TreeChangedMsg{Dir: tmpDir})
	updated := updatedModel.(Model)

	if !updated.tree.Entries[0].Expanded {
		t.Fatal("expected tree change refresh to preserve expanded state")
	}
	if len(updated.tree.Entries[0].Children) != 1 {
		t.Fatalf("expected 1 child after tree refresh, got %d", len(updated.tree.Entries[0].Children))
	}
}

func TestFileListMsgIgnoresStaleGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", tmpDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.logFile != nil {
		defer model.logFile.Close()
	}

	model.fileListGeneration = 2
	model.cachedFiles = []string{"fresh.go"}
	model.cachedFilesReady = true

	updatedModel, _ := model.Update(FileListMsg{Files: []string{"stale.go"}, Generation: 1})
	updated := updatedModel.(Model)
	if len(updated.cachedFiles) != 1 || updated.cachedFiles[0] != "fresh.go" {
		t.Fatalf("stale file list should be ignored, got %v", updated.cachedFiles)
	}

	updatedModel, _ = updated.Update(FileListMsg{Files: []string{"new.go"}, Generation: 2})
	updated = updatedModel.(Model)
	if len(updated.cachedFiles) != 1 || updated.cachedFiles[0] != "new.go" {
		t.Fatalf("fresh file list should replace cache, got %v", updated.cachedFiles)
	}
}
