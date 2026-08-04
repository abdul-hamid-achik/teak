package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

func newSaveFlowModel(t *testing.T, cfg config.Config, rootDir string) Model {
	t.Helper()

	cfg.Session.Enabled = false
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

func addDirtyEditor(t *testing.T, model *Model, fileName, diskContent, bufferContent string) int {
	t.Helper()

	path := filepath.Join(model.rootDir, fileName)
	if err := os.WriteFile(path, []byte(diskContent), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	buf, err := text.NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile() error = %v", err)
	}
	if bufferContent != diskContent {
		buf.SelectAll()
		buf.InsertAtCursor([]byte(bufferContent))
	}

	cfg := editor.Config{
		TabSize:       model.appCfg.Editor.TabSize,
		InsertTabs:    model.appCfg.Editor.InsertTabs,
		AutoIndent:    model.appCfg.Editor.AutoIndent,
		WordWrap:      model.appCfg.Editor.WordWrap,
		CommentPrefix: editor.CommentPrefixForFile(path),
	}
	ed := editor.New(buf, model.theme, cfg)
	ed.SetSize(80, 24)

	model.editors = append(model.editors, ed)
	idx := model.tabBar.AddTab(filepath.Base(path), path)
	model.tabBar.Tabs[idx].Dirty = buf.Dirty()
	model.activeTab = idx
	model.tabBar.ActiveIdx = idx
	return idx
}

func pendingRequestIDForPath(model Model, path string) int {
	for requestID, req := range model.pendingSaves {
		if req.Path == path {
			return requestID
		}
	}
	return 0
}

func TestSaveFlowPreservesCRLFLineEndings(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	// Disk holds Windows line endings; the buffer loads normalized to LF and
	// the user edits within that view.
	idx := addDirtyEditor(t, &model, "crlf.txt", "line1\r\nline2\r\n", "line1X\nline2\n")
	path := model.editors[idx].Buffer.FilePath

	if model.editors[idx].Buffer.LineEnding() != text.CRLF {
		t.Fatalf("LineEnding() = %v, want CRLF for a CRLF file", model.editors[idx].Buffer.LineEnding())
	}

	cmd := model.beginSaveForTab(idx, false, false)
	requireFileSavedMsg(t, cmd)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != "line1X\r\nline2\r\n" {
		t.Fatalf("saved bytes = %q, want CRLF restored around the edit", data)
	}
}

func requireFileSavedMsg(t *testing.T, cmd tea.Cmd) FileSavedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	if savedMsg, ok := fileSavedMsgFromBatch(msg); ok {
		return savedMsg
	}
	t.Fatalf("cmd() returned %T without a FileSavedMsg", msg)
	return FileSavedMsg{}
}

func fileSavedMsgFromBatch(msg tea.Msg) (FileSavedMsg, bool) {
	switch msg := msg.(type) {
	case FileSavedMsg:
		return msg, true
	case tea.BatchMsg:
		for _, cmd := range msg {
			if cmd == nil {
				continue
			}
			if saved, ok := fileSavedMsgFromBatch(cmd()); ok {
				return saved, true
			}
		}
	}
	return FileSavedMsg{}, false
}

func TestFormatOnSaveAppliedEditsThenSaves(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = true

	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "fmt.Println(1)\n", "fmt.Println( 1 )\n")
	path := model.editors[0].Buffer.FilePath

	requestID := model.nextSaveID()
	model.pendingSaves[requestID] = pendingSaveRequest{TabIndex: 0, Path: path}

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		RequestID: requestID,
		FilePath:  path,
		Status:    lsp.FormatApplied,
		Edits: []lsp.TextEdit{
			{
				StartLine: 0,
				StartCol:  0,
				EndLine:   0,
				EndCol:    len("fmt.Println( 1 )"),
				NewText:   "fmt.Println(1)",
			},
		},
	})
	updated := updatedAny.(Model)

	if got := updated.editors[0].Buffer.Content(); got != "fmt.Println(1)\n" {
		t.Fatalf("formatted buffer = %q", got)
	}

	savedMsg := requireFileSavedMsg(t, cmd)
	finalAny, _ := updated.Update(savedMsg)
	final := finalAny.(Model)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != "fmt.Println(1)\n" {
		t.Fatalf("saved file = %q", string(data))
	}
	if final.tabBar.Tabs[0].Dirty {
		t.Fatal("tab should be clean after save")
	}
}

func TestSavePreflightRejectsExternalChangeBeforeWriterRuns(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk baseline\n", "local edits\n")
	path := model.editors[0].Buffer.FilePath

	cmd := model.beginSaveForTab(0, false, false)
	if cmd == nil {
		t.Fatal("save did not start")
	}
	if err := os.WriteFile(path, []byte("external before writer\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := cmd()
	updatedAny, _ := model.Update(result)
	updated := updatedAny.(Model)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "external before writer\n" {
		t.Fatalf("save preflight overwrote changed disk bytes with %q", got)
	}
	if got := updated.editors[0].Buffer.Content(); got != "local edits\n" {
		t.Fatalf("save preflight discarded local buffer: %q", got)
	}
	if !updated.editors[0].Buffer.Dirty() {
		t.Fatal("blocked save marked local edits clean")
	}
	if !updated.hasExternalConflict(path) || updated.unsavedConfirm == nil {
		t.Fatal("changed save destination did not create an explicit conflict")
	}
	if len(updated.pendingSaves) != 0 {
		t.Fatalf("blocked save retained %d pending requests", len(updated.pendingSaves))
	}
}

func TestFormatOnSaveFallbacksStillSave(t *testing.T) {
	tests := []struct {
		name       string
		status     lsp.FormatStatus
		err        error
		wantStatus string
	}{
		{name: "noop", status: lsp.FormatNoOp, wantStatus: "no formatting changes"},
		{name: "unsupported", status: lsp.FormatUnsupported, wantStatus: "formatting not supported"},
		{name: "error", status: lsp.FormatError, err: errors.New("boom"), wantStatus: "formatting failed: boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Editor.FormatOnSave = true

			model := newSaveFlowModel(t, cfg, t.TempDir())
			addDirtyEditor(t, &model, "main.go", "before\n", "after\n")
			path := model.editors[0].Buffer.FilePath

			requestID := model.nextSaveID()
			model.pendingSaves[requestID] = pendingSaveRequest{TabIndex: 0, Path: path}

			updatedAny, cmd := model.Update(lsp.FormatResultMsg{
				RequestID: requestID,
				FilePath:  path,
				Status:    tt.status,
				Err:       tt.err,
			})
			updated := updatedAny.(Model)

			savedMsg := requireFileSavedMsg(t, cmd)
			finalAny, _ := updated.Update(savedMsg)
			final := finalAny.(Model)

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if string(data) != "after\n" {
				t.Fatalf("saved file = %q", string(data))
			}
			if !strings.Contains(final.status, tt.wantStatus) {
				t.Fatalf("status = %q, want substring %q", final.status, tt.wantStatus)
			}
		})
	}
}

func TestCommandPaletteSaveUsesSaveOrchestrator(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "before\n", "after\n")
	path := model.editors[0].Buffer.FilePath

	updatedAny, cmd := model.Update(commandPaletteMsg{inner: saveRequestMsg{}})
	updated := updatedAny.(Model)

	if len(updated.pendingSaves) != 1 {
		t.Fatalf("pendingSaves = %d, want 1", len(updated.pendingSaves))
	}

	savedMsg := requireFileSavedMsg(t, cmd)
	if savedMsg.RequestID == 0 {
		t.Fatal("expected save request id")
	}

	finalAny, _ := updated.Update(savedMsg)
	final := finalAny.(Model)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != "after\n" {
		t.Fatalf("saved file = %q", string(data))
	}
	if final.tabBar.Tabs[0].Dirty {
		t.Fatal("tab should be clean after command palette save")
	}
}

func TestSaveAsIsAsyncAndReconcilesOnlyAfterSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "before.txt", "before\n", "after\n")

	oldPath := model.editors[0].Buffer.FilePath
	newPath := filepath.Join(model.rootDir, "after.go")
	model.saveAsMode = true
	model.saveAsInput = newPath

	updatedAny, cmd := model.handleSaveAsInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)

	if cmd == nil {
		t.Fatal("Save As must return an asynchronous write command")
	}
	if got := updated.editors[0].Buffer.FilePath; got != oldPath {
		t.Fatalf("buffer path changed before write completed: got %q, want %q", got, oldPath)
	}
	if got := updated.tabBar.Tabs[0].FilePath; got != oldPath {
		t.Fatalf("tab path changed before write completed: got %q, want %q", got, oldPath)
	}
	if len(updated.pendingSaves) != 1 {
		t.Fatalf("pending saves = %d, want 1", len(updated.pendingSaves))
	}

	saved := requireFileSavedMsg(t, cmd)
	finalAny, _ := updated.Update(saved)
	final := finalAny.(Model)

	if got := final.editors[0].Buffer.FilePath; got != newPath {
		t.Fatalf("buffer path after save = %q, want %q", got, newPath)
	}
	if got := final.tabBar.Tabs[0].FilePath; got != newPath {
		t.Fatalf("tab path after save = %q, want %q", got, newPath)
	}
	if final.tabBar.Tabs[0].Dirty {
		t.Fatal("Save As tab should be clean after its snapshot succeeds")
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read Save As destination: %v", err)
	}
	if got, want := string(data), "after\n"; got != want {
		t.Fatalf("Save As data = %q, want %q", got, want)
	}
	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}
	if got, want := string(oldData), "before\n"; got != want {
		t.Fatalf("original file was changed: got %q, want %q", got, want)
	}
}

func TestSaveAsExistingDestinationRequiresConfirmationAndRechecksBeforeOverwrite(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "source.txt", "source\n", "local save as\n")

	oldPath := model.editors[0].Buffer.FilePath
	destination := filepath.Join(model.rootDir, "existing.txt")
	if err := os.WriteFile(destination, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	firstCmd := model.beginSaveAsForTab(0, destination)
	requestID := pendingRequestIDForPath(model, destination)
	if firstCmd == nil || requestID == 0 {
		t.Fatal("Save As did not create an asynchronous pending request")
	}
	firstFailure, ok := firstCmd().(savePreconditionFailedMsg)
	if !ok {
		t.Fatal("Save As replaced an existing destination without a precondition failure")
	}
	updatedAny, _ := model.Update(firstFailure)
	model = updatedAny.(Model)
	if model.unsavedConfirm == nil {
		t.Fatal("existing Save As destination did not require explicit confirmation")
	}
	if _, ok := model.pendingSaves[requestID]; !ok {
		t.Fatal("confirmation discarded the pending Save As snapshot")
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "existing\n" {
		t.Fatalf("destination changed before confirmation: %q, %v", data, err)
	}

	// Even after the user chooses Overwrite, compare against the exact bytes
	// shown by the prompt. A second external change must force a new decision.
	if err := os.WriteFile(destination, []byte("changed after prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updatedAny, retryCmd := model.Update(saveAsDestinationResolutionMsg{
		RequestID: requestID,
		Path:      destination,
		Overwrite: true,
	})
	model = updatedAny.(Model)
	if retryCmd == nil {
		t.Fatal("confirmed Save As did not retry asynchronously")
	}
	secondFailure, ok := retryCmd().(savePreconditionFailedMsg)
	if !ok {
		t.Fatal("confirmed Save As ignored a destination change after the prompt")
	}
	updatedAny, _ = model.Update(secondFailure)
	model = updatedAny.(Model)
	if model.unsavedConfirm == nil {
		t.Fatal("second destination version did not require a renewed confirmation")
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "changed after prompt\n" {
		t.Fatalf("CAS retry overwrote the second destination version: %q, %v", data, err)
	}

	updatedAny, retryCmd = model.Update(saveAsDestinationResolutionMsg{
		RequestID: requestID,
		Path:      destination,
		Overwrite: true,
	})
	model = updatedAny.(Model)
	saved := requireFileSavedMsg(t, retryCmd)
	updatedAny, _ = model.Update(saved)
	model = updatedAny.(Model)

	if got := model.editors[0].Buffer.FilePath; got != destination {
		t.Fatalf("buffer path after confirmed Save As = %q, want %q", got, destination)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "local save as\n" {
		t.Fatalf("confirmed Save As destination = %q, %v", data, err)
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "source\n" {
		t.Fatalf("Save As changed original file: %q, %v", data, err)
	}
}

func TestSaveAsExistingDestinationCancelPreservesBothFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "source.txt", "source\n", "local edits\n")
	oldPath := model.editors[0].Buffer.FilePath
	destination := filepath.Join(model.rootDir, "existing.txt")
	if err := os.WriteFile(destination, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := model.beginSaveAsForTab(0, destination)
	requestID := pendingRequestIDForPath(model, destination)
	failure, ok := cmd().(savePreconditionFailedMsg)
	if !ok {
		t.Fatal("Save As did not stop at the existing destination")
	}
	updatedAny, _ := model.Update(failure)
	model = updatedAny.(Model)
	updatedAny, cmd = model.Update(saveAsDestinationResolutionMsg{
		RequestID: requestID,
		Path:      destination,
		Overwrite: false,
	})
	model = updatedAny.(Model)

	if cmd != nil {
		t.Fatal("cancelled Save As scheduled another write")
	}
	if len(model.pendingSaves) != 0 {
		t.Fatal("cancelled Save As retained a pending request")
	}
	if got := model.editors[0].Buffer.FilePath; got != oldPath {
		t.Fatalf("cancelled Save As changed buffer path to %q", got)
	}
	if !model.editors[0].Buffer.Dirty() {
		t.Fatal("cancelled Save As marked local edits clean")
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "existing\n" {
		t.Fatalf("cancelled Save As changed destination: %q, %v", data, err)
	}
}

func TestSaveAsExistingDestinationEscapeCancelsPendingRequest(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "source.txt", "source\n", "local edits\n")
	oldPath := model.editors[0].Buffer.FilePath
	destination := filepath.Join(model.rootDir, "existing.txt")
	if err := os.WriteFile(destination, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := model.beginSaveAsForTab(0, destination)
	failure, ok := cmd().(savePreconditionFailedMsg)
	if !ok {
		t.Fatal("Save As did not stop at the existing destination")
	}
	updatedAny, _ := model.Update(failure)
	model = updatedAny.(Model)
	if model.unsavedConfirm == nil {
		t.Fatal("existing destination did not open a confirmation")
	}
	updatedAny, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updatedAny.(Model)

	if len(model.pendingSaves) != 0 {
		t.Fatal("Escape left Save As permanently pending")
	}
	if got := model.editors[0].Buffer.FilePath; got != oldPath {
		t.Fatalf("Escape changed buffer path to %q", got)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "existing\n" {
		t.Fatalf("Escape changed Save As destination: %q, %v", data, err)
	}

	regularSave := model.beginSaveForTab(0, false, false)
	saved := requireFileSavedMsg(t, regularSave)
	updatedAny, _ = model.Update(saved)
	model = updatedAny.(Model)
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "local edits\n" {
		t.Fatalf("regular Save after Escape wrote %q, %v", data, err)
	}
}

func TestSaveAsDestinationAlreadyOpenInAnotherTabIsBlocked(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	sourceIndex := addDirtyEditor(t, &model, "source.txt", "source\n", "local source\n")
	destinationIndex := addDirtyEditor(t, &model, "destination.txt", "destination\n", "destination\n")
	sourcePath := model.editors[sourceIndex].Buffer.FilePath
	destinationPath := model.editors[destinationIndex].Buffer.FilePath
	sourceRope := model.editors[sourceIndex].Buffer.Rope()
	destinationRope := model.editors[destinationIndex].Buffer.Rope()

	if cmd := model.beginSaveAsForTab(sourceIndex, destinationPath); cmd != nil {
		t.Fatal("Save As started despite the destination being open in another tab")
	}
	if len(model.pendingSaves) != 0 {
		t.Fatal("blocked Save As retained a pending request")
	}
	if model.editors[sourceIndex].Buffer.FilePath != sourcePath ||
		model.editors[destinationIndex].Buffer.FilePath != destinationPath {
		t.Fatal("blocked Save As changed an editor identity")
	}
	if model.editors[sourceIndex].Buffer.Rope() != sourceRope ||
		model.editors[destinationIndex].Buffer.Rope() != destinationRope {
		t.Fatal("blocked Save As changed an open buffer")
	}
	if data, err := os.ReadFile(destinationPath); err != nil || string(data) != "destination\n" {
		t.Fatalf("blocked Save As changed destination disk bytes: %q, %v", data, err)
	}
	if !strings.Contains(model.status, "already open") {
		t.Fatalf("blocked Save As status = %q, want an already-open explanation", model.status)
	}
}

func TestSaveAsRelativeDestinationMatchesOpenAbsoluteTab(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	root := t.TempDir()
	t.Chdir(root)

	model := newSaveFlowModel(t, cfg, root)
	sourceIndex := addDirtyEditor(t, &model, "source.txt", "source\n", "local source\n")
	destinationIndex := addDirtyEditor(t, &model, "destination.txt", "destination\n", "destination\n")
	destinationPath := model.editors[destinationIndex].Buffer.FilePath

	if cmd := model.beginSaveAsForTab(sourceIndex, "destination.txt"); cmd != nil {
		t.Fatal("relative Save As alias started despite its absolute destination being open")
	}
	if len(model.pendingSaves) != 0 {
		t.Fatal("relative Save As alias retained a pending request")
	}
	if data, err := os.ReadFile(destinationPath); err != nil || string(data) != "destination\n" {
		t.Fatalf("relative Save As alias changed destination bytes: %q, %v", data, err)
	}
	if !strings.Contains(model.status, "already open") {
		t.Fatalf("relative Save As alias status = %q, want an already-open explanation", model.status)
	}
}

func TestSaveAsDestinationReservedByAnotherEditorIsBlocked(t *testing.T) {
	tests := []struct {
		name       string
		firstQueue bool
	}{
		{name: "in flight"},
		{name: "queued behind regular save", firstQueue: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Editor.FormatOnSave = false
			model := newSaveFlowModel(t, cfg, t.TempDir())
			firstIndex := addDirtyEditor(t, &model, "first.txt", "first\n", "first local\n")
			secondIndex := addDirtyEditor(t, &model, "second.txt", "second\n", "second local\n")
			destination := filepath.Join(model.rootDir, "shared.txt")

			if tt.firstQueue {
				if cmd := model.beginSaveForTab(firstIndex, false, false); cmd == nil {
					t.Fatal("first editor regular save did not start")
				}
				model.editors[firstIndex].Buffer.InsertAtCursor([]byte("newer"))
				if cmd := model.beginSaveAsForTab(firstIndex, destination); cmd != nil {
					t.Fatal("first editor Save As did not queue")
				}
			} else if cmd := model.beginSaveAsForTab(firstIndex, destination); cmd == nil {
				t.Fatal("first editor Save As did not start")
			}
			pendingBefore := len(model.pendingSaves)

			if cmd := model.beginSaveAsForTab(secondIndex, destination); cmd != nil {
				t.Fatal("second editor started Save As to a destination reserved by the first")
			}
			if got := len(model.pendingSaves); got != pendingBefore {
				t.Fatalf("blocked second Save As changed pending request count: got %d, want %d", got, pendingBefore)
			}
			if !strings.Contains(model.status, "another save") {
				t.Fatalf("blocked second Save As status = %q, want reservation explanation", model.status)
			}
		})
	}
}

func TestPendingAndQueuedSaveAsDestinationIsReservedFromOpen(t *testing.T) {
	tests := []struct {
		name  string
		queue bool
	}{
		{name: "in flight"},
		{name: "queued behind regular save", queue: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Editor.FormatOnSave = false
			model := newSaveFlowModel(t, cfg, t.TempDir())
			addDirtyEditor(t, &model, "source.txt", "source\n", "local source\n")
			destination := filepath.Join(model.rootDir, "reserved.txt")

			if tt.queue {
				if cmd := model.beginSaveForTab(0, false, false); cmd == nil {
					t.Fatal("regular save did not start")
				}
				model.editors[0].Buffer.InsertAtCursor([]byte("newer"))
				if cmd := model.beginSaveAsForTab(0, destination); cmd != nil {
					t.Fatal("Save As did not queue behind regular save")
				}
			} else if cmd := model.beginSaveAsForTab(0, destination); cmd == nil {
				t.Fatal("Save As did not start")
			}

			openedAny, openCmd := model.openFilePinned(filepath.Base(destination))
			opened := openedAny.(Model)
			if openCmd != nil {
				t.Fatal("reserved Save As destination scheduled an open")
			}
			if len(opened.editors) != 1 || len(opened.tabBar.Tabs) != 1 {
				t.Fatalf("reserved destination created %d editors / %d tabs", len(opened.editors), len(opened.tabBar.Tabs))
			}
			if opened.editors[0].Buffer.FilePath == destination {
				t.Fatal("reserved destination replaced the source editor")
			}
			if !strings.Contains(opened.status, "Save As") {
				t.Fatalf("reserved destination status = %q, want Save As explanation", opened.status)
			}
		})
	}
}

func TestSaveAsFailureKeepsOriginalPathAndDirtyState(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "before.txt", "before\n", "after\n")

	oldPath := model.editors[0].Buffer.FilePath
	newPath := filepath.Join(model.rootDir, "after.txt")
	model.saveAsMode = true
	model.saveAsInput = newPath
	updatedAny, _ := model.handleSaveAsInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := updatedAny.(Model)
	requestID := pendingRequestIDForPath(updated, newPath)
	if requestID == 0 {
		t.Fatal("expected pending Save As request")
	}

	failedAny, _ := updated.Update(FileErrorMsg{Path: newPath, RequestID: requestID, Err: errors.New("disk full")})
	failed := failedAny.(Model)
	if got := failed.editors[0].Buffer.FilePath; got != oldPath {
		t.Fatalf("buffer path after failed Save As = %q, want %q", got, oldPath)
	}
	if !failed.editors[0].Buffer.Dirty() {
		t.Fatal("failed Save As must leave the buffer dirty")
	}
	if got := failed.tabBar.Tabs[0].FilePath; got != oldPath {
		t.Fatalf("tab path after failed Save As = %q, want %q", got, oldPath)
	}
}

func TestSaveAsFormatsSnapshotBeforeWritingNewPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = true
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "before.txt", "before\n", "fmt.Println( 1 )\n")

	oldPath := model.editors[0].Buffer.FilePath
	newPath := filepath.Join(model.rootDir, "after.go")
	cmd := model.beginSaveAsForTab(0, newPath)
	if cmd == nil {
		t.Fatal("Save As with format-on-save must start formatting asynchronously")
	}
	requestID := pendingRequestIDForPath(model, newPath)
	if requestID == 0 {
		t.Fatal("expected pending Save As request")
	}
	req := model.pendingSaves[requestID]

	formattedAny, saveCmd := model.Update(lsp.FormatResultMsg{
		RequestID:      requestID,
		FilePath:       newPath,
		BaseVersion:    req.SnapshotVersion,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			EndLine:   0,
			EndCol:    len("fmt.Println( 1 )"),
			NewText:   "fmt.Println(1)",
		}},
	})
	formatted := formattedAny.(Model)
	if got := formatted.editors[0].Buffer.FilePath; got != oldPath {
		t.Fatalf("format must not redirect buffer before Save As writes: got %q, want %q", got, oldPath)
	}

	saved := requireFileSavedMsg(t, saveCmd)
	finalAny, _ := formatted.Update(saved)
	final := finalAny.(Model)
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read formatted Save As destination: %v", err)
	}
	if got, want := string(data), "fmt.Println(1)\n"; got != want {
		t.Fatalf("formatted Save As data = %q, want %q", got, want)
	}
	if got := final.editors[0].Buffer.FilePath; got != newPath {
		t.Fatalf("buffer path = %q, want %q", got, newPath)
	}
	if final.editors[0].Buffer.Dirty() {
		t.Fatal("formatted Save As should be clean after write")
	}
}

func TestSaveAsQueuesLaterEditsForItsNewDestination(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "before.txt", "before\n", "first\n")
	newPath := filepath.Join(model.rootDir, "after.txt")

	firstCmd := model.beginSaveAsForTab(0, newPath)
	if firstCmd == nil {
		t.Fatal("expected initial Save As command")
	}
	model.editors[0].Buffer.SelectAll()
	model.editors[0].Buffer.InsertAtCursor([]byte("second\n"))
	if cmd := model.beginSaveForTab(0, false, false); cmd != nil {
		t.Fatal("later save must queue behind the in-flight Save As")
	}

	firstSaved := requireFileSavedMsg(t, firstCmd)
	afterFirstAny, queuedCmd := model.Update(firstSaved)
	afterFirst := afterFirstAny.(Model)
	if queuedCmd == nil {
		t.Fatal("Save As completion did not start the queued newer snapshot")
	}
	if got := afterFirst.editors[0].Buffer.FilePath; got != newPath {
		t.Fatalf("buffer path after initial Save As = %q, want %q", got, newPath)
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "first\n"; got != want {
		t.Fatalf("initial Save As data = %q, want %q", got, want)
	}

	secondSaved := requireFileSavedMsg(t, queuedCmd)
	finalAny, _ := afterFirst.Update(secondSaved)
	final := finalAny.(Model)
	data, err = os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "second\n"; got != want {
		t.Fatalf("queued Save As data = %q, want %q", got, want)
	}
	if final.editors[0].Buffer.Dirty() {
		t.Fatal("latest queued Save As snapshot should be clean")
	}
}

func TestSaveAsQueuedBehindRegularSaveChecksItsOwnDestination(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = false
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "before.txt", "before\n", "first\n")
	oldPath := model.editors[0].Buffer.FilePath
	newPath := filepath.Join(model.rootDir, "after.txt")

	firstCmd := model.beginSaveForTab(0, false, false)
	model.editors[0].Buffer.SelectAll()
	model.editors[0].Buffer.InsertAtCursor([]byte("second\n"))
	if cmd := model.beginSaveAsForTab(0, newPath); cmd != nil {
		t.Fatal("Save As should queue behind the in-flight regular save")
	}

	firstSaved := requireFileSavedMsg(t, firstCmd)
	updatedAny, queuedCmd := model.Update(firstSaved)
	model = updatedAny.(Model)
	if queuedCmd == nil {
		t.Fatal("regular save completion did not start queued Save As")
	}
	queuedSaved := requireFileSavedMsg(t, queuedCmd)
	updatedAny, _ = model.Update(queuedSaved)
	model = updatedAny.(Model)

	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "first\n" {
		t.Fatalf("regular save result = %q, %v", data, err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "second\n" {
		t.Fatalf("queued Save As result = %q, %v", data, err)
	}
	if got := model.editors[0].Buffer.FilePath; got != newPath {
		t.Fatalf("queued Save As buffer path = %q, want %q", got, newPath)
	}
	if model.editors[0].Buffer.Dirty() {
		t.Fatal("queued Save As latest snapshot should be clean")
	}
}

func TestSaveAndCloseClosesOnlyAfterSaveSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "before\n", "after\n")

	updatedAny, cmd := model.Update(SaveAndCloseTabMsg{Index: 0})
	updated := updatedAny.(Model)

	if len(updated.editors) != 1 {
		t.Fatalf("editor count before save = %d, want 1", len(updated.editors))
	}

	savedMsg := requireFileSavedMsg(t, cmd)
	finalAny, _ := updated.Update(savedMsg)
	final := finalAny.(Model)

	if len(final.editors) != 0 {
		t.Fatalf("editor count after save = %d, want 0", len(final.editors))
	}
	if final.welcome == nil {
		t.Fatal("expected welcome screen after closing last tab")
	}
}

func TestSaveAllAndQuitTargetsSavedPathsAndQuitsAfterLastSave(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())

	addDirtyEditor(t, &model, "one.go", "one-before\n", "one-after\n")
	addDirtyEditor(t, &model, "two.go", "two-before\n", "two-after\n")
	model.activeTab = 0
	model.tabBar.ActiveIdx = 0

	updatedAny, cmd := model.Update(SaveAllAndQuitMsg{})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("expected save-all command")
	}
	if len(updated.pendingSaves) != 2 {
		t.Fatalf("pendingSaves = %d, want 2", len(updated.pendingSaves))
	}

	pathOne := updated.editors[0].Buffer.FilePath
	pathTwo := updated.editors[1].Buffer.FilePath
	requestOne := pendingRequestIDForPath(updated, pathOne)
	requestTwo := pendingRequestIDForPath(updated, pathTwo)
	if requestOne == 0 || requestTwo == 0 {
		t.Fatalf("missing pending save requests: %d %d", requestOne, requestTwo)
	}

	firstAny, firstCmd := updated.Update(FileSavedMsg{Path: pathTwo, RequestID: requestTwo})
	first := firstAny.(Model)
	if first.tabBar.Tabs[0].Dirty != updated.tabBar.Tabs[0].Dirty {
		t.Fatal("saving a non-active path should not mutate the active tab dirty state")
	}
	if first.tabBar.Tabs[1].Dirty {
		t.Fatal("saved path should be marked clean")
	}
	if firstCmd != nil {
		if msg := firstCmd(); msg != nil {
			if _, ok := msg.(QuitWithoutSavingMsg); ok {
				t.Fatal("quit triggered before final save completed")
			}
		}
	}

	secondAny, secondCmd := first.Update(FileSavedMsg{Path: pathOne, RequestID: requestOne})
	second := secondAny.(Model)
	if second.hasPendingQuitAfterSaves() {
		t.Fatal("quit follow-up should be cleared after the final save")
	}
	if secondCmd == nil {
		t.Fatal("expected quit command after final save")
	}
	msg := secondCmd()
	if _, ok := msg.(QuitWithoutSavingMsg); !ok {
		t.Fatalf("final save follow-up = %T, want QuitWithoutSavingMsg", msg)
	}
}

func TestSaveSnapshotKeepsEditsMadeBeforeWriteDirty(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "first\n")
	path := model.editors[0].Buffer.FilePath

	cmd := model.beginSaveForTab(0, false, false)
	if cmd == nil {
		t.Fatal("expected save command")
	}

	// The command must persist the snapshot captured by beginSaveForTab, not
	// whatever happens to be in the mutable buffer when the command runs.
	model.editors[0].Buffer.SelectAll()
	model.editors[0].Buffer.InsertAtCursor([]byte("second\n"))

	saved := requireFileSavedMsg(t, cmd)
	updatedAny, _ := model.Update(saved)
	updated := updatedAny.(Model)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "first\n"; got != want {
		t.Fatalf("disk content = %q, want captured snapshot %q", got, want)
	}
	if got := updated.editors[0].Buffer.Content(); got != "second\n" {
		t.Fatalf("buffer content = %q, want later edit", got)
	}
	if !updated.editors[0].Buffer.Dirty() || !updated.tabBar.Tabs[0].Dirty {
		t.Fatal("later edit must remain dirty after an older snapshot saves")
	}
}

func TestSecondSaveWaitsForFirstAndPersistsLatestSnapshot(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "first\n")
	path := model.editors[0].Buffer.FilePath

	firstCmd := model.beginSaveForTab(0, false, false)
	if firstCmd == nil {
		t.Fatal("expected first save command")
	}

	model.editors[0].Buffer.SelectAll()
	model.editors[0].Buffer.InsertAtCursor([]byte("second\n"))
	secondCmd := model.beginSaveForTab(0, false, false)
	if secondCmd != nil {
		t.Fatal("second save started concurrently instead of waiting for the first")
	}

	firstSaved := requireFileSavedMsg(t, firstCmd)
	afterFirstAny, queuedCmd := model.Update(firstSaved)
	afterFirst := afterFirstAny.(Model)
	if queuedCmd == nil {
		t.Fatal("first completion did not start the queued latest snapshot")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "first\n"; got != want {
		t.Fatalf("disk after first save = %q, want %q", got, want)
	}

	secondSaved := requireFileSavedMsg(t, queuedCmd)
	finalAny, _ := afterFirst.Update(secondSaved)
	final := finalAny.(Model)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "second\n"; got != want {
		t.Fatalf("disk after queued save = %q, want %q", got, want)
	}
	if final.editors[0].Buffer.Dirty() || final.tabBar.Tabs[0].Dirty {
		t.Fatal("latest queued snapshot should be clean after it saves")
	}
}

func TestSaveAndCloseKeepsTabOpenWhenSnapshotIsObsolete(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "first\n")

	updatedAny, cmd := model.Update(SaveAndCloseTabMsg{Index: 0})
	updated := updatedAny.(Model)
	updated.editors[0].Buffer.SelectAll()
	updated.editors[0].Buffer.InsertAtCursor([]byte("second\n"))

	saved := requireFileSavedMsg(t, cmd)
	finalAny, _ := updated.Update(saved)
	final := finalAny.(Model)

	if len(final.editors) != 1 {
		t.Fatalf("obsolete save closed tab: editors = %d, want 1", len(final.editors))
	}
	if !final.editors[0].Buffer.Dirty() {
		t.Fatal("later edit must remain dirty")
	}
}

func TestFormatOnSaveDiscardsResultForObsoleteSnapshot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = true
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "disk\n", "first\n")
	path := model.editors[0].Buffer.FilePath

	_ = model.beginSaveForTab(0, false, false)
	requestID := pendingRequestIDForPath(model, path)
	req := model.pendingSaves[requestID]
	if requestID == 0 {
		t.Fatal("expected pending formatting request")
	}

	model.editors[0].Buffer.SelectAll()
	model.editors[0].Buffer.InsertAtCursor([]byte("second\n"))

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		RequestID:      requestID,
		FilePath:       path,
		BaseVersion:    req.SnapshotVersion,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    len("first"),
			NewText:   "formatted",
		}},
	})
	updated := updatedAny.(Model)

	if cmd != nil {
		t.Fatal("obsolete formatting result must not start a write")
	}
	if got, want := updated.editors[0].Buffer.Content(), "second\n"; got != want {
		t.Fatalf("obsolete formatting changed buffer: got %q, want %q", got, want)
	}
	if !updated.editors[0].Buffer.Dirty() {
		t.Fatal("buffer must remain dirty after discarding stale format result")
	}
}

func TestSaveAllAndQuitCancelsQuitWhenASnapshotIsObsolete(t *testing.T) {
	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "one.go", "one-disk\n", "one-first\n")
	addDirtyEditor(t, &model, "two.go", "two-disk\n", "two-first\n")

	updatedAny, _ := model.Update(SaveAllAndQuitMsg{})
	updated := updatedAny.(Model)
	pathOne := updated.editors[0].Buffer.FilePath
	pathTwo := updated.editors[1].Buffer.FilePath
	requestOne := pendingRequestIDForPath(updated, pathOne)
	requestTwo := pendingRequestIDForPath(updated, pathTwo)

	updated.editors[0].Buffer.SelectAll()
	updated.editors[0].Buffer.InsertAtCursor([]byte("one-second\n"))

	finalAny, _ := updated.Update(FileSavedMsg{Path: pathOne, RequestID: requestOne})
	final := finalAny.(Model)
	if final.pendingSaves[requestTwo].QuitAfter {
		t.Fatal("a stale save snapshot must cancel the deferred quit")
	}
	if !final.editors[0].Buffer.Dirty() {
		t.Fatal("edited buffer must remain dirty")
	}
}
