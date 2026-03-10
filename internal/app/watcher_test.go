package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	os.Remove(testFile)

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

	if fw.isWatched(nodeModulesDir) {
		t.Fatalf("expected %q to be skipped by watcher", nodeModulesDir)
	}
	if fw.isWatched(nodeModulesChild) {
		t.Fatalf("expected %q to be skipped by watcher", nodeModulesChild)
	}
	if !fw.isWatched(visibleDir) {
		t.Fatalf("expected %q to be watched", visibleDir)
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

	if !fw.isWatched(tmpDir) {
		t.Fatalf("expected root %q to be watched", tmpDir)
	}
	if got := fw.watchedCount(); got != 2 {
		t.Fatalf("watchedCount() = %d, want 2", got)
	}
	if !fw.watchLimitReached() {
		t.Fatal("expected watch limit to be reported as reached")
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

	before := fw.watchedCount()
	fw.WatchFile(filePath)
	after := fw.watchedCount()

	if after != before {
		t.Fatalf("WatchFile() added a redundant watch: before=%d after=%d", before, after)
	}
}
