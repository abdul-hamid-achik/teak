package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/editor"
	"teak/internal/editor/overlays"
	"teak/internal/filetree"
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
			want:     mouseSidebarDivider,
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
			m := testModel(modelState{
				width:     tt.width,
				height:    tt.height,
				showTree:  tt.showTree,
				showAgent: tt.showAgent,
			})

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

func TestMouseLayoutCompactHeightMatchesMinimalView(t *testing.T) {
	tests := []struct {
		height int
		y      int
		want   mouseSurface
	}{
		{height: 2, y: 0, want: mouseEditorTabs},
		{height: 2, y: 1, want: mouseStatus},
		{height: 3, y: 0, want: mouseEditorTabs},
		{height: 3, y: 1, want: mouseEditorBody},
		{height: 3, y: 2, want: mouseStatus},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("height-%d-row-%d", tt.height, tt.y), func(t *testing.T) {
			m := testModel(modelState{width: 12, height: tt.height, showTree: true, showAgent: true})
			got, _ := m.mouseLayout().hit(1, tt.y)
			if got != tt.want {
				t.Fatalf("surface = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEditorContextMenuUsesCompactScreenCoordinates(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 3
	m.relayout()

	openedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: 8, Y: 1}))
	opened := openedAny.(Model)
	if !opened.activeEditor().IsContextMenuVisible() {
		t.Fatal("compact editor context menu did not open")
	}
	localX, localY := opened.activeEditor().ContextMenuPosition()
	screenX, screenY := opened.editorContextMenuScreenPosition()
	if screenX != localX || screenY != localY+1 {
		t.Fatalf("compact menu screen position = (%d,%d), want editor-local (%d,%d)", screenX, screenY, localX, localY+1)
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

func TestMouseWheelOverTabBarScrollsStrip(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "package second\n", "package second\n")
	m.activeTab = 0
	m.tabBar.SetActive(0)
	m.relayout()

	// Plain wheel scrolls the tab strip; the active tab stays put.
	updatedModel, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, X: 2, Y: 0}))
	updated := updatedModel.(Model)
	if updated.activeTab != 0 {
		t.Fatalf("active tab = %d, want unchanged 0 (plain wheel scrolls the strip)", updated.activeTab)
	}

	// Shift+wheel preserves the previous tab-cycling behavior.
	updatedModel, _ = updated.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, Mod: tea.ModShift, X: 2, Y: 0}))
	updated = updatedModel.(Model)
	if updated.activeTab != 1 || updated.tabBar.ActiveIdx != 1 {
		t.Fatalf("active tab = %d / %d, want 1 with shift+wheel", updated.activeTab, updated.tabBar.ActiveIdx)
	}
}

func TestMouseWheelOverAutocompleteScrollsPopup(t *testing.T) {
	m := newViewTestModel(t, false)
	// A document taller than the viewport keeps the "editor must not scroll"
	// assertion below honest: with the one-line default buffer a leaked wheel
	// would clamp ScrollY back to zero and the check could never fail.
	m.setEditor(m.activeTab, editor.New(
		text.NewBufferFromBytes([]byte(strings.Repeat("line\n", 100))),
		m.theme,
		editor.Config{TabSize: m.appCfg.Editor.TabSize},
	))
	m.relayout()
	ed := m.activeEditor()
	items := make([]overlays.AutocompleteItem, 15)
	for i := range items {
		label := fmt.Sprintf("wheel%02d", i)
		items[i] = overlays.AutocompleteItem{Label: label, InsertText: label}
	}
	ed.ShowAutocomplete(items)
	p, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView())
	if !ok {
		t.Fatal("lspOverlayPlacement() = not visible")
	}

	// Four wheel notches move the selection 12 items down, past the static
	// first window, so the rendered popup itself proves the wheel reached it.
	for range 4 {
		updatedModel, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, X: p.x, Y: p.y}))
		m = updatedModel.(Model)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "wheel12") {
		t.Fatal("wheel over the autocomplete popup did not scroll it")
	}
	if strings.Contains(view, "wheel00") {
		t.Fatal("popup still renders the first window after wheel scrolling")
	}
	// The popup owns the pointer, so the editor underneath must not scroll.
	if got := m.activeEditor().Viewport.ScrollY; got != 0 {
		t.Fatalf("editor ScrollY = %d, want 0 while wheeling over the popup", got)
	}
}

func TestMouseWheelUpOverAutocompleteScrollsBack(t *testing.T) {
	m := newViewTestModel(t, false)
	ed := m.activeEditor()
	items := make([]overlays.AutocompleteItem, 15)
	for i := range items {
		label := fmt.Sprintf("back%02d", i)
		items[i] = overlays.AutocompleteItem{Label: label, InsertText: label}
	}
	ed.ShowAutocomplete(items)
	p, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView())
	if !ok {
		t.Fatal("lspOverlayPlacement() = not visible")
	}

	// Five notches down clamp the selection on the last item (window 5-14);
	// one notch up must move it back three items to 11 (window 2-11). The
	// rendered window distinguishes a real -3 step from a swallowed wheel-up.
	for range 5 {
		updatedModel, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, X: p.x, Y: p.y}))
		m = updatedModel.(Model)
	}
	updatedModel, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp, X: p.x, Y: p.y}))
	m = updatedModel.(Model)

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "back11") || !strings.Contains(view, "back02") {
		t.Fatalf("wheel-up did not scroll the popup back; view shows neither the selection (back11) nor the window head (back02)")
	}
	if strings.Contains(view, "back00") {
		t.Fatal("popup jumped to the first window; wheel-up should step back three items, not to the top")
	}
	if strings.Contains(view, "back14") {
		t.Fatal("popup still renders the clamped bottom window; wheel-up did not take effect")
	}
}

func TestSidebarDividerDragResizesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newViewTestModel(t, true)
	m.width = 120
	m.height = 30
	m.relayout()

	baseWidth := m.treeWidth()
	dividerX := baseWidth // the divider sits at the sidebar's right edge

	// Click the divider to start the drag.
	updatedModel, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: dividerX, Y: 5}))
	m = updatedModel.(Model)
	if !m.sidebarDragging {
		t.Fatal("clicking the sidebar divider did not start a resize drag")
	}

	// Drag 10 columns to the right.
	updatedModel, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{Button: tea.MouseLeft, X: dividerX + 10, Y: 5}))
	m = updatedModel.(Model)
	if got := m.treeWidth(); got != baseWidth+10 {
		t.Fatalf("tree width mid-drag = %d, want %d", got, baseWidth+10)
	}
	if m.appCfg.UI.TreeWidth != baseWidth+10 {
		t.Fatalf("ui.tree_width mid-drag = %d, want %d", m.appCfg.UI.TreeWidth, baseWidth+10)
	}

	// Release ends the drag and persists the width.
	updatedModel, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{Button: tea.MouseLeft, X: dividerX + 10, Y: 5}))
	m = updatedModel.(Model)
	if m.sidebarDragging {
		t.Fatal("releasing the mouse did not end the sidebar drag")
	}
	if cmd == nil {
		t.Fatal("releasing the sidebar drag did not schedule a config save")
	}
	msg := cmd()
	result, ok := msg.(settingsSaveResultMsg)
	if !ok || result.Err != nil {
		t.Fatalf("sidebar width save = %#v, want a successful settingsSaveResultMsg", msg)
	}
	if result.Config.UI.TreeWidth != baseWidth+10 {
		t.Fatalf("persisted tree width = %d, want %d", result.Config.UI.TreeWidth, baseWidth+10)
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

func TestTreeContextMenuWaitsForPendingProjection(t *testing.T) {
	m := newViewTestModel(t, true)
	m.tree = filetree.NewEmpty(m.rootDir, m.theme)
	m.tree.SetSize(24, 10)
	m.tree.Entries = []filetree.Entry{
		{Name: ".hidden", Path: filepath.Join(m.rootDir, ".hidden")},
		{Name: "rename-me.txt", Path: filepath.Join(m.rootDir, "rename-me.txt")},
	}
	_ = m.tree.View()
	_, projectionCmd := m.tree.ToggleShowHiddenAsync()

	pendingAny, _ := m.showTreeContextMenu(5, 2, 0)
	pending := pendingAny.(Model)
	if pending.treeContextMenu.Visible {
		t.Fatal("tree context menu opened against a stale pending projection")
	}

	ready, ok := projectionCmd().(filetree.FilterReadyMsg)
	if !ok {
		t.Fatal("visibility projection returned an unexpected message")
	}
	resolvedAny, _ := pending.handleTreeFilterReady(ready)
	resolved := resolvedAny.(Model)
	if !resolved.treeContextMenu.Visible || !strings.Contains(resolved.treeContextMenu.View(), "Rename...") {
		t.Fatal("queued tree context menu did not open against the prepared projection")
	}
}

func TestTreeContextMenuStaysInsideInteractiveViewport(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 80
	m.height = 12
	m.relayout()

	updatedAny, _ := m.showTreeContextMenu(24, 10, 0)
	updated := updatedAny.(Model)
	rect := contextMenuRect(updated.treeContextMenu.X, updated.treeContextMenu.Y, updated.treeContextMenu.View())
	if rect.x+rect.width > updated.width {
		t.Errorf("menu right edge = %d, width = %d", rect.x+rect.width, updated.width)
	}
	if rect.y+rect.height > updated.height-2 {
		t.Errorf("menu bottom = %d, content bottom = %d", rect.y+rect.height, updated.height-2)
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

func TestMouseDragContinuesPastEditorBottom(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width = 40
	m.height = 7 // editor body: rows 1..4; row 5 is the status divider
	m.activeEditor().Buffer = text.NewBufferFromBytes([]byte("zero\none\ntwo\nthree\nfour\nfive"))
	m.relayout()

	pressedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 4, Y: 1}))
	pressed := pressedAny.(Model)
	if !pressed.activeEditor().IsDragging() {
		t.Fatal("press did not start an editor drag")
	}

	// The pointer is outside the editor body, but the drag should still route
	// to it with a local Y one past the viewport, causing controlled scroll.
	draggedAny, _ := pressed.Update(tea.MouseMotionMsg(tea.Mouse{Button: tea.MouseLeft, X: 4, Y: 5}))
	dragged := draggedAny.(Model)
	if got := dragged.activeEditor().Viewport.ScrollY; got != 1 {
		t.Fatalf("ScrollY = %d, want 1 after dragging past editor bottom", got)
	}
	if dragged.activeEditor().Buffer.Selections.Primary().IsEmpty() {
		t.Fatal("drag past editor bottom should extend the selection")
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

func TestBranchPickerMouseIsModalAndActivatesOnlyVisibleRows(t *testing.T) {
	m := newViewTestModel(t, false)
	m.showBranchPicker = true
	m.branchPickerM.SetSize(m.width, m.height)
	m.branchPickerM.SetBranches([]string{"main", "feature/caf\u00e9", "release"}, "main")

	// The centered 60-cell picker has its list at (32, 19) for a 120x40
	// terminal. A click in the editor, outside the modal cells, is consumed by
	// the modal rather than changing editor focus/cursor or starting a switch.
	before := m.activeEditor().Buffer.Cursor
	updatedAny, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: 2}))
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatal("outside branch-picker click returned a command")
	}
	if !updated.showBranchPicker {
		t.Fatal("outside branch-picker click closed the modal")
	}
	if got := updated.activeEditor().Buffer.Cursor; got != before {
		t.Fatalf("outside modal click reached editor: cursor = %+v, want %+v", got, before)
	}

	// The second visible row is a Unicode branch name. Click its first
	// rendered terminal cell; the root model must return the exact branch
	// command and leave subsequent result handling to the normal git route.
	updatedAny, cmd = updated.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: 32, Y: 20}))
	updated = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("visible branch row click returned no switch command")
	}
	msg := cmd()
	switchMsg, ok := msg.(git.SwitchBranchMsg)
	if !ok {
		t.Fatalf("branch row click command = %T, want git.SwitchBranchMsg", msg)
	}
	if switchMsg.Branch != "feature/caf\u00e9" {
		t.Errorf("branch = %q, want Unicode branch", switchMsg.Branch)
	}
	if !updated.showBranchPicker {
		t.Fatal("picker should close only when its SwitchBranchMsg is processed")
	}
}
