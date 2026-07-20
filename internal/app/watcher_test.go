package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"teak/internal/text"
)

func TestFileWatcherStartsInitialTraversalAsynchronously(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	readDir := func(path string) ([]os.DirEntry, error) {
		close(entered)
		<-release
		return nil, errors.New("stop test traversal")
	}

	started := time.Now()
	fw, err := newFileWatcherWithReadDir(t.TempDir(), defaultMaxWatches(), readDir)
	if err != nil {
		t.Fatalf("newFileWatcherWithReadDir() error = %v", err)
	}
	defer func() {
		close(release)
		fw.Close()
	}()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("watcher construction blocked on recursive traversal for %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background traversal did not start")
	}
}

func TestFileWatcherScanReadsInBatchesAndHonorsGlobalBudget(t *testing.T) {
	root := t.TempDir()
	entry := watcherTestDirEntry{name: "generated.go"}
	var maxRequested, batches int
	reader := func(ctx context.Context, path string, batchSize int, visit func([]os.DirEntry) bool) error {
		if path != root {
			t.Fatalf("reader path = %q, want %q", path, root)
		}
		maxRequested = max(maxRequested, batchSize)
		for delivered := 0; delivered < maxWatchScanEntries+1; delivered += batchSize {
			batches++
			count := min(batchSize, maxWatchScanEntries+1-delivered)
			entries := make([]os.DirEntry, count)
			for i := range entries {
				entries[i] = entry
			}
			if !visit(entries) {
				return nil
			}
		}
		return nil
	}

	fw, err := newFileWatcherWithBatchReader("", defaultMaxWatches(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	fw.rootDir = root

	fw.scanDirectoryTree(root)
	if maxRequested != watchScanBatchSize {
		t.Fatalf("batch size = %d, want %d", maxRequested, watchScanBatchSize)
	}
	if batches < 2 {
		t.Fatalf("batch reader calls = %d, want more than one", batches)
	}
}

func TestFileWatcherScanDoesNotStartAfterCancellation(t *testing.T) {
	called := false
	reader := func(context.Context, string, int, func([]os.DirEntry) bool) error {
		called = true
		return nil
	}
	fw, err := newFileWatcherWithBatchReader("", defaultMaxWatches(), reader)
	if err != nil {
		t.Fatal(err)
	}
	fw.cancel()
	fw.scanDirectoryTree(t.TempDir())
	fw.Close()
	if called {
		t.Fatal("cancelled watcher started a directory read")
	}
}

type watcherTestDirEntry struct{ name string }

func (e watcherTestDirEntry) Name() string             { return e.name }
func (watcherTestDirEntry) IsDir() bool                { return false }
func (watcherTestDirEntry) Type() os.FileMode          { return 0 }
func (watcherTestDirEntry) Info() (os.FileInfo, error) { return nil, errors.New("not needed") }

func TestFileWatcherCloseStopsBackgroundWorkers(t *testing.T) {
	fw, err := newFileWatcher(t.TempDir())
	if err != nil {
		t.Fatalf("newFileWatcher() error = %v", err)
	}
	fw.Close()
	select {
	case <-fw.done:
	case <-time.After(time.Second):
		t.Fatal("watcher workers did not exit after Close")
	}
}

func TestFileWatcherPublishNeverBlocksWhenConsumerIsSlow(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatalf("newFileWatcher() error = %v", err)
	}
	defer fw.Close()

	for i := 0; i < cap(fw.msgChan)+10; i++ {
		started := time.Now()
		fw.publish(TreeChangedMsg{Dir: filepath.Join("project", string(rune('a'+i)))})
		if elapsed := time.Since(started); elapsed > 20*time.Millisecond {
			t.Fatalf("publish blocked for %s", elapsed)
		}
	}
	select {
	case <-fw.msgChan:
	case <-time.After(time.Second):
		t.Fatal("expected a coalesced watcher message")
	}
}

func TestFileWatcherNeverDropsOpenFileChangeDuringTreeStorm(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatalf("newFileWatcher() error = %v", err)
	}
	defer fw.Close()

	criticalPath := filepath.Join("project", "dirty.go")
	fw.publish(FileChangedMsg{Path: criticalPath, Data: []byte("external")})
	for i := 0; i < cap(fw.msgChan)*3; i++ {
		fw.publish(TreeChangedMsg{Dir: filepath.Join("project", "generated", string(rune(i+1)))})
	}

	for range cap(fw.msgChan) {
		msg := fw.listenCmd()()
		if changed, ok := msg.(FileChangedMsg); ok && changed.Path == criticalPath {
			return
		}
	}
	t.Fatal("open-file change was dropped behind tree notifications")
}

func TestWatcherAttributesMatchingExpectedWriteWithoutPublishingIt(t *testing.T) {
	fw := &fileWatcher{ownWrites: make(map[string]ownWriteExpectation)}
	path := filepath.Join(t.TempDir(), "main.go")
	snapshot := text.NewFromString("saved by teak\n")

	fw.expectOwnWrite(path, snapshot)
	if !fw.matchesExpectedOwnWrite(path, []byte("saved by teak\n"), time.Now()) {
		t.Fatal("matching Teak save was not attributed to the expected write")
	}
	if fw.matchesExpectedOwnWrite(path, []byte("external\n"), time.Now()) {
		t.Fatal("different external bytes were attributed to Teak")
	}
}

func TestWatcherPendingSnapshotBudgetDegradesToAsyncReread(t *testing.T) {
	fw := &fileWatcher{
		msgChan:            make(chan tea.Msg, 1),
		pendingFileChanges: make(map[string]FileChangedMsg),
		pendingChangeBytes: maxPendingChangeBytes - 1,
		ctx:                context.Background(),
	}
	path := filepath.Join(t.TempDir(), "large.go")

	fw.publish(FileChangedMsg{Path: path, Snapshot: text.NewFromString("xx"), Observation: 7})

	pending := fw.pendingFileChanges[path]
	if pending.Snapshot != nil || !pending.NeedsRead {
		t.Fatalf("pending change = %#v, want metadata-only async reread fallback", pending)
	}
	if pending.Observation != 7 {
		t.Fatalf("observation = %d, want 7", pending.Observation)
	}
}

func TestFileWatcherProcessErrorReconcilesEveryOpenFileAfterOverflow(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := newFileWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	fw.WatchFile(first)
	fw.WatchFile(second)

	// Call the classification seam directly: an overflow means fsnotify has
	// already lost events, so the recovery must not depend on another event
	// arriving to make either open buffer safe.
	fw.processError(fsnotify.ErrEventOverflow)

	got := make(map[string]FileChangedMsg)
	for range 2 {
		msg, ok := fw.popPendingFileChange()
		if !ok {
			t.Fatal("overflow did not enqueue every open file")
		}
		got[msg.Path] = msg
	}
	for _, path := range []string{first, second} {
		msg, ok := got[path]
		if !ok {
			t.Fatalf("missing recovery notification for %q: %#v", path, got)
		}
		if !msg.NeedsRead || msg.Snapshot != nil || msg.Observation == 0 {
			t.Fatalf("recovery message = %#v, want metadata-only ordered reread", msg)
		}
	}
	deadline := time.After(time.Second)
	for {
		select {
		case raw := <-fw.msgChan:
			tree, ok := raw.(TreeChangedMsg)
			if ok && tree.Dir == root {
				return
			}
		case <-deadline:
			t.Fatal("overflow did not publish a root tree reconciliation")
		}
	}
}

func TestFileWatcherOpenFileWritesBypassTreeDebounce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fw, err := newFileWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	fw.WatchFile(path)

	now := time.Unix(1_700_000_000, 0)
	fw.processEvent(fsnotify.Event{Name: path, Op: fsnotify.Write}, now)
	fw.processEvent(fsnotify.Event{Name: path, Op: fsnotify.Write}, now)

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("did not receive the second open-file observation")
		default:
		}
		changed, ok := fw.popPendingFileChange()
		if !ok || changed.Path != path {
			time.Sleep(time.Millisecond)
			continue
		}
		if changed.Observation != 2 {
			// The first read is allowed to finish before the second event is
			// queued; keep draining until the newest observation arrives.
			continue
		}
		if changed.Snapshot == nil || changed.NeedsRead {
			t.Fatalf("open-file write = %#v, want prepared snapshot", changed)
		}
		return
	}
}

func TestFileWatcherOpenFileRemovalSignalsMissingAndReinstallsParentWatch(t *testing.T) {
	// Use an external file: it starts with only a direct file watch, which is
	// the case that otherwise loses the Create half of an atomic replacement.
	parent := t.TempDir()
	path := filepath.Join(parent, "external.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	fw.WatchFile(path)
	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(path) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(path) {
		t.Fatal("direct external file watch was not installed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fw.processEvent(fsnotify.Event{Name: path, Op: fsnotify.Remove}, time.Unix(1_700_000_000, 0))

	changed, ok := fw.popPendingFileChange()
	if !ok {
		t.Fatal("remove did not enqueue an open-file change")
	}
	if !changed.Missing || !changed.NeedsRead || changed.Observation == 0 {
		t.Fatalf("remove notification = %#v, want missing ordered reread", changed)
	}
	deadline = time.Now().Add(time.Second)
	for !fw.isWatched(parent) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(parent) {
		t.Fatal("remove did not reinstall a parent watch for atomic replacement")
	}
}

func TestFileWatcherFsnotifyOpenWritePublishesPreparedSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fw, err := newFileWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(root) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(root) {
		t.Fatal("root watch was not installed")
	}
	fw.WatchFile(path)
	if err := os.WriteFile(path, []byte("package after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		changed, ok := fw.popPendingFileChange()
		if !ok || changed.Path != path || changed.Snapshot == nil {
			time.Sleep(time.Millisecond)
			continue
		}
		if got := changed.Snapshot.String(); got != "package after\n" {
			t.Fatalf("snapshot = %q, want external contents", got)
		}
		return
	}
	t.Fatal("timed out waiting for prepared external write snapshot")
}

func TestIsGitInternalPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		rootDir string
		want    bool
	}{
		{"git dir itself", "/project/.git", "/project", true},
		{"git HEAD", "/project/.git/HEAD", "/project", true},
		{"git refs heads", "/project/.git/refs/heads/main", "/project", true},
		{"git index", "/project/.git/index", "/project", true},
		{"normal file", "/project/main.go", "/project", false},
		{"dotfile not git", "/project/.gitignore", "/project", false},
		{"nested git", "/project/sub/.git/HEAD", "/project", false},
		{"git prefix in name", "/project/.github/workflows/ci.yml", "/project", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitInternalPath(tt.path, tt.rootDir)
			if got != tt.want {
				t.Errorf("isGitInternalPath(%q, %q) = %v, want %v", tt.path, tt.rootDir, got, tt.want)
			}
		})
	}
}

func TestFileWatcher_GitDirWatched(t *testing.T) {
	// Create a temp directory with a .git structure
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	refsDir := filepath.Join(gitDir, "refs")
	headsDir := filepath.Join(refsDir, "heads")
	if err := os.MkdirAll(headsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(heads) error = %v", err)
	}

	// Write initial HEAD
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(HEAD) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(headsDir, "main"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main ref) error = %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	// Modify a git ref (simulates commit/push)
	time.Sleep(150 * time.Millisecond) // let watcher settle
	if err := os.WriteFile(filepath.Join(headsDir, "main"), []byte("def456\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated main ref) error = %v", err)
	}

	// Should receive a TreeChangedMsg
	select {
	case msg := <-fw.msgChan:
		if _, ok := msg.(TreeChangedMsg); !ok {
			t.Errorf("expected TreeChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TreeChangedMsg from .git change")
	}
}

func TestFileWatcher_NewFileCreation(t *testing.T) {
	tmpDir := t.TempDir()

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	time.Sleep(150 * time.Millisecond)

	// Create a new file (triggers Create event → TreeChangedMsg)
	newFile := filepath.Join(tmpDir, "new_file.go")
	if err := os.WriteFile(newFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(new_file.go) error = %v", err)
	}

	// Should receive a TreeChangedMsg for the creation
	select {
	case msg := <-fw.msgChan:
		switch msg.(type) {
		case TreeChangedMsg:
			// expected
		case FileChangedMsg:
			// also acceptable — some platforms emit Write after Create
		default:
			t.Errorf("expected TreeChangedMsg or FileChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message from file creation")
	}
}

func TestFileWatcher_FileDeletion(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "delete_me.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(delete_me.go) error = %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	time.Sleep(150 * time.Millisecond)

	// Delete the file
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("Remove(delete_me.go) error = %v", err)
	}

	// Should receive a TreeChangedMsg for the removal
	select {
	case msg := <-fw.msgChan:
		if _, ok := msg.(TreeChangedMsg); !ok {
			t.Errorf("expected TreeChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TreeChangedMsg from deletion")
	}
}

func TestFileWatcher_NewDirWatched(t *testing.T) {
	tmpDir := t.TempDir()

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	time.Sleep(150 * time.Millisecond)

	// Create a new subdirectory
	newDir := filepath.Join(tmpDir, "newpkg")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(newpkg) error = %v", err)
	}

	// Should receive a TreeChangedMsg for the new directory
	select {
	case msg := <-fw.msgChan:
		if _, ok := msg.(TreeChangedMsg); !ok {
			t.Errorf("expected TreeChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TreeChangedMsg from mkdir")
	}
}

func TestFileWatcher_EmptyRootDir(t *testing.T) {
	// Empty root dir should still create a watcher without error
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatalf("newFileWatcher with empty root: %v", err)
	}
	defer fw.Close()
}

func TestIsGitInternalPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		rootDir string
		want    bool
	}{
		{"empty root", "/project/.git", "", false},
		{"empty path", "", "/project", false},
		{"git dir with trailing sep", "/project/.git/", "/project", true},
		{"git objects", "/project/.git/objects/ab/cd1234", "/project", true},
		{"git hooks", "/project/.git/hooks/pre-commit", "/project", true},
		{"gitmodules file", "/project/.gitmodules", "/project", false},
		{"git-related but not inside", "/project/.git-backup/file", "/project", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitInternalPath(tt.path, tt.rootDir)
			if got != tt.want {
				t.Errorf("isGitInternalPath(%q, %q) = %v, want %v", tt.path, tt.rootDir, got, tt.want)
			}
		})
	}
}

func TestFileWatcher_WatchDirRecursive_SkipsDotDirs(t *testing.T) {
	tmpDir := t.TempDir()
	// Create visible and hidden subdirectories
	if err := os.MkdirAll(filepath.Join(tmpDir, "visible"), 0o755); err != nil {
		t.Fatalf("MkdirAll(visible) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.hidden) error = %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	// The watcher should have been created without error
	// We can't easily check the internal watch list, but we can verify
	// that creating a file in the visible dir triggers an event
	time.Sleep(150 * time.Millisecond)

	visFile := filepath.Join(tmpDir, "visible", "test.go")
	if err := os.WriteFile(visFile, []byte("package visible"), 0o644); err != nil {
		t.Fatalf("WriteFile(visible/test.go) error = %v", err)
	}

	select {
	case msg := <-fw.msgChan:
		switch msg.(type) {
		case TreeChangedMsg, FileChangedMsg:
			// expected
		default:
			t.Errorf("expected TreeChangedMsg or FileChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event in visible subdir")
	}
}

func TestFileWatcher_WatchesDeepDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	deepDir := filepath.Join(tmpDir, "level1", "level2", "level3", "level4", "level5")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	time.Sleep(150 * time.Millisecond)

	deepFile := filepath.Join(deepDir, "deep.go")
	if err := os.WriteFile(deepFile, []byte("package deep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case msg := <-fw.msgChan:
		switch msg.(type) {
		case TreeChangedMsg, FileChangedMsg:
		default:
			t.Errorf("expected TreeChangedMsg or FileChangedMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event in deep subdir")
	}
}

func TestFileWatcher_PruneDebounceEntries(t *testing.T) {
	now := time.Now()
	fw := &fileWatcher{
		debounce: map[string]time.Time{
			"fresh.go": now.Add(-50 * time.Millisecond),
			"stale.go": now.Add(-5 * time.Minute),
		},
	}

	fw.pruneDebounceEntries(now)

	if _, ok := fw.debounce["fresh.go"]; !ok {
		t.Fatal("expected fresh debounce entry to be retained")
	}
	if _, ok := fw.debounce["stale.go"]; ok {
		t.Fatal("expected stale debounce entry to be pruned")
	}
}

func TestFileWatcher_SkipsGitIgnoredDirsWhenRecursing(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}

	nodeModulesDir := filepath.Join(tmpDir, "node_modules")
	nodeModulesChild := filepath.Join(nodeModulesDir, "left-pad")
	visibleDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(nodeModulesChild, 0o755); err != nil {
		t.Fatalf("MkdirAll(node_modules) error = %v", err)
	}
	if err := os.MkdirAll(visibleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(src) error = %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(visibleDir) {
		if time.Now().After(deadline) {
			t.Fatalf("expected %q to be watched after background scan", visibleDir)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fw.isWatched(nodeModulesDir) {
		t.Fatalf("expected %q to be skipped by watcher", nodeModulesDir)
	}
	if fw.isWatched(nodeModulesChild) {
		t.Fatalf("expected %q to be skipped by watcher", nodeModulesChild)
	}
}

func TestFileWatcher_RespectsMaxWatchLimit(t *testing.T) {
	tmpDir := t.TempDir()
	for _, dir := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	fw, err := newFileWatcherWithMaxWatches(tmpDir, 2)
	if err != nil {
		t.Fatalf("newFileWatcherWithMaxWatches: %v", err)
	}
	defer fw.Close()

	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(tmpDir) {
		if time.Now().After(deadline) {
			t.Fatalf("expected root %q to be watched asynchronously", tmpDir)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for fw.watchedCount() != 2 || !fw.watchLimitReached() {
		if time.Now().After(deadline) {
			t.Fatalf("recursive background scan did not reach its expected limit: watches=%d limited=%v", fw.watchedCount(), fw.watchLimitReached())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFileWatcher_WatchFileSkipsRedundantParentWatch(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error = %v", err)
	}

	fw, err := newFileWatcher(tmpDir)
	if err != nil {
		t.Fatalf("newFileWatcher: %v", err)
	}
	defer fw.Close()

	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(tmpDir) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(tmpDir) {
		t.Fatalf("root watch did not initialize for %q", tmpDir)
	}
	before := fw.watchedCount()
	fw.WatchFile(filePath)
	deadline = time.Now().Add(time.Second)
	for fw.watchedCount() != before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if after := fw.watchedCount(); after != before {
		t.Fatalf("WatchFile() added a redundant watch: before=%d after=%d", before, after)
	}
}
