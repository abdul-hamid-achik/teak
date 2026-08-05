package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/text"
)

func TestExternalDirtyChangeRequiresExplicitResolutionBeforeSave(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath

	updatedAny, cmd := model.handleExternalFileChange(FileChangedMsg{Path: path, Snapshot: text.NewFromString("external\n")})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("external change should keep the watcher listener alive")
	}
	if updated.unsavedConfirm == nil {
		t.Fatal("dirty external change should show an explicit conflict dialog")
	}
	if got := updated.editors[0].Buffer.Content(); got != "local\n" {
		t.Fatalf("dirty buffer content = %q, want local content preserved", got)
	}
	if saveCmd := updated.beginSaveForTab(0, false, false); saveCmd != nil {
		t.Fatal("save must be blocked until the external conflict is resolved")
	}
	if got := updated.status; got == "" {
		t.Fatal("blocked save should explain the conflict")
	}
}

func TestExternalConflictReloadDiscardsLocalEditsOnlyWhenChosen(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath

	prepared := text.NewFromString("external\n")
	updatedAny, _ := model.handleExternalFileChange(FileChangedMsg{Path: path, Snapshot: prepared})
	updated := updatedAny.(Model)
	resultAny, _ := updated.Update(externalConflictResolutionMsg{Path: path, Resolution: externalConflictReload})
	result := resultAny.(Model)

	if result.editors[0].Buffer.Rope() != prepared {
		t.Fatal("external reload materialized the watcher-prepared rope")
	}
	if got := result.editors[0].Buffer.Content(); got != "external\n" {
		t.Fatalf("reloaded buffer = %q, want external content", got)
	}
	if result.editors[0].Buffer.Dirty() {
		t.Fatal("reloaded external content should be clean")
	}
	if result.hasExternalConflict(path) {
		t.Fatal("reload should resolve conflict")
	}
}

func TestExternalConflictOverwriteIsExplicitAndThenSaves(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	if err := os.WriteFile(path, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	updatedAny, _ := model.handleExternalFileChange(FileChangedMsg{Path: path, Snapshot: text.NewFromString("external\n")})
	updated := updatedAny.(Model)
	resultAny, cmd := updated.Update(externalConflictResolutionMsg{Path: path, Resolution: externalConflictOverwrite})
	result := resultAny.(Model)

	if cmd == nil {
		t.Fatal("explicit overwrite should immediately retry the blocked save")
	}
	msg := requireFileSavedMsg(t, cmd)
	finalAny, _ := result.Update(msg)
	_ = finalAny.(Model)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "local\n" {
		t.Fatalf("saved content = %q, want local content", got)
	}
}

func TestExternalConflictSaveAsToNewPathRemainsAvailable(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	updatedAny, _ := model.handleExternalFileChange(FileChangedMsg{Path: path, Snapshot: text.NewFromString("external\n")})
	updated := updatedAny.(Model)

	newPath := filepath.Join(model.rootDir, "copy.go")
	if cmd := updated.beginSaveAsForTab(0, newPath); cmd == nil {
		t.Fatal("Save As to a separate path should not overwrite the conflicted file")
	}
}

func TestExternalChangeCoveredBySaveWatermarkStillRequiresDecision(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local save\n")
	path := model.editors[0].Buffer.FilePath
	saveCmd := model.beginSaveForTab(0, false, false)
	if saveCmd == nil {
		t.Fatal("save did not start")
	}
	requestID := pendingRequestIDForPath(model, path)
	savedAny, _ := model.Update(FileSavedMsg{
		Path: path, RequestID: requestID, WatcherWatermark: 12,
	})
	model = savedAny.(Model)
	current := model.editors[0].Buffer.Rope()

	updatedAny, _ := model.handleExternalFileChange(FileChangedMsg{
		Path:        path,
		Snapshot:    text.NewFromString("stale external\n"),
		Observation: 12,
	})
	updated := updatedAny.(Model)

	if updated.editors[0].Buffer.Rope() != current {
		t.Fatal("an external snapshot covered by the save watermark replaced the saved buffer automatically")
	}
	if !updated.hasExternalConflict(path) {
		t.Fatal("save watermark silently discarded an unattributed external change")
	}
	if updated.unsavedConfirm == nil {
		t.Fatal("unattributed external change did not require an explicit decision")
	}
	if saveCmd := updated.beginSaveForTab(0, false, false); saveCmd != nil {
		t.Fatal("a later save was not blocked after the save/change race")
	}

	restoredAny, _ := updated.Update(externalConflictResolutionMsg{
		Path: path, Resolution: externalConflictReload,
	})
	restored := restoredAny.(Model)
	if got := restored.editors[0].Buffer.Content(); got != "stale external\n" {
		t.Fatalf("restored raced snapshot = %q, want preserved external bytes", got)
	}
	if !restored.editors[0].Buffer.Dirty() {
		t.Fatal("raced external snapshot was marked clean even though disk contains the completed local save")
	}
}

func TestExternalReloadRebuildsDerivedEditorState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.WordWrap = true
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "one\ntwo\nthree\n", "local\nchange\n")
	path := model.editors[0].Buffer.FilePath
	model.editors[0].SetSize(40, 8)
	model.editors[0].Folds.SetRegions([]editor.FoldRegion{{StartLine: 0, EndLine: 1, Collapsed: true}})

	updatedAny, cmd := model.reloadExternalFile(path, text.NewFromString("external line that wraps\n"))
	updated := updatedAny.(Model)

	if cmd == nil {
		t.Fatal("reload did not schedule tokenization/LSP/plugin follow-up")
	}
	if len(updated.editors[0].Folds.Regions) != 0 {
		t.Fatal("reload retained fold regions from the previous document")
	}
	if updated.editors[0].Wrap == nil {
		t.Fatal("reload did not rebuild word-wrap state")
	}
	if got := updated.editors[0].Buffer.Cursor; got != (text.Position{}) {
		t.Fatalf("cursor = %#v, want reset origin", got)
	}
}

func TestExternalReloadInvalidatesStaleLSPRequests(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	overlayCtx := model.overlayRequests.start(overlayRequestHover)
	documentCtx := model.documentRequests.start(documentRequestDefinition, path)

	updatedAny, _ := model.reloadExternalFile(path, text.NewFromString("external\n"))
	updated := updatedAny.(Model)

	assertContextCanceled(t, "overlay request", overlayCtx)
	assertContextCanceled(t, "document request", documentCtx)
	if updated.tabBar.Tabs[0].Dirty {
		t.Fatal("clean external reload left the tab dirty")
	}
}

func TestExternalConflictSnapshotBudgetRetainsDecisionWithoutLargeRope(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	path := filepath.Join(model.rootDir, "large.go")
	model.externalConflictBytes = maxExternalConflictBytes - 1

	model.recordExternalConflict(path, text.NewFromString("xx"), 0, false, false)

	conflict, ok := model.externalConflicts[path]
	if !ok {
		t.Fatal("conflict decision was dropped when snapshot budget was full")
	}
	if conflict.Snapshot != nil {
		t.Fatal("conflict retained a snapshot beyond the aggregate budget")
	}
	if conflict.Generation == 0 {
		t.Fatal("conflict did not receive an async reload generation")
	}
}

func TestCompletedOverwriteClearsOnlyConflictCoveredByWatcherWatermark(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath

	model.recordExternalConflict(path, text.NewFromString("external\n"), 12, false, false)
	if err := os.WriteFile(path, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model = model.showExternalConflictConfirmation(path)
	overwritingAny, cmd := model.Update(externalConflictResolutionMsg{
		Path: path, Resolution: externalConflictOverwrite,
	})
	overwriting := overwritingAny.(Model)
	if cmd == nil {
		t.Fatal("authorized overwrite did not start")
	}
	if !overwriting.hasExternalConflict(path) {
		t.Fatal("overwrite cleared its conflict before the write completed")
	}
	saved := requireFileSavedMsg(t, cmd)
	updatedAny, _ := overwriting.Update(saved)
	updated := updatedAny.(Model)
	if updated.hasExternalConflict(path) {
		t.Fatal("completed overwrite retained the exact conflict it authorized")
	}
	if updated.unsavedConfirm != nil {
		t.Fatal("completed overwrite retained the stale external-conflict dialog")
	}

	updated.recordExternalConflict(path, text.NewFromString("newer\n"), 13, false, false)
	if err := os.WriteFile(path, []byte("newer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated = updated.showExternalConflictConfirmation(path)
	overwritingAny, cmd = updated.Update(externalConflictResolutionMsg{
		Path: path, Resolution: externalConflictOverwrite,
	})
	overwriting = overwritingAny.(Model)
	if cmd == nil {
		t.Fatal("second authorized overwrite did not start")
	}
	// A distinct external observation arrives after authorization but before
	// the write completion. Its generation must not be cleared by a watermark.
	changedAny, _ := overwriting.handleExternalFileChange(FileChangedMsg{
		Path: path, Snapshot: text.NewFromString("newest\n"), Observation: 14,
	})
	overwriting = changedAny.(Model)
	saved = requireFileSavedMsg(t, cmd)
	updatedAny, _ = overwriting.Update(saved)
	updated = updatedAny.(Model)
	if !updated.hasExternalConflict(path) {
		t.Fatal("save watermark cleared a genuinely newer external conflict")
	}
	if got := updated.externalConflicts[path].Observation; got != 14 {
		t.Fatalf("retained conflict observation = %d, want 14", got)
	}
}

func TestAsyncExternalReloadPreservesEditsMadeAfterReloadChoice(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	if err := os.WriteFile(path, []byte("external large version\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	model.recordExternalConflict(path, nil, 4, false, false)
	loadingAny, cmd := model.Update(externalConflictResolutionMsg{
		Path:       path,
		Resolution: externalConflictReload,
	})
	loading := loadingAny.(Model)
	if cmd == nil {
		t.Fatal("snapshot-free conflict did not schedule an async reload")
	}
	loading.editors[0].Buffer.InsertAtCursor([]byte("newer "))

	prepared := cmd()
	finalAny, _ := loading.Update(prepared)
	final := finalAny.(Model)
	if got := final.editors[0].Buffer.Content(); got != "local\nnewer " {
		t.Fatalf("stale async reload replaced newer local edits with %q", got)
	}
	if !final.hasExternalConflict(path) {
		t.Fatal("stale async reload cleared the unresolved conflict")
	}
	if final.unsavedConfirm == nil {
		t.Fatal("stale async reload did not restore the conflict prompt")
	}
}

func TestClosingLastFileOwnerClearsRetainedExternalState(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "closed.go", "disk\n", "local\n")
	path := model.editors[0].Buffer.FilePath
	model.recordExternalConflict(path, text.NewFromString("old external\n"), 21, false, false)
	model.externalChangeObserved[path] = 21
	model.lastSaveWatcherWatermarks[path] = 20
	model.externalReads.pending = map[string]FileChangedMsg{
		path: {Path: path, Observation: 22, NeedsRead: true},
	}

	closedAny, _ := model.closeTab(0)
	closed := closedAny.(Model)
	if closed.hasExternalConflict(path) {
		t.Fatal("closing the last path owner retained an obsolete conflict snapshot")
	}
	if _, ok := closed.externalChangeObserved[path]; ok {
		t.Fatal("closing the last path owner retained its observation generation")
	}
	if _, ok := closed.lastSaveWatcherWatermarks[path]; ok {
		t.Fatal("closing the last path owner retained its save watermark")
	}
	if _, ok := closed.externalReads.pending[path]; ok {
		t.Fatal("closing the last path owner retained a queued disk read")
	}
}

var _ tea.Msg = externalConflictResolutionMsg{}
