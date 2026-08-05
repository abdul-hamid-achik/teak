package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/git"
)

func TestRootRoutesBranchFilterResultToVisiblePicker(t *testing.T) {
	m := newViewTestModel(t, false)
	m.showBranchPicker = true
	m.branchPickerM.SetBranches([]string{"main", "feature/one", "feature/two", "release"}, "main")
	_ = m.branchPickerM.Focus()

	pendingAny, cmd := m.Update(tea.KeyPressMsg{Text: "feature/two"})
	pending := pendingAny.(Model)
	if view := pending.branchPickerM.View(); !strings.Contains(view, "Filtering branches...") {
		t.Fatalf("pending branch picker view = %q, want filtering status", view)
	}

	ready := appBranchFilterMessage(t, cmd)
	appliedAny, followup := pending.Update(ready)
	applied := appliedAny.(Model)
	if followup != nil {
		t.Fatal("ordinary branch filter completion emitted an unexpected command")
	}
	view := applied.branchPickerM.View()
	if strings.Contains(view, "Filtering branches...") || !strings.Contains(view, "feature/two") || strings.Contains(view, "feature/one") || strings.Contains(view, "release") {
		t.Fatalf("applied branch picker view = %q, want only feature/two", view)
	}
}

func TestRootDiscardsBranchListWhilePickerIsClosed(t *testing.T) {
	m := newViewTestModel(t, false)
	m.branchPickerM.SetBranches([]string{"original"}, "original")
	m.showBranchPicker = false

	updatedAny, cmd := m.Update(git.BranchListMsg{Branches: []string{"stale"}, Current: "stale"})
	if cmd != nil {
		t.Fatal("closed-picker branch list emitted an unexpected command")
	}
	updated := updatedAny.(Model)
	updated.showBranchPicker = true
	view := updated.branchPickerM.View()
	if !strings.Contains(view, "original") || strings.Contains(view, "stale") {
		t.Fatalf("closed picker accepted stale branch list: %q", view)
	}
}

func TestRootRejectsSupersededBranchListAfterPickerReopens(t *testing.T) {
	m := newViewTestModel(t, false)
	m.showBranchPicker = true
	m.branchListGeneration = 2
	m.branchPickerM.SetBranches([]string{"current"}, "current")

	staleAny, _ := m.Update(git.BranchListMsg{Generation: 1, Branches: []string{"stale"}, Current: "stale"})
	stale := staleAny.(Model)
	if view := stale.branchPickerM.View(); !strings.Contains(view, "current") || strings.Contains(view, "stale") {
		t.Fatalf("superseded branch list replaced reopened picker: %q", view)
	}

	currentAny, _ := stale.Update(git.BranchListMsg{Generation: 2, Branches: []string{"latest"}, Current: "latest"})
	current := currentAny.(Model)
	if view := current.branchPickerM.View(); !strings.Contains(view, "latest") || strings.Contains(view, "current") {
		t.Fatalf("current branch list was not applied: %q", view)
	}
}

func appBranchFilterMessage(t *testing.T, cmd tea.Cmd) git.BranchFilterReadyMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil root branch picker command")
	}
	switch msg := cmd().(type) {
	case git.BranchFilterReadyMsg:
		return msg
	case tea.BatchMsg:
		for _, child := range msg {
			if child == nil {
				continue
			}
			if ready, ok := child().(git.BranchFilterReadyMsg); ok {
				return ready
			}
		}
	}
	t.Fatal("root branch picker command did not produce BranchFilterReadyMsg")
	return git.BranchFilterReadyMsg{}
}
