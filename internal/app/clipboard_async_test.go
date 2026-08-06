package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/text"
)

func TestClipboardPasteRoutesToOwningEditorAfterTabSwitch(t *testing.T) {
	model := newInputRoutingTestModel(t)
	editorA := newTokenizeRoutingEditor(model, "a.go", "alpha")
	editorB := newTokenizeRoutingEditor(model, "b.go", "beta")
	ownerBefore := editorA.Buffer.Content()
	nonOwnerBefore := editorB.Buffer.Content()
	var pasteCmd tea.Cmd
	editorA, pasteCmd = editorA.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if pasteCmd == nil {
		t.Fatal("Ctrl+V did not schedule an asynchronous clipboard read")
	}
	model = installTokenizeRoutingEditors(model, editorA, editorB, 1)

	updatedAny, _ := model.Update(editor.ClipboardPasteResultMsg{
		EditorID: editorA.ID(), Generation: 1, Content: "paste ",
	})
	updated := updatedAny.(Model)
	if got := updated.editors[0].Buffer.Content(); got != "paste "+ownerBefore {
		t.Fatalf("owner content = %q, want clipboard text in inactive owner", got)
	}
	if got := updated.editors[1].Buffer.Content(); got != nonOwnerBefore {
		t.Fatalf("active non-owner changed to %q", got)
	}
}

func TestLargeClipboardPastePreparationRoutesAndCommits(t *testing.T) {
	model := newInputRoutingTestModel(t)
	ed, request := model.activeEditor().Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if request == nil {
		t.Fatal("Ctrl+V did not start a clipboard request")
	}
	model.setEditor(model.activeTab, ed)

	content := strings.Repeat("p", 64<<10+1)
	queuedAny, prepare := model.Update(editor.ClipboardPasteResultMsg{
		EditorID: ed.ID(), Generation: 1, Content: content,
	})
	queued := queuedAny.(Model)
	if prepare == nil {
		t.Fatal("large clipboard result did not retain async preparation")
	}
	if got := queued.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("large clipboard result changed buffer before preparation: %q", got)
	}

	preparedAny, followup := queued.Update(prepare())
	prepared := preparedAny.(Model)
	_ = followup // a test editor may not have syntax work to schedule.
	if got := prepared.activeEditor().Buffer.Rope().Len(); got != len(content) {
		t.Fatalf("prepared paste length = %d, want %d", got, len(content))
	}
}

func TestClipboardPasteForClosedEditorIsDiscarded(t *testing.T) {
	model := newInputRoutingTestModel(t)
	before := model.activeEditor().Buffer.Content()
	updatedAny, cmd := model.Update(editor.ClipboardPasteResultMsg{
		EditorID: model.activeEditor().ID() + 100, Generation: 1, Content: "stale",
	})
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatal("closed-editor clipboard result scheduled work")
	}
	if got := updated.activeEditor().Buffer.Content(); got != before {
		t.Fatalf("closed-editor clipboard result changed active buffer to %q", got)
	}
}

func TestClipboardCopyFailureKeepsClearStatus(t *testing.T) {
	model := newInputRoutingTestModel(t)
	updatedAny, _ := model.Update(editor.ClipboardCopyResultMsg{
		EditorID:       model.activeEditor().ID(),
		FallbackStored: true,
		Err:            errors.New("backend timed out"),
	})
	updated := updatedAny.(Model)
	if got := updated.status; got == "" {
		t.Fatal("copy fallback failure did not report status")
	}
}

func TestLargeEditLimitsSurfaceStatus(t *testing.T) {
	model := newInputRoutingTestModel(t)
	updatedAny, _ := model.Update(editor.ClipboardOperationLimitMsg{
		EditorID: model.activeEditor().ID(), Operation: "Copy", MaxBytes: 16 << 20,
	})
	updated := updatedAny.(Model)
	if updated.status == "" {
		t.Fatal("clipboard limit did not surface a status")
	}

	updatedAny, _ = updated.Update(editor.MultilineEditLimitMsg{
		EditorID: updated.activeEditor().ID(), Operation: "Indent", MaxLines: 128,
	})
	updated = updatedAny.(Model)
	if got := updated.status; got == "" {
		t.Fatal("multiline limit did not surface a status")
	}

	updatedAny, _ = updated.Update(editor.StructuralEditLimitMsg{
		EditorID: updated.activeEditor().ID(), Operation: "Toggle comment", MaxBytes: 64 << 10,
	})
	if got := updatedAny.(Model).status; !strings.Contains(got, "64 KiB") {
		t.Fatalf("structural prefix limit status = %q, want 64 KiB", got)
	}
}

func TestClipboardPasteIsDiscardedAfterExternalReload(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.FilePath = "document.txt"
	model.tabBar.Tabs[model.activeTab].FilePath = "document.txt"
	ed, pasteCmd := model.activeEditor().Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if pasteCmd == nil {
		t.Fatal("Ctrl+V did not start a clipboard request")
	}
	model.setEditor(model.activeTab, ed)

	reloadedAny, _ := model.reloadExternalFile("document.txt", text.NewFromString("external"))
	reloaded := reloadedAny.(Model)
	updatedAny, _ := reloaded.Update(editor.ClipboardPasteResultMsg{
		EditorID: ed.ID(), Generation: 1, Content: "stale paste",
	})
	updated := updatedAny.(Model)
	if got := updated.activeEditor().Buffer.Content(); got != "external" {
		t.Fatalf("paste result after reload changed buffer to %q", got)
	}
}
