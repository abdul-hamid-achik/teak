package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/search"
)

// TestAuditCommandPaletteRoutesRealCommands exercises the same messages used
// after a picker selection.  It deliberately checks user-visible state rather
// than merely enumerating the registry, so command-palette regressions cannot
// silently diverge from keyboard routing.
func TestAuditCommandPaletteRoutesRealCommands(t *testing.T) {
	newModel := func(t *testing.T) Model {
		t.Helper()
		m := newInputRoutingTestModel(t)
		m.width, m.height = 100, 30
		return m
	}

	t.Run("palette contains executable commands", func(t *testing.T) {
		m := newModel(t)
		items := m.buildCommandList()
		if len(items) < 20 {
			t.Fatalf("command palette items = %d, want the full command set", len(items))
		}
		for _, item := range items {
			command, ok := item.Value.(Command)
			if !ok || command.ID == "" || command.Label == "" || command.Execute == nil {
				t.Fatalf("invalid palette item: %#v", item)
			}
			if _, ok := command.Execute().(commandPaletteMsg); !ok {
				t.Fatalf("command %q did not preserve command-palette dispatch", command.ID)
			}
		}

		openedAny, focusCmd := m.openCommandPalette()
		opened := openedAny.(Model)
		if focusCmd == nil || opened.overlayStack.IsEmpty() {
			t.Fatal("opening the command palette did not focus and push a picker")
		}
		picker, ok := opened.overlayStack.Top().(*overlay.Picker)
		if !ok || picker.ZoneID() != "cmdpalette" {
			t.Fatalf("palette overlay = %T/%q, want command palette", opened.overlayStack.Top(), picker.ZoneID())
		}
	})

	t.Run("overlay selection toggles sidebar and opens text search", func(t *testing.T) {
		m := newModel(t)
		m.showTree = false
		m.focus = FocusEditor
		items := m.buildCommandList()
		var toggle, find overlay.PickerItem
		for _, item := range items {
			command := item.Value.(Command)
			switch command.ID {
			case "toggle_tree":
				toggle = item
			case "find":
				find = item
			}
		}

		updatedAny, _ := m.Update(overlay.PickerSelectMsg{Item: toggle})
		updated := updatedAny.(Model)
		if !updated.showTree || updated.focus != FocusTree {
			t.Fatalf("toggle tree left state show=%v focus=%v", updated.showTree, updated.focus)
		}

		updatedAny, cmd := updated.Update(overlay.PickerSelectMsg{Item: find})
		updated = updatedAny.(Model)
		if !updated.showSearch || updated.searchMode != search.ModeText || cmd == nil {
			t.Fatalf("find palette selection did not open/focus text search: show=%v mode=%v cmd=%v", updated.showSearch, updated.searchMode, cmd != nil)
		}
	})

	t.Run("new file and save-as initialize distinct tab state", func(t *testing.T) {
		m := newModel(t)
		initialTabs := len(m.editors)
		updatedAny, _ := m.handleCommandPaletteAction(newFileMsg{})
		updated := updatedAny.(Model)
		if len(updated.editors) != initialTabs+1 || updated.activeEditor().Buffer.FilePath != "" {
			t.Fatalf("new file created invalid tab state: tabs=%d path=%q", len(updated.editors), updated.activeEditor().Buffer.FilePath)
		}
		if updated.tabBar.Tabs[updated.activeTab].Preview {
			t.Fatal("new file tab must be pinned rather than a preview")
		}

		updatedAny, _ = updated.handleCommandPaletteAction(saveAsMsg{})
		updated = updatedAny.(Model)
		if !updated.saveAsMode || !strings.HasSuffix(updated.saveAsInput, "/") {
			t.Fatalf("save as state = mode %v input %q, want workspace path prompt", updated.saveAsMode, updated.saveAsInput)
		}
	})

	t.Run("settings help problems and unavailable git retain independent focus", func(t *testing.T) {
		m := newModel(t)
		updatedAny, helpCmd := m.handleCommandPaletteAction(showHelpMsg{})
		updated := updatedAny.(Model)
		if !updated.showHelp || helpCmd == nil {
			t.Fatal("help command did not open and focus the overlay")
		}

		updatedAny, _ = updated.handleCommandPaletteAction(openSettingsMsg{})
		updated = updatedAny.(Model)
		if !updated.showSettings {
			t.Fatal("settings command did not open settings")
		}

		updatedAny, _ = updated.handleCommandPaletteAction(toggleProblemsMsg{})
		updated = updatedAny.(Model)
		if !updated.showTree || updated.sidebarTab != SidebarProblems || updated.focus != FocusProblems {
			t.Fatalf("problems command state = show %v tab %v focus %v", updated.showTree, updated.sidebarTab, updated.focus)
		}

		updated.focus = FocusEditor
		updated.sidebarTab = SidebarFiles
		updated.showTree = false
		updatedAny, _ = updated.handleCommandPaletteAction(toggleGitMsg{})
		updated = updatedAny.(Model)
		if updated.showTree || updated.sidebarTab != SidebarFiles || updated.focus != FocusEditor {
			t.Fatal("git command changed sidebar even though the workspace is not a git repository")
		}
	})
}

// TestAuditInputOverlaysKeepInputIsolated covers the compact text-entry
// overlays through their key handlers.  These branches are easy to miss in a
// mouse-centric TUI test, but a wrong mode flag would send keystrokes into the
// editor and lose the user's pending action.
func TestAuditInputOverlaysKeepInputIsolated(t *testing.T) {
	t.Run("go to line clamps, clears selection, and ignores non-digits", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		m.activeEditor().Buffer.LoadContent([]byte("zero\none\ntwo"))
		m.goToLineMode = true
		m.goToLineInput = "2"
		updatedAny, _ := m.handleGoToLineInput(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated := updatedAny.(Model)
		if updated.goToLineMode || updated.activeEditor().Buffer.Cursor.Line != 1 || updated.activeEditor().Buffer.Cursor.Col != 0 {
			t.Fatalf("go-to-line state = mode %v cursor %+v", updated.goToLineMode, updated.activeEditor().Buffer.Cursor)
		}

		updated.goToLineMode = true
		updated.goToLineInput = "9"
		updatedAny, _ = updated.handleGoToLineInput(tea.KeyPressMsg{Code: tea.KeyEnter})
		updated = updatedAny.(Model)
		if updated.activeEditor().Buffer.Cursor.Line != 2 {
			t.Fatalf("out-of-range line cursor = %d, want final line", updated.activeEditor().Buffer.Cursor.Line)
		}

		updated.goToLineMode = true
		updated.goToLineInput = "4"
		updatedAny, _ = updated.handleGoToLineInput(tea.KeyPressMsg{Code: 'x', Text: "x"})
		updated = updatedAny.(Model)
		if updated.goToLineInput != "4" {
			t.Fatalf("non-digit changed go-to-line input to %q", updated.goToLineInput)
		}
	})

	t.Run("rename and save-as cancellation discard only their prompt", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		m.renameMode = true
		m.renameInput = "réname"
		updatedAny, _ := m.handleRenameInput(tea.KeyPressMsg{Code: tea.KeyBackspace})
		updated := updatedAny.(Model)
		if updated.renameInput != "rénam" {
			t.Fatalf("unicode rename backspace = %q, want %q", updated.renameInput, "rénam")
		}
		updatedAny, _ = updated.handleRenameInput(tea.KeyPressMsg{Code: tea.KeyEscape})
		updated = updatedAny.(Model)
		if updated.renameMode || updated.renameInput != "" {
			t.Fatalf("rename cancel left mode/input = %v/%q", updated.renameMode, updated.renameInput)
		}

		updated.saveAsMode = true
		updated.saveAsInput = "draft.go"
		updatedAny, _ = updated.handleSaveAsInput(tea.KeyPressMsg{Code: tea.KeyEscape})
		updated = updatedAny.(Model)
		if updated.saveAsMode || updated.saveAsInput != "" {
			t.Fatalf("save-as cancel left mode/input = %v/%q", updated.saveAsMode, updated.saveAsInput)
		}
	})

	t.Run("context menu starts rename without leaking a key to the editor", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		updatedAny, _ := m.handleContextMenuAction("rename_symbol")
		updated := updatedAny.(Model)
		if !updated.renameMode || updated.renameInput != "" {
			t.Fatal("rename context-menu action did not start a clean rename prompt")
		}
		updated = updateInputRoutingModel(t, updated, tea.KeyPressMsg{Code: 'x', Text: "x"})
		if updated.renameInput != "x" || updated.activeEditor().Buffer.Content() != "" {
			t.Fatalf("rename key routing input=%q editor=%q", updated.renameInput, updated.activeEditor().Buffer.Content())
		}
	})
}

func TestAuditQuickOpenResultHandlesStaleSuccessAndFailure(t *testing.T) {
	m := newInputRoutingTestModel(t)
	m.width, m.height = 100, 30
	openedAny, _ := m.openQuickOpen()
	opened := openedAny.(Model)
	generation := opened.fileListGeneration
	if opened.overlayStack.IsEmpty() {
		t.Fatal("quick open did not push a picker")
	}

	staleAny, _ := opened.Update(FileListMsg{Generation: generation + 1, Files: []string{"stale.go"}})
	stale := staleAny.(Model)
	if stale.cachedFilesReady || len(stale.cachedFiles) != 0 {
		t.Fatalf("stale quick-open result altered cache: ready=%v files=%v", stale.cachedFilesReady, stale.cachedFiles)
	}

	failedAny, _ := stale.Update(FileListMsg{Generation: generation, Err: errors.New("permission denied")})
	failed := failedAny.(Model)
	if !failed.cachedFilesReady || !strings.Contains(failed.status, "Quick Open scan") {
		t.Fatalf("failed scan state ready=%v status=%q", failed.cachedFilesReady, failed.status)
	}

	updatedAny, refreshCmd := failed.Update(FileListMsg{Generation: generation, Files: []string{"nested/main.go", "README.md"}})
	updated := updatedAny.(Model)
	if len(updated.cachedFiles) != 2 {
		t.Fatalf("quick-open cache = %v", updated.cachedFiles)
	}
	if refreshCmd == nil {
		t.Fatal("quick-open result did not schedule picker projection")
	}
	rawReady := refreshCmd()
	itemsReady, ok := rawReady.(overlay.PickerItemsReadyMsg)
	if !ok {
		t.Fatalf("quick-open item command returned %T, want PickerItemsReadyMsg", rawReady)
	}
	updatedAny, filterCmd := updated.Update(itemsReady)
	updated = updatedAny.(Model)
	if filterCmd == nil {
		t.Fatal("quick-open item result did not schedule picker projection")
	}
	rawFilterReady := filterCmd()
	filterReady, ok := rawFilterReady.(overlay.PickerFilterReadyMsg)
	if !ok {
		t.Fatalf("quick-open filter command returned %T, want PickerFilterReadyMsg", rawFilterReady)
	}
	updatedAny, _ = updated.Update(filterReady)
	updated = updatedAny.(Model)
	picker, ok := updated.overlayStack.Top().(*overlay.Picker)
	if !ok || picker.FilteredCount() != 2 {
		t.Fatalf("quick-open picker was not refreshed: %T count=%d", updated.overlayStack.Top(), picker.FilteredCount())
	}

	closedAny, _ := updated.Update(overlay.PickerCloseMsg{})
	closed := closedAny.(Model)
	if !closed.overlayStack.IsEmpty() || closed.fileListCancel != nil {
		t.Fatal("closing quick open left an overlay or a live scan cancellation handle")
	}
}

func TestAuditTreeContextActionsAndAsyncStatus(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.treeContextPath = filepath.Join(root, "dir", "file.go")

	newFileAny, _ := m.handleTreeContextMenuAction("tree_new_file_sibling")
	newFile := newFileAny.(Model)
	if !newFile.newFileMode || newFile.newItemDir != filepath.Join(root, "dir") {
		t.Fatalf("new sibling file state = mode %v dir %q", newFile.newFileMode, newFile.newItemDir)
	}

	newFolderAny, _ := newFile.handleTreeContextMenuAction("tree_new_folder_sibling")
	newFolder := newFolderAny.(Model)
	if !newFolder.newFolderMode || newFolder.newItemDir != filepath.Join(root, "dir") {
		t.Fatalf("new sibling folder state = mode %v dir %q", newFolder.newFolderMode, newFolder.newItemDir)
	}

	deleteAny, _ := newFolder.handleTreeContextMenuAction("tree_delete")
	deleted := deleteAny.(Model)
	if !deleted.deleteConfirm || deleted.deleteTarget != m.treeContextPath {
		t.Fatalf("delete prompt state = confirm %v target %q", deleted.deleteConfirm, deleted.deleteTarget)
	}

	copyOKAny, _ := deleted.handleTreeCopyPathResult(treeCopyPathResultMsg{Path: "dir/file.go"})
	copyOK := copyOKAny.(Model)
	if copyOK.status != "Copied: dir/file.go" {
		t.Fatalf("copy success status = %q", copyOK.status)
	}
	copyFailedAny, _ := copyOK.handleTreeCopyPathResult(treeCopyPathResultMsg{Path: "dir/file.go", Err: errors.New("clipboard unavailable")})
	copyFailed := copyFailedAny.(Model)
	if !strings.Contains(copyFailed.status, "Teak clipboard") || !strings.Contains(copyFailed.status, "dir/file.go") {
		t.Fatalf("copy fallback status = %q", copyFailed.status)
	}
}

func TestAuditWorkspaceEditFailureReportsNoPartialMutation(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	outside := filepath.Join(t.TempDir(), "outside.go")

	updated := applyWorkspaceEditAsyncForTest(t, m, lsp.WorkspaceEdit{
		Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(outside): {{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, NewText: "bad"}},
		},
	})
	if !strings.Contains(updated.status, "Workspace edit rejected") {
		t.Fatalf("rejected workspace edit status=%q", updated.status)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("rejected workspace edit mutated active buffer to %q", got)
	}

	for _, tc := range []struct {
		name string
		op   lsp.WorkspaceFileOperation
		want string
	}{
		{"unsupported operation", lsp.WorkspaceFileOperation{Kind: "move", URI: lsp.FileURI(filepath.Join(root, "a.go"))}, "move"},
		{"outside operation", lsp.WorkspaceFileOperation{Kind: lsp.FileOpCreate, URI: lsp.FileURI(outside)}, "outside"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := m
			got := applyWorkspaceEditAsyncForTest(t, candidate, lsp.WorkspaceEdit{
				DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &tc.op}},
			})
			if !strings.Contains(got.status, "rejected") {
				t.Fatalf("operation status=%q", got.status)
			}
		})
	}
}

func TestAuditLSPPickerPresentationKeepsNestedLocationsReadable(t *testing.T) {
	root := t.TempDir()
	locations := []lsp.Location{
		{URI: lsp.FileURI(filepath.Join(root, "pkg", "worker.go")), StartLine: 3, StartCol: 5},
		{URI: lsp.FileURI(filepath.Join(root, "main.go")), StartLine: 0, StartCol: 0},
	}
	items := lspLocationsToPickerItems(locations, root)
	if len(items) != 2 || items[0].Label != "worker.go:4" || items[0].Description != "pkg" || items[1].Description != "" {
		t.Fatalf("location picker items = %#v", items)
	}

	symbols := []lsp.DocumentSymbol{{
		Name: "Service", Kind: 5, Detail: "",
		Children: []lsp.DocumentSymbol{{Name: "Run", Kind: 6, Detail: "method"}},
	}}
	symbolItems := lspSymbolsToPickerItems(symbols)
	if len(symbolItems) != 2 || symbolItems[0].Description != "Class" || symbolItems[1].Label != "Service.Run" || symbolItems[1].Description != "method" {
		t.Fatalf("symbol picker items = %#v", symbolItems)
	}

	for kind, want := range map[int]string{
		1: "File", 2: "Module", 3: "Namespace", 4: "Package", 5: "Class", 6: "Method",
		7: "Property", 8: "Field", 9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
		13: "Variable", 14: "Constant", 15: "String", 16: "Number", 17: "Boolean", 18: "Array",
		19: "Object", 23: "Struct", 24: "Event", 25: "Operator", 26: "TypeParameter", 999: "Symbol",
	} {
		if got := symbolKindName(kind); got != want {
			t.Errorf("symbolKindName(%d) = %q, want %q", kind, got, want)
		}
	}
}

func TestAuditGlobalRoutingAndSearchNavigationState(t *testing.T) {
	t.Run("global routes open overlays without reaching the editor", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		m.width, m.height = 100, 30
		m.cachedFilesReady = true
		m.cachedFiles = []string{"main.go"}

		for _, tc := range []struct {
			name  string
			key   tea.KeyPressMsg
			check func(t *testing.T, m Model, cmd tea.Cmd)
		}{
			{
				name: "find replace", key: tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl},
				check: func(t *testing.T, m Model, cmd tea.Cmd) {
					if !m.showSearch || m.searchMode != search.ModeText || cmd == nil {
						t.Fatalf("replace overlay state show=%v mode=%v cmd=%v", m.showSearch, m.searchMode, cmd != nil)
					}
				},
			},
			{
				name: "project text search", key: tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl | tea.ModShift},
				check: func(t *testing.T, m Model, cmd tea.Cmd) {
					if !m.showSearch || m.searchMode != search.ModeText || cmd == nil {
						t.Fatalf("text search overlay state show=%v mode=%v cmd=%v", m.showSearch, m.searchMode, cmd != nil)
					}
				},
			},
			{
				name: "go to line", key: tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl},
				check: func(t *testing.T, m Model, _ tea.Cmd) {
					if !m.goToLineMode || m.goToLineInput != "" {
						t.Fatalf("go-to-line state = %v/%q", m.goToLineMode, m.goToLineInput)
					}
				},
			},
			{
				name: "quick open", key: tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl},
				check: func(t *testing.T, m Model, cmd tea.Cmd) {
					if m.overlayStack.IsEmpty() || cmd == nil {
						t.Fatal("quick-open global action did not push/focus its picker")
					}
				},
			},
			{
				name: "command palette", key: tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift},
				check: func(t *testing.T, m Model, cmd tea.Cmd) {
					if m.overlayStack.IsEmpty() || cmd == nil {
						t.Fatal("command-palette global action did not push/focus its picker")
					}
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				candidate := m
				updatedAny, cmd, handled := candidate.handleGlobalKey(tc.key)
				if !handled {
					t.Fatal("global key was not handled")
				}
				updated := updatedAny.(Model)
				tc.check(t, updated, cmd)
				if got := updated.activeEditor().Buffer.Content(); got != "" {
					t.Fatalf("global shortcut leaked into editor: %q", got)
				}
			})
		}
	})

	t.Run("search navigation prefers the next local location then wraps", func(t *testing.T) {
		root := t.TempDir()
		m := newTreeUXModel(t, root)
		path := filepath.Join(root, "main.go")
		m.activeEditor().Buffer.FilePath = path
		m.tabBar.Tabs[m.activeTab].FilePath = path
		m.activeEditor().Buffer.LoadContent([]byte("zero\none\ntwo"))
		m.activeEditor().Buffer.Cursor.Line = 0
		m.lastSearchResults = []search.Result{
			{FilePath: "main.go", Line: 0, Col: 1},
			{FilePath: "main.go", Line: 2, Col: 0},
		}
		m.lastSearchIndex = 0

		nextAny, nextCmd := m.findNext()
		next := nextAny.(Model)
		if nextCmd != nil || next.lastSearchIndex != 1 || next.activeEditor().Buffer.Cursor.Line != 2 || !strings.Contains(next.status, "Match 2/2") {
			t.Fatalf("next navigation index=%d cursor=%+v status=%q cmd=%v", next.lastSearchIndex, next.activeEditor().Buffer.Cursor, next.status, nextCmd != nil)
		}

		next.activeEditor().Buffer.Cursor.Line = 0
		next.activeEditor().Buffer.Cursor.Col = 0
		prevAny, prevCmd := next.findPrev()
		prev := prevAny.(Model)
		if prevCmd != nil || prev.lastSearchIndex != 0 || prev.activeEditor().Buffer.Cursor.Col != 1 || !strings.Contains(prev.status, "Match 1/2") {
			t.Fatalf("previous navigation index=%d cursor=%+v status=%q cmd=%v", prev.lastSearchIndex, prev.activeEditor().Buffer.Cursor, prev.status, prevCmd != nil)
		}

		prev.activeEditor().Buffer.Cursor.Line = 2
		prev.activeEditor().Buffer.Cursor.Col = 0
		wrapAny, wrapCmd := prev.findNext()
		wrapped := wrapAny.(Model)
		if wrapCmd != nil || wrapped.lastSearchIndex != 1 || !strings.Contains(wrapped.status, "wrapped") {
			t.Fatalf("wrapped navigation index=%d status=%q cmd=%v", wrapped.lastSearchIndex, wrapped.status, wrapCmd != nil)
		}
	})
}

func TestAuditDiffAndFileErrorResultsCannotCorruptTabs(t *testing.T) {
	t.Run("diff completion validates identity before rendering", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		m.width, m.height = 100, 30
		openedAny, cmd := m.openDiff("main.go", "M")
		opened := openedAny.(Model)
		if cmd == nil || len(opened.pendingDiffLoads) != 1 || opened.tabBar.Tabs[opened.activeTab].Kind != editor.TabDiff {
			t.Fatalf("open diff state command=%v pending=%d tab=%+v", cmd != nil, len(opened.pendingDiffLoads), opened.tabBar.Tabs[opened.activeTab])
		}
		var request pendingDiffLoad
		for _, value := range opened.pendingDiffLoads {
			request = value
		}

		staleAny, _ := opened.handleDiffLoaded(DiffLoadedMsg{Path: request.Path, EditorID: request.EditorID + 1, RequestID: request.ID})
		stale := staleAny.(Model)
		if len(stale.pendingDiffLoads) != 1 || stale.status != "" {
			t.Fatalf("stale diff result changed state pending=%d status=%q", len(stale.pendingDiffLoads), stale.status)
		}

		failedAny, _ := stale.handleDiffLoaded(DiffLoadedMsg{Path: request.Path, EditorID: request.EditorID, RequestID: request.ID, Err: errors.New("git unavailable")})
		failed := failedAny.(Model)
		if len(failed.pendingDiffLoads) != 0 || !strings.Contains(failed.status, "Diff error") {
			t.Fatalf("diff failure pending=%d status=%q", len(failed.pendingDiffLoads), failed.status)
		}
	})

	t.Run("successful diff and save error update only their visible state", func(t *testing.T) {
		m := newInputRoutingTestModel(t)
		m.width, m.height = 100, 30
		openedAny, _ := m.openDiff("main.go", "M")
		opened := openedAny.(Model)
		var request pendingDiffLoad
		for _, value := range opened.pendingDiffLoads {
			request = value
		}
		view := diff.New(request.Path, []diff.DiffLine{{
			Right: "package main", RightNum: 1, RightKind: diff.KindAdded,
		}}, opened.theme)
		updatedAny, _ := opened.handleDiffLoaded(DiffLoadedMsg{
			Path: request.Path, EditorID: request.EditorID, RequestID: request.ID,
			View: &view,
		})
		updated := updatedAny.(Model)
		if _, ok := updated.diffViews[updated.activeTab]; !ok || updated.status != "" {
			t.Fatalf("successful diff not installed: views=%d status=%q", len(updated.diffViews), updated.status)
		}

		errorAny, _ := updated.Update(FileErrorMsg{Path: "main.go", Err: errors.New("disk full")})
		afterError := errorAny.(Model)
		if !strings.Contains(afterError.status, "Error saving main.go") || !strings.Contains(afterError.status, "disk full") {
			t.Fatalf("save error status=%q", afterError.status)
		}
	})
}
