package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/session"
	"teak/internal/text"
)

func TestFileLoadCompletionDoesNotPopulateReplacementTab(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	for path, content := range map[string]string{first: "first", second: "second"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	opened, _ := m.openFile(first)
	m = opened.(Model)
	request := m.latestFileLoadRequest()
	opened, _ = m.openFile(second) // replaces the preview editor before first completes
	m = opened.(Model)

	updated, _ := m.handleFileLoaded(FileLoadedMsg{
		Path: first, Snapshot: text.NewFromString("first"), EditorID: request.EditorID, RequestID: request.ID,
	})
	m = updated.(Model)
	if got := m.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("stale completion populated replacement tab with %q", got)
	}
}

func TestFileLoadCompletionAfterCloseIsIgnored(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "closed.go")
	if err := os.WriteFile(path, []byte("should never appear"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	opened, _ := m.openFilePinned(path)
	m = opened.(Model)
	request := m.latestFileLoadRequest()
	closed, _ := m.closeTab(m.activeTab)
	m = closed.(Model)
	updated, _ := m.handleFileLoaded(FileLoadedMsg{Path: path, Snapshot: text.NewFromString("should never appear"), EditorID: request.EditorID, RequestID: request.ID})
	m = updated.(Model)
	if len(m.editors) != 0 {
		t.Fatalf("closed async load recreated or modified tabs: %d", len(m.editors))
	}
}

func TestOpenInNewTabDoesNotDuplicateAnExistingFileBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.go")
	if err := os.WriteFile(path, []byte("package shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	opened, _ := m.openFilePinned(path)
	m = opened.(Model)
	tabCount := len(m.editors)
	requestID := m.nextFileLoadID

	reopened, cmd := m.openFileForceNewTab(path)
	m = reopened.(Model)
	if cmd != nil {
		t.Fatal("opening an existing file in a new tab started a duplicate load")
	}
	if len(m.editors) != tabCount {
		t.Fatalf("editor count = %d, want existing %d", len(m.editors), tabCount)
	}
	if m.nextFileLoadID != requestID {
		t.Fatal("duplicate open allocated a second file-load identity")
	}
	if m.activeTab < 0 || m.tabBar.Tabs[m.activeTab].FilePath != path {
		t.Fatal("duplicate open did not focus the existing file tab")
	}
	if m.tabBar.Tabs[m.activeTab].Preview {
		t.Fatal("Open in New Tab did not pin the existing tab")
	}
}

func TestOpenRelativePathDoesNotDuplicateAbsoluteFileBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.go")
	if err := os.WriteFile(path, []byte("package shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	opened, _ := m.openFilePinned(path)
	m = opened.(Model)
	tabCount := len(m.editors)
	requestID := m.nextFileLoadID

	reopened, cmd := m.openFilePinned("shared.go")
	m = reopened.(Model)
	if cmd != nil {
		t.Fatal("opening a relative alias started a duplicate file load")
	}
	if len(m.editors) != tabCount {
		t.Fatalf("relative alias created %d editors, want %d", len(m.editors), tabCount)
	}
	if m.nextFileLoadID != requestID {
		t.Fatal("relative alias allocated a second file-load identity")
	}
	active := m.activeEditor()
	if active == nil {
		t.Fatal("relative alias left no active editor")
	}
	if active.Buffer.FilePath != path {
		t.Fatalf("relative alias focused %q, want existing absolute path %q", active.Buffer.FilePath, path)
	}
}

func TestFileLoadCompletionPreservesEditsMadeInPlaceholder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "slow.go")
	if err := os.WriteFile(path, []byte("package slow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	opened, _ := m.openFilePinned(path)
	m = opened.(Model)
	request := m.latestFileLoadRequest()
	m.activeEditor().Buffer.InsertAtCursor([]byte("draft typed while loading"))

	updated, _ := m.handleFileLoaded(FileLoadedMsg{
		Path:      path,
		Snapshot:  text.NewFromString("package slow\n"),
		EditorID:  request.EditorID,
		RequestID: request.ID,
	})
	m = updated.(Model)

	if got := m.activeEditor().Buffer.Content(); got != "draft typed while loading" {
		t.Fatalf("late load replaced placeholder edits with %q", got)
	}
	if !m.activeEditor().Buffer.Dirty() {
		t.Fatal("placeholder draft should remain dirty after stale load is rejected")
	}
	if _, ok := m.pendingFileLoads[request.ID]; ok {
		t.Fatal("rejected load request was not retired")
	}
}

func TestFileLoadDefersIndentFoldDetectionAndRejectsStaleResult(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "folds.txt")
	if err := os.WriteFile(path, []byte("root\n  child\nnext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	opened, _ := m.openFilePinned(path)
	m = opened.(Model)
	request := m.latestFileLoadRequest()
	loaded, cmd := m.handleFileLoaded(FileLoadedMsg{
		Path:      path,
		Snapshot:  text.NewFromString("root\n  child\nnext\n"),
		EditorID:  request.EditorID,
		RequestID: request.ID,
	})
	m = loaded.(Model)
	if got := len(m.activeEditor().Folds.Regions); got != 0 {
		t.Fatalf("handleFileLoaded synchronously detected %d fold regions", got)
	}
	if cmd == nil {
		t.Fatal("file load did not schedule fold detection")
	}

	batchMsg := cmd()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("file load command returned %T, want non-empty batch", batchMsg)
	}
	firstMsg := batch[0]()
	prepared, ok := firstMsg.(externalFoldRegionsPreparedMsg)
	if !ok {
		t.Fatalf("first async follow-up returned %T, want fold preparation", firstMsg)
	}
	if len(prepared.Regions) == 0 {
		t.Fatal("background fold detection found no indent region")
	}

	m.activeEditor().Buffer.InsertAtCursor([]byte("edited"))
	stale, _ := m.handleExternalFoldRegionsPrepared(prepared)
	m = stale.(Model)
	if got := len(m.activeEditor().Folds.Regions); got != 0 {
		t.Fatalf("stale fold result installed %d regions after an edit", got)
	}
}

func TestConcurrentFileLoadsKeepTheirOwnNavigation(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("zero\none\ntwo\nthree"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	m.setPendingCursor(first, text.Position{Line: 1, Col: 1})
	opened, _ := m.openFilePinned(first)
	m = opened.(Model)
	firstRequest := m.latestFileLoadRequest()
	m.setPendingCursor(second, text.Position{Line: 3, Col: 2})
	opened, _ = m.openFileForceNewTab(second)
	m = opened.(Model)
	secondRequest := m.latestFileLoadRequest()

	updated, _ := m.handleFileLoaded(FileLoadedMsg{Path: second, Snapshot: text.NewFromString("zero\none\ntwo\nthree"), EditorID: secondRequest.EditorID, RequestID: secondRequest.ID})
	m = updated.(Model)
	updated, _ = m.handleFileLoaded(FileLoadedMsg{Path: first, Snapshot: text.NewFromString("zero\none\ntwo\nthree"), EditorID: firstRequest.EditorID, RequestID: firstRequest.ID})
	m = updated.(Model)
	for _, ed := range m.editors {
		switch ed.Buffer.FilePath {
		case first:
			if got := ed.Buffer.Cursor; got.Line != 1 || got.Col != 1 {
				t.Fatalf("first cursor = %+v", got)
			}
		case second:
			if got := ed.Buffer.Cursor; got.Line != 3 || got.Col != 2 {
				t.Fatalf("second cursor = %+v", got)
			}
		}
	}
}

func TestRestoreSessionMapsStateAfterMissingTab(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.go")
	kept := filepath.Join(root, "kept.go")
	if err := os.WriteFile(kept, []byte("a\nb\nc\nd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	state, files, _, err := readSessionRestoreFiles(context.Background(), pinned, root, session.State{RootDir: root, ActiveTab: 1, Tabs: []session.TabState{
		{FilePath: missing, CursorLine: 8, CursorCol: 2, ScrollY: 5},
		{FilePath: kept, CursorLine: 3, CursorCol: 0, ScrollY: 2, WrapScrollY: 4, Pinned: true},
	}}, maxStartupSessionBytes)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.restoreSessionFromPinnedRead(state, files, nil)
	if len(m.editors) != 1 || m.activeTab != 0 {
		t.Fatalf("restore tabs=%d active=%d", len(m.editors), m.activeTab)
	}
	message := cmd()
	load, ok := message.(FileLoadedMsg)
	if !ok {
		t.Fatalf("load command returned %T: %v", message, message)
	}
	updated, _ := m.handleFileLoaded(load)
	m = updated.(Model)
	ed := m.activeEditor()
	if got := ed.Buffer.Cursor; got.Line != 3 || got.Col != 0 {
		t.Fatalf("cursor = %+v", got)
	}
	if ed.Viewport.ScrollY != 2 || ed.Viewport.WrapScrollY != 4 {
		t.Fatalf("scroll = %d/%d", ed.Viewport.ScrollY, ed.Viewport.WrapScrollY)
	}
}

func TestReadEditorFileLimitedRejectsSparseOversizeAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparse.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxEditorFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readEditorFile(context.Background(), path); !errors.Is(err, errEditorFileTooLarge) {
		t.Fatalf("readEditorFile() error = %v, want oversized error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readEditorFile(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v", err)
	}
}

func TestLoadFileCmdPreparesImmutableRopeBeforeUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.go")
	content := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	msg := loadFileCmd(context.Background(), path, 7, 11, false)()
	loaded, ok := msg.(FileLoadedMsg)
	if !ok {
		t.Fatalf("load command returned %T", msg)
	}
	if loaded.Snapshot == nil || loaded.Snapshot.String() != string(content) {
		t.Fatalf("prepared snapshot = %v, want file content", loaded.Snapshot)
	}
	if loaded.Data != nil {
		t.Fatalf("load command retained %d raw bytes after preparing the rope", len(loaded.Data))
	}
}

func TestReadEditorFileRejectsNonRegularInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directory.go")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := readEditorFile(context.Background(), path); !errors.Is(err, errEditorFileNotRegular) {
		t.Fatalf("readEditorFile(directory) error = %v, want %v", err, errEditorFileNotRegular)
	}
}
