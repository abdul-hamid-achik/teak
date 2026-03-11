package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
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

func TestGitSidebarMouseClickCollapsesDirectoryOnce(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()

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

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updatedModel, _ := model.Update(git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})
	updated := updatedModel.(Model)

	click := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: 2})
	updatedModel, cmd := updated.Update(click)
	if cmd != nil {
		t.Fatal("expected directory click to be handled without a follow-up command")
	}
	updated = updatedModel.(Model)

	if updated.focus != FocusGitPanel {
		t.Fatalf("focus = %v, want %v", updated.focus, FocusGitPanel)
	}
	if updated.gitPanel.Cursor != 0 {
		t.Fatalf("git cursor = %d, want 0", updated.gitPanel.Cursor)
	}

	node, staged := updated.gitPanel.NodeAtY(1)
	if node == nil {
		t.Fatal("expected staged directory node at y=1")
	}
	if !staged {
		t.Fatal("expected clicked directory to remain in staged section")
	}
	if node.Name != "src" || node.Expanded {
		t.Fatalf("expected src directory to be collapsed after one routed click, got name=%q expanded=%v", node.Name, node.Expanded)
	}
}

func TestGitSidebarMouseClickFocusesCommitBody(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()

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

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updatedModel, _ := model.Update(git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})
	updated := updatedModel.(Model)

	bodyY := -1
	for y := 0; y < updated.height; y++ {
		if updated.gitPanel.CommitFormHitTest(y) == "body" {
			bodyY = y
			break
		}
	}
	if bodyY < 0 {
		t.Fatal("expected git panel commit body to be visible")
	}

	click := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: bodyY + 1})
	updatedModel, cmd := updated.Update(click)
	if cmd == nil {
		t.Fatal("expected commit body click to return a focus command")
	}
	updated = updatedModel.(Model)

	if updated.focus != FocusGitPanel {
		t.Fatalf("focus = %v, want %v", updated.focus, FocusGitPanel)
	}
	if !updated.gitPanel.IsBodyFocused() {
		t.Fatal("expected git commit body to be focused after click")
	}
}

func TestGitRefreshMsgPreservesCollapsedDirectoryAfterInteraction(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()

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

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updatedModel, _ := model.Update(git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})
	updated := updatedModel.(Model)

	click := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: 2})
	updatedModel, _ = updated.Update(click)
	updated = updatedModel.(Model)

	updatedModel, _ = updated.Update(git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})
	updated = updatedModel.(Model)

	node, staged := updated.gitPanel.NodeAtY(1)
	if node == nil {
		t.Fatal("expected staged directory node at y=1 after refresh")
	}
	if !staged {
		t.Fatal("expected node to remain in staged section after refresh")
	}
	if node.Name != "src" || node.Expanded {
		t.Fatalf("expected src directory to stay collapsed after refresh, got name=%q expanded=%v", node.Name, node.Expanded)
	}
}
