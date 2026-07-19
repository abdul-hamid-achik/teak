package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/git"
	"teak/internal/text"
)

func TestMouseLayoutClassifiesApplicationSurfaces(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		showTree  bool
		showAgent bool
		x         int
		y         int
		want      mouseSurface
		wantX     int
		wantY     int
	}{
		{
			name:     "sidebar tab",
			width:    80,
			height:   24,
			showTree: true,
			x:        2,
			y:        0,
			want:     mouseSidebarTabs,
			wantX:    2,
			wantY:    0,
		},
		{
			name:     "sidebar body",
			width:    80,
			height:   24,
			showTree: true,
			x:        5,
			y:        2,
			want:     mouseSidebarBody,
			wantX:    5,
			wantY:    1,
		},
		{
			name:     "sidebar divider",
			width:    80,
			height:   24,
			showTree: true,
			x:        25,
			y:        8,
			want:     mouseChrome,
			wantX:    25,
			wantY:    8,
		},
		{
			name:     "editor tab",
			width:    80,
			height:   24,
			showTree: true,
			x:        31,
			y:        0,
			want:     mouseEditorTabs,
			wantX:    5,
			wantY:    0,
		},
		{
			name:     "editor body",
			width:    80,
			height:   24,
			showTree: true,
			x:        30,
			y:        4,
			want:     mouseEditorBody,
			wantX:    4,
			wantY:    3,
		},
		{
			name:   "status divider",
			width:  100,
			height: 30,
			x:      50,
			y:      28,
			want:   mouseChrome,
			wantX:  50,
			wantY:  28,
		},
		{
			name:   "status bar",
			width:  100,
			height: 30,
			x:      60,
			y:      29,
			want:   mouseStatus,
			wantX:  60,
			wantY:  0,
		},
		{
			name:      "agent divider",
			width:     100,
			height:    30,
			showTree:  true,
			showAgent: true,
			x:         74,
			y:         10,
			want:      mouseChrome,
			wantX:     74,
			wantY:     10,
		},
		{
			name:      "agent body",
			width:     100,
			height:    30,
			showTree:  true,
			showAgent: true,
			x:         80,
			y:         10,
			want:      mouseAgentBody,
			wantX:     5,
			wantY:     10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				width:     tt.width,
				height:    tt.height,
				showTree:  tt.showTree,
				showAgent: tt.showAgent,
			}

			got, local := m.mouseLayout().hit(tt.x, tt.y)
			if got != tt.want {
				t.Fatalf("surface = %v, want %v", got, tt.want)
			}
			if local.X != tt.wantX || local.Y != tt.wantY {
				t.Errorf("local mouse = (%d,%d), want (%d,%d)", local.X, local.Y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestStatusBarClickDoesNotMoveEditorCursor(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 100
	m.height = 30
	m.relayout()
	before := m.activeEditor().Buffer.Cursor

	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      60,
		Y:      29,
	}))
	updated := updatedModel.(Model)

	if got := updated.activeEditor().Buffer.Cursor; got != before {
		t.Errorf("cursor = %#v, want unchanged %#v", got, before)
	}
}

func TestSettingsCapturesBackgroundClick(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 24
	m.relayout()
	m.showSettings = true
	before := m.activeEditor().Buffer.Cursor

	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      37,
		Y:      1,
	}))
	updated := updatedModel.(Model)

	if !updated.showSettings {
		t.Fatal("Settings closed after a background click")
	}
	if got := updated.activeEditor().Buffer.Cursor; got != before {
		t.Errorf("cursor = %#v, want unchanged %#v", got, before)
	}
}

func TestProblemsBodyClickDoesNotRouteToHiddenTree(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 24
	m.sidebarTab = SidebarProblems
	m.focus = FocusProblems
	m.activeEditor().Buffer.Cursor = text.Position{}
	m.relayout()

	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      5,
		Y:      2,
	}))
	updated := updatedModel.(Model)

	if updated.focus != FocusProblems {
		t.Errorf("focus = %v, want %v", updated.focus, FocusProblems)
	}
	if got := updated.activeEditor().Buffer.Cursor; got != (text.Position{}) {
		t.Errorf("cursor = %#v, want unchanged origin", got)
	}
}

func TestRelayoutPersistsProblemsPanelSize(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 24
	m.relayout()

	if got, want := m.problemsPanel.Height(), 21; got != want {
		t.Errorf("Problems panel height = %d, want %d", got, want)
	}
}

func TestAgentBodyClickFocusesInput(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 100
	m.height = 30
	m.showAgent = true
	m.agentPanel.SetConnected(true)
	m.agentPanel.Blur()
	m.relayout()

	layout := m.mouseLayout()
	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      layout.agentBody.x + 1,
		Y:      layout.agentBody.y + 1,
	}))
	updated := updatedModel.(Model)
	typedModel, _ := updated.Update(tea.KeyPressMsg{Text: "x"})
	typed := typedModel.(Model)

	if typed.focus != FocusAgent {
		t.Errorf("focus = %v, want %v", typed.focus, FocusAgent)
	}
	if got := typed.agentPanel.InputValue(); got != "x" {
		t.Errorf("agent input = %q, want %q", got, "x")
	}
}

func TestRelayoutReleasesFocusWhenAgentAutoHides(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 100
	m.height = 30
	m.showAgent = true
	m.focus = FocusAgent
	m.relayout()
	if m.agentPanelWidth() == 0 {
		t.Fatal("agent unexpectedly hidden at wide layout")
	}

	m.width = 60
	m.relayout()

	if m.agentPanelWidth() != 0 {
		t.Fatal("agent should auto-hide at narrow layout")
	}
	if m.focus != FocusEditor {
		t.Errorf("focus = %v, want %v after agent auto-hide", m.focus, FocusEditor)
	}
}

func TestContextMenuCapturesWheel(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 24
	m.activeEditor().Buffer = text.NewBufferFromBytes([]byte(strings.Repeat("line\n", 100)))
	m.relayout()

	withMenuModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseRight,
		X:      5,
		Y:      2,
	}))
	withMenu := withMenuModel.(Model)
	if !withMenu.treeContextMenu.Visible {
		t.Fatal("tree context menu was not opened")
	}

	before := withMenu.activeEditor().Viewport.ScrollY
	afterModel, _ := withMenu.Update(tea.MouseWheelMsg(tea.Mouse{
		Button: tea.MouseWheelDown,
		X:      30,
		Y:      5,
	}))
	after := afterModel.(Model)

	if got := after.activeEditor().Viewport.ScrollY; got != before {
		t.Errorf("editor ScrollY = %d, want unchanged %d while context menu is open", got, before)
	}
	if !after.treeContextMenu.Visible {
		t.Fatal("tree context menu closed after wheel")
	}
}

func TestTreeContextMenuUsesAbsoluteScreenRow(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 24
	m.relayout()

	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseRight,
		X:      5,
		Y:      2,
	}))
	updated := updatedModel.(Model)

	if !updated.treeContextMenu.Visible {
		t.Fatal("tree context menu was not opened")
	}
	if got := updated.treeContextMenu.Y; got != 2 {
		t.Errorf("tree context menu Y = %d, want absolute screen row 2", got)
	}
}

func TestMouseReleaseStopsEditorDrag(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width = 80
	m.height = 24
	m.activeEditor().Buffer = text.NewBufferFromBytes([]byte("abcdefghijklmnopqrstuvwxyz\n"))
	m.relayout()

	pressedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 10, Y: 1}))
	pressed := pressedModel.(Model)
	draggedModel, _ := pressed.Update(tea.MouseMotionMsg(tea.Mouse{Button: tea.MouseLeft, X: 20, Y: 1}))
	dragged := draggedModel.(Model)
	beforeRelease := dragged.activeEditor().Buffer.Selections.Primary()
	if beforeRelease.IsEmpty() {
		t.Fatal("drag did not create a selection")
	}

	releasedModel, _ := dragged.Update(tea.MouseReleaseMsg(tea.Mouse{Button: tea.MouseLeft, X: 20, Y: 1}))
	released := releasedModel.(Model)
	afterReleaseModel, _ := released.Update(tea.MouseMotionMsg(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: 1}))
	afterRelease := afterReleaseModel.(Model)

	if got := afterRelease.activeEditor().Buffer.Selections.Primary(); got != beforeRelease {
		t.Errorf("selection after release and move = %#v, want %#v", got, beforeRelease)
	}
}

func TestContextMenusConsumeClicksOutsideHorizontalBounds(t *testing.T) {
	t.Run("tree", func(t *testing.T) {
		m := newViewTestModel(t, true)
		m.treeContextMenu.Show([]editor.ContextMenuItem{{Label: "New File...", Action: "tree_new_file"}}, 5, 2)

		updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: 3}))
		updated := updatedModel.(Model)

		if updated.newFileMode {
			t.Error("tree context menu action ran for a click outside the menu")
		}
		if updated.treeContextMenu.Visible {
			t.Error("tree context menu remained visible after an outside click")
		}
	})

	t.Run("git", func(t *testing.T) {
		m := newViewTestModel(t, true)
		m.gitContextEntry = &git.StatusEntry{Path: "main.go"}
		m.gitContextMenu.Show([]editor.ContextMenuItem{{Label: "Stage File", Action: "git_stage"}}, 5, 2)

		updatedModel, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: 3}))
		updated := updatedModel.(Model)

		if cmd != nil {
			t.Error("git context menu action ran for a click outside the menu")
		}
		if updated.gitContextMenu.Visible {
			t.Error("git context menu remained visible after an outside click")
		}
	})

	t.Run("editor", func(t *testing.T) {
		m := newViewTestModel(t, true)
		m.width = 80
		m.height = 24
		m.activeEditor().Buffer = text.NewBufferFromBytes([]byte("abcdefghijklmnopqrstuvwxyz\n"))
		m.relayout()

		openedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: 40, Y: 2}))
		opened := openedModel.(Model)
		if !opened.activeEditor().IsContextMenuVisible() {
			t.Fatal("editor context menu was not opened")
		}

		updatedModel, _ := opened.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: 7}))
		updated := updatedModel.(Model)

		if !updated.activeEditor().Buffer.Selections.Primary().IsEmpty() {
			t.Error("editor context menu action ran for a click outside the menu")
		}
		if updated.activeEditor().IsContextMenuVisible() {
			t.Error("editor context menu remained visible after an outside click")
		}
	})
}
