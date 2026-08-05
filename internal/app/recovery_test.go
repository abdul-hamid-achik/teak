package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/session"
	"teak/internal/text"
)

func newRecoveryModel(t *testing.T, rootDir string) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agent.Enabled = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.editors = nil
	model.tabBar.Tabs = nil
	model.activeTab = 0
	model.tabBar.ActiveIdx = 0
	model.welcome = nil
	t.Cleanup(model.cleanup)
	return model
}

// drainSessionSave runs a session save command and fails the test if the
// write reports an error.
func drainSessionSave(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a session save command")
	}
	result, ok := cmd().(sessionSaveResultMsg)
	if !ok {
		t.Fatalf("session save command returned %T", cmd())
	}
	if result.err != nil {
		t.Fatalf("session save failed: %v", result.err)
	}
}

func TestSessionSavePersistsRecoveryRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	model := newRecoveryModel(t, t.TempDir())

	// One dirty file buffer and one untitled buffer with content.
	addDirtyEditor(t, &model, "dirty.txt", "disk version\n", "edited version\n")
	createdAny, _ := model.newUntitledTab()
	model = createdAny.(Model)
	model.activeEditor().Buffer.InsertAtCursor([]byte("scratch work"))

	updated, cmd := model.requestSessionSave()
	model = updated
	drainSessionSave(t, cmd)

	records, err := session.LoadRecovery(model.rootDir)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	var sawDirty, sawUntitled bool
	for _, record := range records {
		if record.FilePath != "" && filepath.Base(record.FilePath) == "dirty.txt" {
			sawDirty = true
			if string(record.Content) != "edited version\n" {
				t.Errorf("dirty record content = %q, want the edited buffer", record.Content)
			}
		}
		if record.Untitled {
			sawUntitled = true
			if string(record.Content) != "scratch work" {
				t.Errorf("untitled record content = %q, want the scratch buffer", record.Content)
			}
		}
	}
	if !sawDirty || !sawUntitled {
		t.Fatalf("recovery records = %+v, want the dirty file and the untitled buffer", records)
	}
}

func TestSessionSaveClearsRecoveryWhenEverythingSaved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	model := newRecoveryModel(t, t.TempDir())
	addDirtyEditor(t, &model, "dirty.txt", "disk\n", "edited\n")

	updated, cmd := model.requestSessionSave()
	model = updated
	resultMsg := cmd()
	if result, ok := resultMsg.(sessionSaveResultMsg); !ok || result.err != nil {
		t.Fatalf("first save = %#v, want success", resultMsg)
	}
	modelAny, _ := model.Update(resultMsg)
	model = modelAny.(Model)

	// Save the buffer itself: nothing dirty remains, so the recovery set must
	// clear rather than keep resurrecting the now-saved edit.
	for i, ed := range model.editors {
		if ed.Buffer.FilePath != "" && ed.Buffer.Dirty() {
			if err := ed.Buffer.Save(); err != nil {
				t.Fatalf("Save: %v", err)
			}
			model.tabBar.Tabs[i].Dirty = false
		}
	}

	updated, cmd = model.requestSessionSave()
	model = updated
	drainSessionSave(t, cmd)

	records, err := session.LoadRecovery(model.rootDir)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("recovery records after clean save = %+v, want none", records)
	}
}

func TestSessionRestoreAppliesRecoveryOverDiskContent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	kept := filepath.Join(root, "kept.go")
	if err := os.WriteFile(kept, []byte("disk content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveRecovery(root, []session.RecoveryRecord{{FilePath: kept, Content: []byte("recovered content\n")}}); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}

	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: kept}}}, nil
	}
	defer func() { loadSessionForRestore = oldLoad }()

	model := newRecoveryModel(t, root)
	result := sessionRestoreCmd(model.sessionRestoreCtx, model.sessionRestoreGeneration, root)()
	restoreResult, ok := result.(sessionRestoreResultMsg)
	if !ok || len(restoreResult.Recovery) != 1 || restoreResult.Recovery[0].Snapshot == nil {
		t.Fatalf("session restore recovery was not prepared off Update: %#v", result)
	}
	updated, command := model.Update(result)
	model = updated.(Model)
	if command == nil {
		t.Fatal("session restore did not schedule file loads")
	}
	loadedAny, _ := model.Update(command())
	model = loadedAny.(Model)

	ed := model.activeEditor()
	if ed == nil {
		t.Fatal("no editor after restore")
	}
	if got := ed.Buffer.Content(); got != "recovered content\n" {
		t.Fatalf("buffer content = %q, want the recovery record to win over disk", got)
	}
	if !ed.Buffer.Dirty() {
		t.Fatal("recovered buffer must be dirty")
	}
	if !model.tabBar.Tabs[model.activeTab].Dirty {
		t.Fatal("recovered tab must show dirty")
	}
}

func TestSessionRestoreRecreatesUntitledRecoveryWithoutTabs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := session.SaveRecovery(root, []session.RecoveryRecord{{Untitled: true, Content: []byte("unsaved notes")}}); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}

	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: -1}, nil
	}
	defer func() { loadSessionForRestore = oldLoad }()

	model := newRecoveryModel(t, root)
	result := sessionRestoreCmd(model.sessionRestoreCtx, model.sessionRestoreGeneration, root)()
	updated, command := model.Update(result)
	model = updated.(Model)
	if command == nil {
		t.Fatal("empty session with untitled recovery still needs a restore pass")
	}
	loadedAny, _ := model.Update(command())
	model = loadedAny.(Model)

	if len(model.editors) != 1 {
		t.Fatalf("editors = %d, want the recreated untitled buffer", len(model.editors))
	}
	ed := model.editors[0]
	if got := ed.Buffer.Content(); got != "unsaved notes" {
		t.Fatalf("untitled buffer content = %q, want the recovered text", got)
	}
	if !ed.Buffer.Dirty() || ed.Buffer.FilePath != "" {
		t.Fatalf("recovered untitled buffer dirty=%v path=%q, want dirty and untitled", ed.Buffer.Dirty(), ed.Buffer.FilePath)
	}
}

func TestQuitWithoutSavingClearsRecovery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	model := newRecoveryModel(t, t.TempDir())
	addDirtyEditor(t, &model, "dirty.txt", "disk\n", "edited\n")

	// First persist a recovery record for the dirty buffer.
	updated, cmd := model.requestSessionSave()
	model = updated
	firstResult := cmd()
	if result, ok := firstResult.(sessionSaveResultMsg); !ok || result.err != nil {
		t.Fatalf("first save = %#v, want success", firstResult)
	}
	// Deliver the result so the save slot frees up for the final quit write.
	modelAny, _ := model.Update(firstResult)
	model = modelAny.(Model)
	if records, _ := session.LoadRecovery(model.rootDir); len(records) == 0 {
		t.Fatal("expected a recovery record before quit")
	}

	// Quit Without Saving: the final write must clear the records.
	model.recoverySuppressed = true
	updated, cmd = model.finalizeQuit()
	model = updated
	if cmd != nil {
		if msg := cmd(); msg != nil {
			modelAny, _ := model.Update(msg)
			model = modelAny.(Model)
		}
	}
	records, err := session.LoadRecovery(model.rootDir)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("recovery records after quit-without-saving = %+v, want cleared", records)
	}
}

func TestRecoveryPrepsRespectSuppression(t *testing.T) {
	model := newRecoveryModel(t, t.TempDir())
	addDirtyEditor(t, &model, "dirty.txt", "disk\n", "edited\n")

	if preps := model.recoveryPreps(); len(preps) != 1 {
		t.Fatalf("recoveryPreps() = %d records, want 1", len(preps))
	}
	model.recoverySuppressed = true
	if preps := model.recoveryPreps(); len(preps) != 0 {
		t.Fatalf("suppressed recoveryPreps() = %d records, want 0", len(preps))
	}
}

func TestHandleRecoveryLoadedStandalone(t *testing.T) {
	model := newRecoveryModel(t, t.TempDir())

	msg := recoveryLoadedMsg{Records: []preparedRecoveryRecord{
		{FilePath: "/nonexistent/elsewhere.txt", Snapshot: text.NewFromString("recovered file")},
		{Untitled: true, Snapshot: text.NewFromString("recovered untitled")},
	}}
	updatedAny, _ := model.handleRecoveryLoaded(msg)
	model = updatedAny.(Model)

	if len(model.editors) != 2 {
		t.Fatalf("editors = %d, want both recovered buffers", len(model.editors))
	}
	var fileContent, untitledContent string
	for _, ed := range model.editors {
		if ed.Buffer.FilePath != "" {
			fileContent = ed.Buffer.Content()
			if !ed.Buffer.Dirty() {
				t.Error("recovered file buffer must be dirty")
			}
		} else {
			untitledContent = ed.Buffer.Content()
			if !ed.Buffer.Dirty() {
				t.Error("recovered untitled buffer must be dirty")
			}
		}
	}
	if fileContent != "recovered file" || untitledContent != "recovered untitled" {
		t.Fatalf("recovered contents = %q / %q", fileContent, untitledContent)
	}
}

func TestRecoveryLoadCmdPreparesOwnedSnapshots(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := session.SaveRecovery(root, []session.RecoveryRecord{{
		FilePath: filepath.Join(root, "main.go"),
		CRLF:     true,
		Content:  []byte("recovered\n"),
	}}); err != nil {
		t.Fatalf("SaveRecovery() error = %v", err)
	}

	msg, ok := recoveryLoadCmd(root)().(recoveryLoadedMsg)
	if !ok {
		t.Fatalf("recoveryLoadCmd() returned unexpected message")
	}
	if len(msg.Records) != 1 {
		t.Fatalf("prepared records = %d, want 1", len(msg.Records))
	}
	record := msg.Records[0]
	if record.Snapshot == nil || record.Snapshot.String() != "recovered\n" {
		t.Fatalf("prepared snapshot = %#v", record.Snapshot)
	}
	if record.LineEnding != text.CRLF {
		t.Fatalf("line ending = %v, want CRLF", record.LineEnding)
	}
}

func TestExistingBufferRecoveryComparesBeforeApplying(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "disk\n")
	ed := model.activeEditor()
	recovered := text.NewFromString("recovered\n")
	overlayCtx := model.overlayRequests.start(overlayRequestHover)
	documentCtx := model.documentRequests.start(documentRequestDefinition, ed.Buffer.FilePath)

	updatedAny, compareCmd := model.handleRecoveryLoaded(recoveryLoadedMsg{Records: []preparedRecoveryRecord{{
		FilePath: ed.Buffer.FilePath,
		Snapshot: recovered,
	}}})
	model = updatedAny.(Model)
	if compareCmd == nil {
		t.Fatal("existing clean buffer did not schedule a background comparison")
	}
	if got := model.activeEditor().Buffer.Content(); got != "disk\n" {
		t.Fatalf("Update applied recovery before comparison: %q", got)
	}

	comparisonMsg, ok := compareCmd().(recoveryComparisonsMsg)
	if !ok {
		t.Fatalf("comparison command returned unexpected message")
	}
	resultAny, postCmd := model.Update(comparisonMsg)
	result := resultAny.(Model)
	if postCmd == nil {
		t.Fatal("applied recovery did not schedule editor reconciliation")
	}
	if result.activeEditor().Buffer.Rope() != recovered {
		t.Fatal("recovery did not install the prepared rope by identity")
	}
	if !result.activeEditor().Buffer.Dirty() {
		t.Fatal("recovered buffer is not dirty")
	}
	assertContextCanceled(t, "overlay request", overlayCtx)
	assertContextCanceled(t, "document request", documentCtx)
}

func TestExistingBufferRecoveryDiscardsEqualOrStaleResults(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		mutate bool
	}{
		{name: "equal content", value: "disk\n"},
		{name: "buffer changed during comparison", value: "recovered\n", mutate: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
			addDirtyEditor(t, &model, "main.go", "disk\n", "disk\n")
			ed := model.activeEditor()
			updatedAny, compareCmd := model.handleRecoveryLoaded(recoveryLoadedMsg{Records: []preparedRecoveryRecord{{
				FilePath: ed.Buffer.FilePath,
				Snapshot: text.NewFromString(tt.value),
			}}})
			model = updatedAny.(Model)
			msg := compareCmd().(recoveryComparisonsMsg)
			if tt.mutate {
				model.activeEditor().Buffer.InsertAtCursor([]byte("live "))
			}

			resultAny, postCmd := model.Update(msg)
			result := resultAny.(Model)
			if postCmd != nil {
				t.Fatal("discarded recovery scheduled follow-up work")
			}
			want := "disk\n"
			if tt.mutate {
				want = "live disk\n"
			}
			if got := result.activeEditor().Buffer.Content(); got != want {
				t.Fatalf("buffer content = %q, want %q", got, want)
			}
		})
	}
}
