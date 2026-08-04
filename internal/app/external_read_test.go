package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"teak/internal/config"
	"teak/internal/text"
)

func detachWatcherForExternalReadTest(model *Model) {
	if model.watcher != nil {
		model.watcher.Close()
		model.watcher = nil
	}
}

func TestExternalFallbackReadsAreSerializedAcrossOpenFiles(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	first := addDirtyEditor(t, &model, "first.go", "disk one\n", "local one\n")
	second := addDirtyEditor(t, &model, "second.go", "disk two\n", "local two\n")
	firstPath := model.editors[first].Buffer.FilePath
	secondPath := model.editors[second].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)

	started := make(chan string, 2)
	release := make(chan struct{}, 2)
	var active atomic.Int32
	var maximum atomic.Int32
	model.externalFileReader = func(_ context.Context, path string) ([]byte, error) {
		current := active.Add(1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		started <- path
		<-release
		active.Add(-1)
		return []byte("external " + filepath.Base(path) + "\n"), nil
	}

	updatedAny, firstCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: firstPath, Observation: 1, NeedsRead: true,
	})
	model = updatedAny.(Model)
	if firstCmd == nil {
		t.Fatal("first fallback read did not start")
	}
	firstResult := make(chan any, 1)
	go func() { firstResult <- firstCmd() }()
	if got := <-started; got != firstPath {
		t.Fatalf("first read path = %q, want %q", got, firstPath)
	}

	updatedAny, secondCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: secondPath, Observation: 2, NeedsRead: true,
	})
	model = updatedAny.(Model)
	if secondCmd != nil {
		t.Fatal("second fallback read started while the first was in flight")
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent readers = %d, want 1", got)
	}

	release <- struct{}{}
	prepared := <-firstResult
	updatedAny, nextCmd := model.Update(prepared)
	model = updatedAny.(Model)
	if nextCmd == nil {
		t.Fatal("second queued fallback read was not started after the first")
	}
	secondResult := make(chan any, 1)
	go func() { secondResult <- nextCmd() }()
	if got := <-started; got != secondPath {
		t.Fatalf("second read path = %q, want %q", got, secondPath)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent readers = %d after handoff, want 1", got)
	}
	release <- struct{}{}
	updatedAny, _ = model.Update(<-secondResult)
	model = updatedAny.(Model)
	if !model.hasExternalConflict(firstPath) || !model.hasExternalConflict(secondPath) {
		t.Fatal("serialized reads did not preserve conflict signals for both dirty files")
	}
}

func TestOwnWriteRecheckMatchingBytesDoesNotCreateConflict(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "saved by teak\n")
	path := model.editors[0].Buffer.FilePath
	expected := model.editors[0].Buffer.Rope()
	model.editors[0].Buffer.MarkSavedSnapshot(path, expected)
	model.lastSaveWatcherWatermarks[path] = 1
	change := FileChangedMsg{
		Path:              path,
		Observation:       1,
		OwnWriteCandidate: true,
		OwnWriteSnapshot:  expected,
		RequiresConflict:  true,
	}
	model.externalReads.inFlight = true
	model.externalReads.current = change

	updatedAny, _ := model.handleExternalFileReadPrepared(externalFileReadPreparedMsg{
		Change:        change,
		Snapshot:      text.NewFromString("saved by teak\n"),
		OwnWriteMatch: true,
	})
	updated := updatedAny.(Model)
	if updated.hasExternalConflict(path) || updated.unsavedConfirm != nil {
		t.Fatalf("matching bytes from Teak's own atomic save created an external conflict: conflict=%#v confirm=%#v dirty=%v observed=%d watermark=%d status=%q", updated.externalConflicts[path], updated.unsavedConfirm, updated.editors[0].Buffer.Dirty(), updated.externalChangeObserved[path], updated.lastSaveWatcherWatermarks[path], updated.status)
	}
}

func TestExternalFallbackReadKeepsOnlyNewestObservationPerPath(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)

	started := make(chan int, 2)
	release := make(chan struct{}, 2)
	var calls atomic.Int32
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		call := int(calls.Add(1))
		started <- call
		<-release
		if call == 1 {
			return []byte("stale external\n"), nil
		}
		return []byte("new external\n"), nil
	}

	updatedAny, firstCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 10, NeedsRead: true,
	})
	model = updatedAny.(Model)
	result := make(chan any, 1)
	go func() { result <- firstCmd() }()
	<-started

	updatedAny, secondCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 11, NeedsRead: true,
	})
	model = updatedAny.(Model)
	if secondCmd != nil {
		t.Fatal("newer same-path read started concurrently")
	}

	release <- struct{}{}
	updatedAny, nextCmd := model.Update(<-result)
	model = updatedAny.(Model)
	if model.hasExternalConflict(path) {
		t.Fatal("stale in-flight snapshot was applied before the newer observation")
	}
	if nextCmd == nil {
		t.Fatal("newest same-path observation was not retained")
	}
	go func() { result <- nextCmd() }()
	<-started
	release <- struct{}{}
	updatedAny, _ = model.Update(<-result)
	model = updatedAny.(Model)

	conflict := model.externalConflicts[path]
	if conflict.Observation != 11 {
		t.Fatalf("conflict observation = %d, want 11", conflict.Observation)
	}
	if conflict.Snapshot == nil || conflict.Snapshot.String() != "new external\n" {
		t.Fatalf("conflict snapshot = %v, want newest external bytes", conflict.Snapshot)
	}
}

func TestExternalReadCompletedAfterSaveWatermarkRequiresConflict(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local save\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		return []byte("external before save\n"), nil
	}

	updatedAny, readCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 12, NeedsRead: true,
	})
	model = updatedAny.(Model)
	if readCmd == nil {
		t.Fatal("external observation did not schedule its serialized read")
	}

	saveCmd := model.beginSaveForTab(0, false, false)
	saved := requireFileSavedMsg(t, saveCmd)
	saved.WatcherWatermark = 12
	updatedAny, _ = model.Update(saved)
	model = updatedAny.(Model)

	// The read was scheduled by the older observation but completes after the
	// save watermark became known. It must not replace the just-saved buffer as
	// a clean reload; the user must decide which snapshot to keep.
	updatedAny, _ = model.Update(readCmd())
	model = updatedAny.(Model)

	if got := model.editors[0].Buffer.Content(); got != "local save\n" {
		t.Fatalf("late external read replaced the saved buffer with %q", got)
	}
	conflict, ok := model.externalConflicts[path]
	if !ok || !conflict.PostSave || conflict.Snapshot == nil ||
		conflict.Snapshot.String() != "external before save\n" {
		t.Fatalf("post-save conflict = %#v, want the late external snapshot", conflict)
	}
	if model.unsavedConfirm == nil {
		t.Fatal("late external read did not require an explicit user decision")
	}
}

func TestExternalReadFailureAfterSaveWatermarkRecordsPostSaveConflict(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local save\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)
	readErr := errors.New("injected late read failure")
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		return nil, readErr
	}

	updatedAny, readCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 13, NeedsRead: true,
	})
	model = updatedAny.(Model)
	saveCmd := model.beginSaveForTab(0, false, false)
	saved := requireFileSavedMsg(t, saveCmd)
	saved.WatcherWatermark = 13
	updatedAny, _ = model.Update(saved)
	model = updatedAny.(Model)
	updatedAny, _ = model.Update(readCmd())
	model = updatedAny.(Model)

	conflict, ok := model.externalConflicts[path]
	if !ok || !conflict.PostSave || conflict.Observation != 13 {
		t.Fatalf("late unreadable conflict = %#v, want post-save observation 13", conflict)
	}
	if got := model.editors[0].Buffer.Content(); got != "local save\n" {
		t.Fatalf("late unreadable observation changed the saved buffer to %q", got)
	}
}

func TestExternalReadOlderThanAppliedSnapshotIsDiscarded(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "disk\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		return []byte("stale external\n"), nil
	}

	updatedAny, readCmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 20, NeedsRead: true,
	})
	model = updatedAny.(Model)
	updatedAny, _ = model.handleExternalFileChange(FileChangedMsg{
		Path:        path,
		Observation: 21,
		Snapshot:    text.NewFromString("new external\n"),
	})
	model = updatedAny.(Model)
	updatedAny, _ = model.Update(readCmd())
	model = updatedAny.(Model)

	if got := model.editors[0].Buffer.Content(); got != "new external\n" {
		t.Fatalf("older serialized read replaced newer observation with %q", got)
	}
	if got := model.externalChangeObserved[path]; got != 21 {
		t.Fatalf("applied observation regressed to %d, want 21", got)
	}
}

func TestMissingOpenFileCreatesExplicitConflictWithoutDiscardingBuffer(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "removed.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	updatedAny, cmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 7, NeedsRead: true, Missing: true,
	})
	model = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("missing-file signal did not schedule its serialized verification")
	}
	updatedAny, _ = model.Update(cmd())
	model = updatedAny.(Model)

	if got := model.editors[0].Buffer.Content(); got != "local\n" {
		t.Fatalf("missing-file handling discarded local buffer: %q", got)
	}
	conflict, ok := model.externalConflicts[path]
	if !ok || !conflict.Missing || conflict.Observation != 7 {
		t.Fatalf("missing conflict = %#v, want explicit observation 7", conflict)
	}
	if model.unsavedConfirm == nil {
		t.Fatal("missing open file did not require an explicit user decision")
	}
	if saveCmd := model.beginSaveForTab(0, false, false); saveCmd != nil {
		t.Fatal("ordinary save was not blocked for a removed external file")
	}
}

func TestExternalReadFailureBlocksPotentialOverwrite(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "unreadable.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	detachWatcherForExternalReadTest(&model)
	readErr := errors.New("injected read failure")
	model.externalFileReader = func(context.Context, string) ([]byte, error) {
		return nil, readErr
	}

	updatedAny, cmd := model.handleExternalFileChange(FileChangedMsg{
		Path: path, Observation: 8, NeedsRead: true,
	})
	model = updatedAny.(Model)
	updatedAny, _ = model.Update(cmd())
	model = updatedAny.(Model)
	if !model.hasExternalConflict(path) {
		t.Fatal("unverifiable external change did not block a later overwrite")
	}
	if saveCmd := model.beginSaveForTab(0, false, false); saveCmd != nil {
		t.Fatal("save proceeded after external bytes could not be verified")
	}
}
