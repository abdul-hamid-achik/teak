package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/config"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(m.cleanup)
	m.width, m.height = 120, 40
	return m
}

func TestSetFocusReleasesTheAreaBeingLeft(t *testing.T) {
	m := newTestModel(t)

	m.setFocus(FocusAgent)
	if cmd := m.agentPanel.Focus(); cmd != nil {
		_ = cmd
	}
	if !m.agentPanel.IsInputFocused() {
		t.Fatal("setup: agent input should be focused")
	}

	// Leaving the agent panel must blur its input; otherwise a phantom caret
	// keeps blinking in a panel that no longer receives keys.
	m.setFocus(FocusEditor)
	if m.agentPanel.IsInputFocused() {
		t.Error("agent input still focused after focus moved to the editor")
	}
}

func TestSetFocusReleasesGitCommitForm(t *testing.T) {
	m := newTestModel(t)
	m.setFocus(FocusGitPanel)
	_ = m.gitPanel.FocusTitle()
	if !m.gitPanel.IsTitleFocused() {
		t.Fatal("setup: commit title should be focused")
	}

	// A stale commit box silently swallows navigation keys on return.
	m.setFocus(FocusTree)
	if m.gitPanel.IsTitleFocused() || m.gitPanel.IsBodyFocused() {
		t.Error("commit form still focused after leaving the git panel")
	}
}

func TestSidebarFocusFollowsVisibleTab(t *testing.T) {
	tests := []struct {
		tab  SidebarTab
		want FocusArea
	}{
		{SidebarFiles, FocusTree},
		{SidebarGit, FocusGitPanel},
		{SidebarProblems, FocusProblems},
		{SidebarDebugger, FocusDebugger},
	}
	for _, tc := range tests {
		m := newTestModel(t)
		m.sidebarTab = tc.tab
		if got := m.sidebarFocus(); got != tc.want {
			t.Errorf("sidebarTab %v: sidebarFocus() = %v, want %v", tc.tab, got, tc.want)
		}
	}
}

func TestReopeningSidebarKeepsFocusOnVisibleTab(t *testing.T) {
	m := newTestModel(t)
	m.showTree = true
	m.sidebarTab = SidebarGit
	m.setFocus(FocusGitPanel)

	hide := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	updated, _, _ := m.handleGlobalKey(hide)
	m = updated.(Model)
	updated, _, _ = m.handleGlobalKey(hide)
	m = updated.(Model)

	// Always restoring FocusTree pointed the arrow keys at an invisible file
	// tree while the Git panel was the one on screen.
	if !m.showTree {
		t.Fatal("sidebar should be visible again")
	}
	if m.focus != FocusGitPanel {
		t.Errorf("focus = %v, want FocusGitPanel to match the visible tab", m.focus)
	}
}

func TestSplitPanesShowDifferentTabs(t *testing.T) {
	m := newTestModel(t)
	m.editors = append(m.editors, m.editors[0], m.editors[0])
	m.activeTab = 0
	m.toggleSplit()

	if m.split.firstTab == m.split.secondTab {
		t.Fatalf("panes share tab %d", m.split.firstTab)
	}
	firstTab := m.split.firstTab

	m.cycleSplitFocus()
	if m.activeTab != m.split.secondTab {
		t.Errorf("after F6, activeTab = %d, want pane B tab %d", m.activeTab, m.split.secondTab)
	}
	// Pane A must still own its own tab: deriving it from activeTab made both
	// panes render the same buffer once focus moved.
	if m.split.firstTab != firstTab {
		t.Errorf("pane A tab changed to %d, want %d", m.split.firstTab, firstTab)
	}

	m.cycleSplitFocus()
	if m.activeTab != firstTab {
		t.Errorf("after a second F6, activeTab = %d, want %d", m.activeTab, firstTab)
	}
}

func TestSplitSizesEachEditorToItsPane(t *testing.T) {
	m := newTestModel(t)
	m.editors = append(m.editors, m.editors[0], m.editors[0])
	m.activeTab = 0
	m.showTree = false
	m.toggleSplit()
	m.relayout()

	paneA := m.editors[m.split.firstTab].Viewport.Width
	paneB := m.editors[m.split.secondTab].Viewport.Width

	// Sizing both to the full width and merely clipping at render time let the
	// cursor sit in a column the user could not see.
	if paneA >= m.width {
		t.Errorf("pane A width = %d, want less than the full width %d", paneA, m.width)
	}
	if paneB >= m.width {
		t.Errorf("pane B width = %d, want less than the full width %d", paneB, m.width)
	}
}

func TestClickInPaneBFocusesPaneB(t *testing.T) {
	m := newTestModel(t)
	m.editors = append(m.editors, m.editors[0], m.editors[0])
	m.activeTab = 0
	m.showTree = false
	m.showAgent = false
	m.toggleSplit()
	m.relayout()

	layout := m.mouseLayout()
	if layout.editorPaneB.width <= 0 {
		t.Fatal("setup: pane B has no mouse rect")
	}
	x := layout.editorPaneB.x + 1
	y := layout.editorPaneB.y + 1
	if !layout.inPaneB(x, y) {
		t.Fatalf("setup: (%d,%d) is not inside pane B", x, y)
	}

	m.focusSplitPaneAt(x, y)

	if m.split.focused != 1 {
		t.Errorf("split.focused = %d, want 1 after clicking pane B", m.split.focused)
	}
	if m.activeTab != m.split.secondTab {
		t.Errorf("activeTab = %d, want pane B tab %d", m.activeTab, m.split.secondTab)
	}
}
