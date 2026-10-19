package app

import (
	"strings"
	"testing"

	"teak/internal/config"
	"teak/internal/text"
)

func TestProjectReplaceStatusCountsSuccessesAroundClosedFailure(t *testing.T) {
	m := newTestModel(t)
	m.replaces.generation = 1

	updatedAny, _ := m.handleProjectReplacePrepared(projectReplacePreparedMsg{
		Generation: 1,
		Closed: []projectReplaceClosedResult{
			{Path: "/w/a.go", Matches: 2},
			{Path: "/w/blob.dat", Err: text.ErrBinaryFile},
			{Path: "/w/b.go", Matches: 1},
		},
	})
	status := updatedAny.(Model).status
	if !strings.Contains(status, "Replaced 3 matches in 2 files") {
		t.Fatalf("status = %q, want the successful replacements counted despite one closed-file failure", status)
	}
	if !strings.Contains(status, "blob.dat") {
		t.Fatalf("status = %q, want the failing file named", status)
	}
}

func TestProjectReplaceStaleOpenBufferKeepsSyncCommandsForAppliedBuffers(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idxA := addDirtyEditor(t, &model, "a.txt", "old\n", "old\n")
	idxB := addDirtyEditor(t, &model, "b.txt", "old\n", "old\n")
	edA, edB := model.editors[idxA], model.editors[idxB]
	versionA := edA.Buffer.Version()
	versionB := edB.Buffer.Version()
	model.replaces.generation = 1

	updatedAny, cmd := model.handleProjectReplacePrepared(projectReplacePreparedMsg{
		Generation: 1,
		Open: []projectReplaceOpenResult{
			{
				Target:  projectReplaceTarget{Path: edA.Buffer.FilePath, Open: true, EditorID: edA.ID(), Version: versionA, Source: edA.Buffer.Rope()},
				Result:  text.NewFromString("new\n"),
				Matches: 1,
			},
			{
				// Stale: the buffer changed after the replacement was prepared.
				Target:  projectReplaceTarget{Path: edB.Buffer.FilePath, Open: true, EditorID: edB.ID(), Version: versionB - 1, Source: edB.Buffer.Rope()},
				Result:  text.NewFromString("new\n"),
				Matches: 1,
			},
		},
	})
	updated := updatedAny.(Model)
	if got := updated.editors[idxB].Buffer.Content(); got != "old\n" {
		t.Fatalf("stale buffer was replaced: %q", got)
	}
	if got := updated.editors[idxA].Buffer.Content(); got == "old\n" {
		t.Fatalf("fresh buffer was not replaced")
	}
	if got := updated.editors[idxA].Buffer.Version(); got == versionA {
		t.Fatalf("fresh buffer version did not advance")
	}
	if cmd == nil {
		t.Fatal("buffer mutated by project replace returned no sync commands (LSP/dirty state desync)")
	}
}
