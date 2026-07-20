package app

import (
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
)

func TestWorkspaceEditUpdatesAppMutationState(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "rename.go", "oldName\n", "oldName\n")
	model.tabBar.Tabs[idx].Preview = true
	path := model.editors[idx].Buffer.FilePath

	updated := applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{
		Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {
				{
					StartLine: 0,
					StartCol:  0,
					EndLine:   0,
					EndCol:    len("oldName"),
					NewText:   "newName",
				},
			},
		},
	})

	if got, want := updated.editors[idx].Buffer.Content(), "newName\n"; got != want {
		t.Fatalf("workspace edit content = %q, want %q", got, want)
	}
	if !updated.tabBar.Tabs[idx].Dirty {
		t.Fatal("workspace edit did not synchronize the tab dirty indicator")
	}
	if updated.tabBar.Tabs[idx].Preview {
		t.Fatal("workspace edit did not pin the modified preview")
	}
}
