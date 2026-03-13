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

func requireFileSavedMsg(t *testing.T, cmd tea.Cmd) FileSavedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	savedMsg, ok := msg.(FileSavedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want FileSavedMsg", msg)
	}
	return savedMsg
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
