package app

import (
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
)

func TestManualFormatUpdatesAppMutationState(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "format.go", "before\n", "before\n")
	model.tabBar.Tabs[idx].Preview = true
	path := model.editors[idx].Buffer.FilePath
	baseVersion := model.editors[idx].Buffer.Version()

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       path,
		BaseVersion:    baseVersion,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    len("before"),
			NewText:   "after",
		}},
	})
	updated := updatedAny.(Model)
	if got, want := updated.editors[idx].Buffer.Content(), "before\n"; got != want {
		t.Fatalf("format mutated content before preparation: got %q, want %q", got, want)
	}
	updated, cmd = completeFormatPreparation(t, updated, cmd)

	if got, want := updated.editors[idx].Buffer.Content(), "after\n"; got != want {
		t.Fatalf("formatted content = %q, want %q", got, want)
	}
	if !updated.tabBar.Tabs[idx].Dirty {
		t.Fatal("manual format did not synchronize the tab dirty indicator")
	}
	if updated.tabBar.Tabs[idx].Preview {
		t.Fatal("manual format did not pin the modified preview")
	}
	if cmd == nil {
		t.Fatal("manual format did not schedule post-edit work")
	}
}

func TestManualFormatRejectsStaleVersion(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "format.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	baseVersion := model.editors[idx].Buffer.Version()

	model.editors[idx].Buffer.SelectAll()
	model.editors[idx].Buffer.InsertAtCursor([]byte("newer\n"))

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       path,
		BaseVersion:    baseVersion,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    len("old"),
			NewText:   "formatted",
		}},
	})
	updated := updatedAny.(Model)

	if got, want := updated.editors[idx].Buffer.Content(), "newer\n"; got != want {
		t.Fatalf("stale format changed buffer: got %q, want %q", got, want)
	}
	if cmd != nil {
		t.Fatal("stale manual format scheduled post-edit work")
	}
}
