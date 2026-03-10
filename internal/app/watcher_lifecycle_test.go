package app

import (
	"os"
	"path/filepath"
	"testing"

	"teak/internal/config"
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
	baseWatchCount := model.watcher.watchedCount()

	openedModel, _ := model.openFilePinned(externalFile)
	model = openedModel.(Model)

	loadedModel, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:     externalFile,
		Data:     []byte("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	if !model.watcher.isWatched(externalFile) {
		t.Fatalf("expected %q to be watched after file load", externalFile)
	}
	if got := model.watcher.watchedCount(); got != baseWatchCount+1 {
		t.Fatalf("watchedCount() after open = %d, want %d", got, baseWatchCount+1)
	}

	closedModel, _ := model.closeTab(model.activeTab)
	model = closedModel.(Model)

	if model.watcher.isWatched(externalFile) {
		t.Fatalf("expected %q watch to be removed after close", externalFile)
	}
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
	baseWatchCount := model.watcher.watchedCount()

	openedModel, _ := model.openFile(firstFile)
	model = openedModel.(Model)
	loadedModel, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:     firstFile,
		Data:     []byte("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	if !model.watcher.isWatched(firstFile) {
		t.Fatalf("expected %q to be watched after first preview load", firstFile)
	}

	replacedModel, _ := model.openFile(secondFile)
	model = replacedModel.(Model)

	if model.watcher.isWatched(firstFile) {
		t.Fatalf("expected %q watch to be removed when preview tab is replaced", firstFile)
	}

	loadedModel, _ = model.handleFileLoaded(FileLoadedMsg{
		Path:     secondFile,
		Data:     []byte("package main\n"),
		TabIndex: model.activeTab,
	})
	model = loadedModel.(Model)

	if !model.watcher.isWatched(secondFile) {
		t.Fatalf("expected %q to be watched after replacement preview load", secondFile)
	}
	if got := model.watcher.watchedCount(); got != baseWatchCount+1 {
		t.Fatalf("watchedCount() after replacement = %d, want %d", got, baseWatchCount+1)
	}
}
