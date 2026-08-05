package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/filetree"
	"teak/internal/git"
)

func applyGitRefresh(t *testing.T, model Model, msg git.RefreshMsg) Model {
	t.Helper()
	updatedModel, cmd := model.Update(msg)
	updated := updatedModel.(Model)
	for cmd != nil {
		updatedModel, cmd = updated.Update(cmd())
		updated = updatedModel.(Model)
	}
	return updated
}

func TestPreparedGitTreeProjectionRoutesThroughRootModel(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	model.gitPanel.SetSize(40, 20)
	model = applyGitRefresh(t, model, git.RefreshMsg{Entries: []git.StatusEntry{
		{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
	}})

	panel, cmd := model.gitPanel.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, Y: 1}))
	if cmd == nil {
		t.Fatal("directory click did not schedule a tree projection")
	}
	model.gitPanel = panel
	updatedAny, next := model.Update(cmd())
	if next != nil {
		t.Fatal("prepared tree projection unexpectedly scheduled more work")
	}
	updated := updatedAny.(Model)
	node, staged := updated.gitPanel.NodeAtY(1)
	if node == nil || !staged || node.Path != "src" || node.Expanded {
		t.Fatalf("routed projection node = (%+v, staged=%v), want collapsed staged src", node, staged)
	}
}

func loadInitialTreeForTest(t *testing.T, model *Model) {
	t.Helper()
	updated, cmd := model.Update(treeLoadedMsg{Tree: filetree.New(model.rootDir, model.theme)})
	*model = runWorkspaceEditCommands(t, updated.(Model), cmd)
}

func TestNewModelDefersInitialTreeRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	if len(model.tree.Entries) != 0 {
		t.Fatalf("NewModel read the tree synchronously: %v", model.tree.Entries)
	}
	loadInitialTreeForTest(t, &model)
	if len(model.tree.Entries) != 1 {
		t.Fatalf("async tree result entries = %v, want one file", model.tree.Entries)
	}
}

func TestTreeLoadPreparesVisibleProjectionAsynchronously(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	model.tree.SetShowHidden(false)
	model.tree.SetFilter("main")

	loaded := filetree.NewEmpty(root, model.theme)
	loaded.Entries = []filetree.Entry{
		{Name: ".main.secret", Path: filepath.Join(root, ".main.secret")},
		{Name: "main.go", Path: filepath.Join(root, "main.go")},
		{Name: "notes.txt", Path: filepath.Join(root, "notes.txt")},
	}
	updatedAny, cmd := model.Update(treeLoadedMsg{Tree: loaded, Generation: model.treeRefreshGeneration})
	updated := updatedAny.(Model)
	if cmd == nil || !updated.tree.FilterPending() {
		t.Fatal("tree load did not schedule its visible projection")
	}
	if entry := updated.tree.EntryAtY(0); entry != nil {
		t.Fatalf("pending tree load exposed stale entry %#v", entry)
	}

	updated = runWorkspaceEditCommands(t, updated, cmd)
	entry := updated.tree.EntryAtY(0)
	if entry == nil || entry.Name != "main.go" {
		t.Fatalf("prepared loaded entry = %#v, want main.go", entry)
	}
}

func TestTreeFilterReadyMessageIsRoutedToTheLiveTree(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()

	model.tree.Entries = []filetree.Entry{
		{Name: "main.go", Path: filepath.Join(root, "main.go")},
		{Name: "notes.txt", Path: filepath.Join(root, "notes.txt")},
	}
	model.tree.SetSize(40, 5)
	model.tree.StartFilter()
	updatedTree, cmd := model.tree.Update(tea.KeyPressMsg{Text: "main"})
	if cmd == nil || !updatedTree.FilterPending() {
		t.Fatal("tree did not schedule an asynchronous filter projection")
	}
	model.tree = updatedTree
	msg := cmd()
	routed, followup := model.Update(msg)
	if followup != nil {
		t.Fatal("filter result produced an unexpected follow-up command")
	}
	updated := routed.(Model)
	if updated.tree.FilterPending() {
		t.Fatal("root model did not install the routed filter result")
	}
	entry := updated.tree.EntryAtY(1)
	if entry == nil || entry.Name != "main.go" {
		t.Fatalf("routed filtered entry = %#v, want main.go", entry)
	}
}

func TestHandleTreeChangeDebouncesGitRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", tmpDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.logFile != nil {
		defer func() {
			_ = model.logFile.Close()
		}()
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
	refresh, ok := msg.(git.RefreshMsg)
	if !ok {
		t.Fatalf("refreshCmd() returned %T, want git.RefreshMsg", msg)
	}
	if refresh.Generation != 2 {
		t.Fatalf("refresh generation = %d, want 2", refresh.Generation)
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
		defer func() {
			_ = model.logFile.Close()
		}()
	}
	loadInitialTreeForTest(t, &model)

	updatedTree, cmd := model.tree.ToggleEntry(dirPath)
	model.tree = updatedTree
	if cmd == nil {
		t.Fatal("expected directory expansion command")
	}
	model = runWorkspaceEditCommands(t, model, cmd)

	updatedModel, _ := model.handleTreeChange(TreeChangedMsg{Dir: tmpDir})
	updated := updatedModel.(Model)

	// A filesystem event must only schedule work. Reading every expanded
	// directory here would block Bubble Tea's Update loop on slow filesystems.
	if !updated.tree.Entries[0].Expanded || len(updated.tree.Entries[0].Children) != 1 {
		t.Fatal("tree changed before its asynchronous refresh completed")
	}

	scheduledModel, refreshCmd := updated.Update(treeRefreshDebounceMsg{Generation: updated.treeRefreshGeneration})
	if refreshCmd == nil {
		t.Fatal("expected a deferred tree refresh command")
	}
	result := refreshCmd()
	refreshedModel, _ := scheduledModel.Update(result)
	refreshed := refreshedModel.(Model)
	if !refreshed.tree.Entries[0].Expanded {
		t.Fatal("expected async tree refresh to preserve expanded state")
	}
	if len(refreshed.tree.Entries[0].Children) != 1 {
		t.Fatalf("expected 1 child after async tree refresh, got %d", len(refreshed.tree.Entries[0].Children))
	}
}

func TestTreeRefreshIgnoresStaleResult(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fresh.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	loadInitialTreeForTest(t, &model)

	updatedModel, _ := model.handleTreeChange(TreeChangedMsg{Dir: root})
	updated := updatedModel.(Model)
	staleTree := filetree.New(root, updated.theme)
	staleTree.Entries = nil
	result := treeRefreshResultMsg{
		Generation: updated.treeRefreshGeneration - 1,
		Refresh:    filetree.RefreshResult{Entries: staleTree.Entries},
	}

	ignoredModel, cmd := updated.Update(result)
	if cmd != nil {
		t.Fatal("stale tree refresh should not start more work")
	}
	ignored := ignoredModel.(Model)
	if len(ignored.tree.Entries) != 1 || ignored.tree.Entries[0].Name != "fresh.go" {
		t.Fatalf("stale tree refresh overwrote current tree: %#v", ignored.tree.Entries)
	}
}

func TestTreeRefreshRetriesWhenExpansionChangesDuringCommand(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()
	loadInitialTreeForTest(t, &model)

	snapshot := model.tree.SnapshotForRefresh()
	result, err := snapshot.Refresh(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model.tree, _ = model.tree.ToggleEntry(dir)
	if !model.tree.Entries[0].Expanded {
		t.Fatal("setup did not expand the live directory")
	}

	updatedAny, retryCmd := model.handleTreeRefreshResult(treeRefreshResultMsg{
		Generation: model.treeRefreshGeneration,
		Refresh:    result,
	})
	updated := updatedAny.(Model)
	if retryCmd == nil {
		t.Fatal("refresh did not retry after the persistent entry root changed")
	}
	if !updated.tree.Entries[0].Expanded {
		t.Fatal("stale refresh result undid the live expansion")
	}

	retryResult := retryCmd().(treeRefreshResultMsg)
	completedAny, nextCmd := updated.handleTreeRefreshResult(retryResult)
	completed := completedAny.(Model)
	if nextCmd != nil {
		t.Fatal("current retry scheduled another refresh")
	}
	if !completed.tree.Entries[0].Expanded || len(completed.tree.Entries[0].Children) != 1 {
		t.Fatalf("retried refresh tree = %#v, want expanded directory with one child", completed.tree.Entries)
	}
}

func TestInitialTreeLoadCannotOverwriteNewerWatcherGeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "from-watcher.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer model.cleanup()

	updatedModel, _ := model.handleTreeChange(TreeChangedMsg{Dir: root})
	updated := updatedModel.(Model)
	if updated.treeRefreshGeneration == 0 {
		t.Fatal("watcher event did not advance the tree refresh generation")
	}

	lateInitial := filetree.New(root, updated.theme)
	ignoredModel, cmd := updated.Update(treeLoadedMsg{Tree: lateInitial, Generation: 0})
	if cmd != nil {
		t.Fatal("stale initial tree load should not schedule work")
	}
	ignored := ignoredModel.(Model)
	if len(ignored.tree.Entries) != 0 {
		t.Fatalf("late initial tree load overwrote newer state: %#v", ignored.tree.Entries)
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
		defer func() {
			_ = model.logFile.Close()
		}()
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
		defer func() {
			_ = model.logFile.Close()
		}()
	}

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updated := applyGitRefresh(t, model, git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})

	click := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: 2})
	updatedModel, cmd := updated.Update(click)
	if cmd == nil {
		t.Fatal("expected directory click to schedule a prepared tree projection")
	}
	updated = updatedModel.(Model)
	for cmd != nil {
		updatedModel, cmd = updated.Update(cmd())
		updated = updatedModel.(Model)
	}

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
		defer func() {
			_ = model.logFile.Close()
		}()
	}

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updated := applyGitRefresh(t, model, git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})

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
		defer func() {
			_ = model.logFile.Close()
		}()
	}

	model.sidebarTab = SidebarGit
	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	updated := applyGitRefresh(t, model, git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})

	click := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: 2})
	updatedModel, _ := updated.Update(click)
	updated = updatedModel.(Model)

	updated = applyGitRefresh(t, updated, git.RefreshMsg{
		Branch: "main",
		Entries: []git.StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
		},
	})

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
