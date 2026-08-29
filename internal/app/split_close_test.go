package app

import (
	"path/filepath"
	"testing"

	"teak/internal/config"
)

// newSplitCloseModel builds a model with real tabs so closeTab exercises the
// full tab bookkeeping (tab bar, diff views, plugin events) rather than a
// hand-maintained editors slice.
func newSplitCloseModel(t *testing.T) Model {
	t.Helper()
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	addDirtyEditor(t, &model, "a.go", "package a\n", "package a\n")
	addDirtyEditor(t, &model, "b.go", "package b\n", "package b\n")
	addDirtyEditor(t, &model, "c.go", "package c\n", "package c\n")
	model.width, model.height = 120, 40
	return model
}

// closing a tab below both panes must shift the pane indices down with the
// editors slice; otherwise pane B points past the end and the split silently
// stops rendering.
func TestCloseTabBelowSplitPanesShiftsPaneTabs(t *testing.T) {
	model := newSplitCloseModel(t)
	model.activateTab(1)
	model.toggleSplit()
	if model.split.firstTab != 1 || model.split.secondTab != 2 {
		t.Fatalf("setup: split panes = %d/%d, want 1/2", model.split.firstTab, model.split.secondTab)
	}

	closedAny, _ := model.closeTab(0)
	closed := closedAny.(Model)

	if !closed.split.enabled {
		t.Fatal("closing a tab below both panes collapsed the split")
	}
	if closed.split.firstTab != 0 || closed.split.secondTab != 1 {
		t.Fatalf("split panes = %d/%d, want 0/1 after the lower tab closed", closed.split.firstTab, closed.split.secondTab)
	}
	if got := filepath.Base(closed.editors[closed.split.firstTab].Buffer.FilePath); got != "b.go" {
		t.Errorf("pane A shows %s, want b.go", got)
	}
	if got := filepath.Base(closed.editors[closed.split.secondTab].Buffer.FilePath); got != "c.go" {
		t.Errorf("pane B shows %s, want c.go", got)
	}
}

// closing the tab displayed in a pane must collapse the split like unsplit
// instead of silently reassigning pane contents to neighboring tabs.
func TestClosePaneATabCollapsesSplit(t *testing.T) {
	model := newSplitCloseModel(t)
	model.activateTab(0)
	model.toggleSplit()

	closedAny, _ := model.closeTab(model.split.firstTab)
	closed := closedAny.(Model)

	if closed.split.enabled || closed.split.firstTab != -1 || closed.split.secondTab != -1 {
		t.Fatalf("split state after closing pane A tab = enabled %t panes %d/%d, want collapsed",
			closed.split.enabled, closed.split.firstTab, closed.split.secondTab)
	}
	if len(closed.editors) != 2 {
		t.Fatalf("editor count = %d, want 2", len(closed.editors))
	}
}

func TestClosePaneBTabCollapsesSplit(t *testing.T) {
	model := newSplitCloseModel(t)
	model.activateTab(0)
	model.toggleSplit()

	closedAny, _ := model.closeTab(model.split.secondTab)
	closed := closedAny.(Model)

	if closed.split.enabled || closed.split.firstTab != -1 || closed.split.secondTab != -1 {
		t.Fatalf("split state after closing pane B tab = enabled %t panes %d/%d, want collapsed",
			closed.split.enabled, closed.split.firstTab, closed.split.secondTab)
	}
	if len(closed.editors) != 2 {
		t.Fatalf("editor count = %d, want 2", len(closed.editors))
	}
}

// closing the final tab while split must not leave a stale enabled split
// pointing at editors that no longer exist.
func TestCloseLastTabWhileSplitResetsSplit(t *testing.T) {
	model := newSplitCloseModel(t)
	// Reduce to a single tab while pane references remain set; this is the
	// only state in which closeTab's last-tab branch runs with a split.
	model.editors = model.editors[:1]
	model.tabBar.Tabs = model.tabBar.Tabs[:1]
	model.activateTab(0)
	model.split.enabled = true
	model.split.firstTab = 0
	model.split.secondTab = -1

	closedAny, _ := model.closeTab(model.activeTab)
	closed := closedAny.(Model)

	if closed.split.enabled || closed.split.firstTab != -1 || closed.split.secondTab != -1 {
		t.Fatalf("split state after closing last tab = enabled %t panes %d/%d, want reset",
			closed.split.enabled, closed.split.firstTab, closed.split.secondTab)
	}
	if len(closed.editors) != 0 || closed.welcome == nil {
		t.Fatalf("close state = %d editors, welcome %v", len(closed.editors), closed.welcome)
	}
}

// Regression guard: closing a tab without a split must not invent one.
func TestCloseTabWithoutSplitKeepsSplitDisabled(t *testing.T) {
	model := newSplitCloseModel(t)
	model.activateTab(1)

	closedAny, _ := model.closeTab(1)
	closed := closedAny.(Model)

	if closed.split.enabled || closed.split.firstTab != -1 || closed.split.secondTab != -1 {
		t.Fatalf("split state after plain close = enabled %t panes %d/%d",
			closed.split.enabled, closed.split.firstTab, closed.split.secondTab)
	}
	if len(closed.editors) != 2 || closed.activeTab != 1 {
		t.Fatalf("close state = %d editors, active %d", len(closed.editors), closed.activeTab)
	}
}
