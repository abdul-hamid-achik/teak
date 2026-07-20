package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"teak/internal/config"
	"teak/internal/text"
)

func TestOpenExternalFileCloseTabRemovesWatch(t *testing.T) {
	rootDir := t.TempDir()
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "external.go")
	if err := os.WriteFile(externalFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(external.go) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer func() {
		model.cleanup()
	}()

	if model.watcher == nil {
		t.Fatal("expected model watcher to be initialized")
	}
	requireFileWatchState(t, model.watcher, rootDir, true)
	baseWatchCount := model.watcher.watchedCount()

	openedModel, _ := model.openFilePinned(externalFile)
	model = openedModel.(Model)

	loadedModel, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:     externalFile,
		Snapshot: text.NewFromString("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	requireFileWatchState(t, model.watcher, externalFile, true)
	if got := model.watcher.watchedCount(); got != baseWatchCount+1 {
		t.Fatalf("watchedCount() after open = %d, want %d", got, baseWatchCount+1)
	}

	closedModel, _ := model.closeTab(model.activeTab)
	model = closedModel.(Model)

	requireFileWatchState(t, model.watcher, externalFile, false)
	if got := model.watcher.watchedCount(); got != baseWatchCount {
		t.Fatalf("watchedCount() after close = %d, want %d", got, baseWatchCount)
	}
}

func TestReplacingPreviewTabRemovesOldExternalWatch(t *testing.T) {
	rootDir := t.TempDir()
	externalDir := t.TempDir()
	firstFile := filepath.Join(externalDir, "first.go")
	secondFile := filepath.Join(externalDir, "second.go")
	for _, file := range []string{firstFile, secondFile} {
		if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", filepath.Base(file), err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer func() {
		model.cleanup()
	}()

	if model.watcher == nil {
		t.Fatal("expected model watcher to be initialized")
	}
	requireFileWatchState(t, model.watcher, rootDir, true)
	baseWatchCount := model.watcher.watchedCount()

	openedModel, _ := model.openFile(firstFile)
	model = openedModel.(Model)
	loadedModel, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:     firstFile,
		Snapshot: text.NewFromString("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	requireFileWatchState(t, model.watcher, firstFile, true)

	replacedModel, _ := model.openFile(secondFile)
	model = replacedModel.(Model)

	requireFileWatchState(t, model.watcher, firstFile, false)

	loadedModel, _ = model.handleFileLoaded(FileLoadedMsg{
		Path:     secondFile,
		Snapshot: text.NewFromString("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	requireFileWatchState(t, model.watcher, secondFile, true)
	if got := model.watcher.watchedCount(); got != baseWatchCount+1 {
		t.Fatalf("watchedCount() after replacement = %d, want %d", got, baseWatchCount+1)
	}
}

func requireFileWatchState(t *testing.T, watcher *fileWatcher, path string, want bool) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if watcher.isWatched(path) == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("watch state for %q = %v, want %v", path, watcher.isWatched(path), want)
		case <-poll.C:
		}
	}
}
