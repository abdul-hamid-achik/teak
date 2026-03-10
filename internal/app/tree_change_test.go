package app

import (
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
