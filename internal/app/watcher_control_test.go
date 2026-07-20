package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileWatcherWatchControlDoesNotBlockUpdateCallers(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	path := filepath.Join(t.TempDir(), "active.go")
	entered := make(chan struct{})
	release := make(chan struct{})
	removed := make(chan struct{})
	var once sync.Once
	fw.watchAdd = func(string) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	fw.watchRemove = func(string) error {
		close(removed)
		return nil
	}

	started := time.Now()
	fw.WatchFile(path)
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("WatchFile blocked for %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("watch worker did not start")
	}

	started = time.Now()
	fw.UnwatchFile(path)
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("UnwatchFile blocked while Add was stalled for %s", elapsed)
	}
	if fw.isOpenFile(path) {
		t.Fatal("UnwatchFile did not update openFiles immediately")
	}

	close(release)
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("queued unwatch did not run after the stalled add")
	}
	if fw.isWatched(path) {
		t.Fatal("queued unwatch did not run after the stalled add")
	}
}

func TestFileWatcherWatchControlPreservesLatestRewatchAfterStalledRemove(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	path := filepath.Join(t.TempDir(), "active.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fw.addWatch(path) {
		t.Fatal("initial watch was not installed")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	fw.watchRemove = func(string) error {
		close(entered)
		<-release
		return nil
	}

	fw.UnwatchFile(path)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("remove worker did not start")
	}

	started := time.Now()
	fw.WatchFile(path)
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("WatchFile blocked while Remove was stalled for %s", elapsed)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for !fw.isWatched(path) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(path) || !fw.isOpenFile(path) {
		t.Fatal("latest WatchFile request was not restored after queued removal")
	}
}

func TestFileWatcherWatchControlReconcilesRequestsDroppedByFullQueue(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.go")
	fw.mu.Lock()
	fw.openFiles[stale] = struct{}{}
	fw.watched[stale] = struct{}{}
	fw.fileWatches[stale] = struct{}{}
	fw.mu.Unlock()

	blocked := filepath.Join(dir, "blocked.go")
	entered := make(chan struct{})
	release := make(chan struct{})
	fw.watchAdd = func(path string) error {
		if path == blocked {
			close(entered)
			<-release
		}
		return nil
	}
	fw.watchRemove = func(string) error { return nil }

	fw.WatchFile(blocked)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("control worker did not block on the first Add")
	}
	for i := 0; i < maxWatchControlOps; i++ {
		fw.WatchFile(filepath.Join(dir, fmt.Sprintf("queued-%d.go", i)))
	}
	missed := filepath.Join(dir, "reconciled.go")
	fw.WatchFile(missed) // Overflow: handled by the coalesced reconciliation.
	fw.UnwatchFile(stale)
	close(release)

	deadline := time.Now().Add(time.Second)
	for (!fw.isWatched(missed) || fw.isWatched(stale)) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fw.isWatched(missed) || fw.isWatched(stale) {
		t.Fatalf("reconciliation lost state: missed watched=%v stale watched=%v", fw.isWatched(missed), fw.isWatched(stale))
	}
}

func TestFileWatcherCloseCancelsQueuedWatchControl(t *testing.T) {
	fw, err := newFileWatcher("")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked.go")
	queued := filepath.Join(dir, "queued.go")
	entered := make(chan struct{})
	release := make(chan struct{})
	var queuedAdded atomic.Bool
	fw.watchAdd = func(path string) error {
		if path == blocked {
			close(entered)
			<-release
		}
		if path == queued {
			queuedAdded.Store(true)
		}
		return nil
	}

	fw.WatchFile(blocked)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("control worker did not block on Add")
	}
	fw.WatchFile(queued)
	fw.Close()
	close(release)
	select {
	case <-fw.done:
	case <-time.After(time.Second):
		t.Fatal("Close did not stop the control worker")
	}
	if queuedAdded.Load() {
		t.Fatal("queued watch control ran after Close")
	}
}
