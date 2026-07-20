package app

import (
	"strings"
	"testing"

	"teak/internal/config"
	"teak/internal/search"
	"teak/internal/text"
)

func TestSearchReplaceUpdatesTabStateAndPinsPreview(t *testing.T) {
	tests := []struct {
		name string
		msg  any
		want string
	}{
		{
			name: "replace one",
			msg:  search.ReplaceOneMsg{Query: "old", Replacement: "new"},
			want: "new old\n",
		},
		{
			name: "replace all",
			msg:  search.ReplaceAllMsg{Query: "old", Replacement: "new"},
			want: "new new\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
			idx := addDirtyEditor(t, &model, "replace.txt", "old old\n", "old old\n")
			model.tabBar.Tabs[idx].Preview = true

			updatedAny, cmd := model.Update(tt.msg)
			pending := updatedAny.(Model)
			if cmd == nil {
				t.Fatal("replace did not schedule background preparation")
			}
			if got := pending.editors[idx].Buffer.Content(); got != "old old\n" {
				t.Fatalf("Update mutated buffer before command completion: %q", got)
			}

			result := cmd()
			completedAny, postCmd := pending.Update(result)
			updated := completedAny.(Model)

			if got := updated.editors[idx].Buffer.Content(); got != tt.want {
				t.Fatalf("buffer content = %q, want %q", got, tt.want)
			}
			if !updated.editors[idx].Buffer.Dirty() {
				t.Fatal("replace did not mark the buffer dirty")
			}
			if !updated.tabBar.Tabs[idx].Dirty {
				t.Fatal("replace did not synchronize the tab dirty indicator")
			}
			if updated.tabBar.Tabs[idx].Preview {
				t.Fatal("replace did not pin the modified preview tab")
			}
			if postCmd == nil {
				t.Fatal("replace did not schedule post-edit work")
			}
		})
	}
}

func TestSearchReplaceDiscardsStalePreparation(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "replace.txt", "old old\n", "old old\n")

	pendingAny, cmd := model.Update(search.ReplaceOneMsg{Query: "old", Replacement: "new"})
	pending := pendingAny.(Model)
	if cmd == nil {
		t.Fatal("replace did not schedule background preparation")
	}
	pending.editors[idx].Buffer.InsertAtCursor([]byte("user "))
	current := pending.editors[idx].Buffer.Rope()

	completedAny, postCmd := pending.Update(cmd())
	completed := completedAny.(Model)
	if completed.editors[idx].Buffer.Rope() != current {
		t.Fatal("stale replace result overwrote a newer edit")
	}
	if postCmd != nil {
		t.Fatal("stale replace result scheduled post-edit work")
	}
}

func TestSearchReplaceLatestRequestWins(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "replace.txt", "old old\n", "old old\n")

	firstAny, firstCmd := model.Update(search.ReplaceOneMsg{Query: "old", Replacement: "first"})
	first := firstAny.(Model)
	secondAny, secondCmd := first.Update(search.ReplaceOneMsg{Query: "old", Replacement: "second"})
	second := secondAny.(Model)
	if firstCmd == nil || secondCmd == nil {
		t.Fatal("replace requests did not schedule background preparation")
	}

	afterFirstAny, firstPost := second.Update(firstCmd())
	afterFirst := afterFirstAny.(Model)
	if firstPost != nil || afterFirst.editors[idx].Buffer.Content() != "old old\n" {
		t.Fatal("superseded replace request mutated the buffer")
	}

	completedAny, postCmd := afterFirst.Update(secondCmd())
	completed := completedAny.(Model)
	if got, want := completed.editors[idx].Buffer.Content(), "second old\n"; got != want {
		t.Fatalf("latest replace content = %q, want %q", got, want)
	}
	if postCmd == nil {
		t.Fatal("latest replace did not schedule post-edit work")
	}
}

func TestReplaceAllHasDeterministicSafetyBounds(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "generated.txt", strings.Repeat("x", maxReplaceAllBytes+1), "")

	updatedAny, cmd := model.Update(search.ReplaceAllMsg{Query: "x", Replacement: "y"})
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatal("oversized Replace All must not schedule expensive follow-up work")
	}
	if got := updated.editors[idx].Buffer.Content(); got != strings.Repeat("x", maxReplaceAllBytes+1) {
		t.Fatal("oversized Replace All modified the buffer")
	}
	if !strings.Contains(updated.status, "limited") {
		t.Fatalf("status = %q, want limit explanation", updated.status)
	}

	content := strings.Repeat("x", maxReplaceAllMatches+1)
	if _, matches, ok := boundedReplaceAll(content, "x", "y"); ok || matches != maxReplaceAllMatches+1 {
		t.Fatalf("boundedReplaceAll = matches %d ok %t, want %d false", matches, ok, maxReplaceAllMatches+1)
	}
}

func TestPrepareReplaceAllMapsCursorThroughInsertedLines(t *testing.T) {
	source := text.NewFromString("old\nkeep old tail\n")
	preparation := replacePreparation{
		Source:      source,
		Cursor:      text.Position{Line: 1, Col: 8},
		Query:       "old",
		Replacement: "X\nY",
		All:         true,
	}

	msg, ok := prepareReplaceCmd(t.Context(), preparation)().(replacePreparedMsg)
	if !ok {
		t.Fatal("prepareReplaceCmd returned an unexpected message")
	}
	if msg.Err != nil {
		t.Fatalf("prepareReplaceCmd error: %v", msg.Err)
	}
	if got, want := msg.Result.String(), "X\nY\nkeep X\nY tail\n"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
	if got, want := msg.Cursor, (text.Position{Line: 3, Col: 1}); got != want {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

func TestFindReplaceableTabSkipsDirtyPreview(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "preview.txt", "before\n", "after\n")
	model.tabBar.Tabs[idx].Preview = true
	model.tabBar.Tabs[idx].Dirty = true

	if got := model.findReplaceableTab(); got != -1 {
		t.Fatalf("findReplaceableTab() = %d, want -1 for dirty preview", got)
	}
}
