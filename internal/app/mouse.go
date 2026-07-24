package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/git"
	"teak/internal/overlay"
	"teak/internal/search"
)

type mouseSurface uint8

const (
	mouseOutside mouseSurface = iota
	mouseChrome
	mouseStatus
	mouseSidebarTabs
	mouseSidebarBody
	mouseEditorTabs
	mouseEditorBody
	mouseAgentBody
)

type mousePoint struct {
	X int
	Y int
}

type mouseRect struct {
	x      int
	y      int
	width  int
	height int
}

func newMouseRect(x, y, width, height int) mouseRect {
	return mouseRect{
		x:      x,
		y:      y,
		width:  max(0, width),
		height: max(0, height),
	}
}

func (r mouseRect) contains(x, y int) bool {
	return r.width > 0 &&
		r.height > 0 &&
		x >= r.x &&
		x < r.x+r.width &&
		y >= r.y &&
		y < r.y+r.height
}

func (r mouseRect) local(x, y int) mousePoint {
	return mousePoint{X: x - r.x, Y: y - r.y}
}

func contextMenuRect(x, y int, view string) mouseRect {
	if view == "" {
		return mouseRect{}
	}

	lines := strings.Split(view, "\n")
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	return newMouseRect(x, y, width, len(lines))
}

// mouseLayout is the input counterpart of View's terminal geometry.
// Every routed surface owns an explicit rectangle and all remaining cells are
// chrome, so coordinates cannot leak to a component rendered behind them.
type mouseLayout struct {
	window         mouseRect
	statusDivider  mouseRect
	statusBar      mouseRect
	sidebarTabs    mouseRect
	sidebarBody    mouseRect
	sidebarDivider mouseRect
	editorTabs     mouseRect
	editorBody     mouseRect
	editorPaneB    mouseRect
	agentDivider   mouseRect
	agentBody      mouseRect
}

func (m Model) mouseLayout() mouseLayout {
	if m.width <= 0 || m.height < 2 {
		return mouseLayout{window: newMouseRect(0, 0, m.width, m.height)}
	}

	statusHeight := 2
	compact := m.height < compactTerminalHeight
	if compact {
		statusHeight = 1
	}
	contentHeight := max(0, m.height-statusHeight)
	editorStart := 0
	editorEnd := m.width

	layout := mouseLayout{
		window:    newMouseRect(0, 0, m.width, m.height),
		statusBar: newMouseRect(0, m.height-1, m.width, 1),
	}
	if !compact {
		layout.statusDivider = newMouseRect(0, m.height-2, m.width, 1)
	}

	if !compact && m.treeVisible() {
		treeWidth := m.treeWidth()
		layout.sidebarTabs = newMouseRect(0, 0, treeWidth, min(1, contentHeight))
		layout.sidebarBody = newMouseRect(0, 1, treeWidth, contentHeight-1)
		layout.sidebarDivider = newMouseRect(treeWidth, 0, 1, contentHeight)
		editorStart = treeWidth + 1
	}

	if agentWidth := m.agentPanelWidth(); !compact && agentWidth > 0 {
		agentStart := m.width - agentWidth
		layout.agentDivider = newMouseRect(agentStart-1, 0, 1, contentHeight)
		layout.agentBody = newMouseRect(agentStart, 0, agentWidth, contentHeight)
		editorEnd = agentStart - 1
	}

	editorWidth := max(0, editorEnd-editorStart)
	layout.editorTabs = newMouseRect(editorStart, 0, editorWidth, min(1, contentHeight))
	layout.editorBody = newMouseRect(editorStart, 1, editorWidth, contentHeight-1)

	// While split side-by-side, a click must reach the pane it landed on with
	// coordinates local to that pane. Without this the whole editor region was
	// treated as one surface, so clicks visually inside pane B were delivered to
	// pane A's editor at a column beyond its visible width.
	if m.split.enabled && !m.split.vertical {
		paneAWidth := m.split.paneAWidth(editorWidth)
		paneBStart := editorStart + paneAWidth + 1 // +1 for the divider column
		paneBWidth := max(0, editorEnd-paneBStart)
		if paneBWidth > 0 {
			layout.editorPaneB = newMouseRect(paneBStart, 1, paneBWidth, contentHeight-1)
		}
	}
	return layout
}

// inPaneB reports whether a point falls in the second editor pane.
func (l mouseLayout) inPaneB(x, y int) bool {
	return l.editorPaneB.contains(x, y)
}

func (l mouseLayout) hit(x, y int) (mouseSurface, mousePoint) {
	point := mousePoint{X: x, Y: y}
	if !l.window.contains(x, y) {
		return mouseOutside, point
	}

	switch {
	case l.statusBar.contains(x, y):
		return mouseStatus, l.statusBar.local(x, y)
	case l.statusDivider.contains(x, y):
		return mouseChrome, point
	case l.agentBody.contains(x, y):
		return mouseAgentBody, l.agentBody.local(x, y)
	case l.agentDivider.contains(x, y):
		return mouseChrome, point
	case l.sidebarTabs.contains(x, y):
		return mouseSidebarTabs, l.sidebarTabs.local(x, y)
	case l.sidebarBody.contains(x, y):
		return mouseSidebarBody, l.sidebarBody.local(x, y)
	case l.sidebarDivider.contains(x, y):
		return mouseChrome, point
	case l.editorTabs.contains(x, y):
		return mouseEditorTabs, l.editorTabs.local(x, y)
	case l.editorPaneB.contains(x, y):
		return mouseEditorBody, l.editorPaneB.local(x, y)
	case l.editorBody.contains(x, y):
		return mouseEditorBody, l.editorBody.local(x, y)
	default:
		return mouseChrome, point
	}
}

func mouseAt(mouse tea.Mouse, point mousePoint) tea.Mouse {
	mouse.X = point.X
	mouse.Y = point.Y
	return mouse
}

func (m Model) passiveModalVisible() bool {
	return m.goToLineMode ||
		m.renameMode ||
		m.newFileMode ||
		m.newFolderMode ||
		m.deleteConfirm ||
		m.saveAsMode
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Modal priority mirrors View: the topmost visible surface always consumes
	// the click, even when it has no mouse interaction of its own.
	if m.editorInputCaptured() {
		m.cancelActiveEditorDrag()
	}
	if m.unsavedConfirm != nil {
		updated, cmd := m.unsavedConfirm.Update(msg)
		if updated.IsDismissed() {
			m.unsavedConfirm = nil
		} else {
			m.unsavedConfirm = updated.(*overlay.Confirm)
		}
		return m, cmd
	}
	if !m.overlayStack.IsEmpty() {
		return m, m.overlayStack.Update(msg)
	}
	if m.showBranchPicker {
		return m.updateBranchPicker(msg)
	}
	if m.showSearch {
		if zone.Get("search-replace-btn").InBounds(msg) {
			query := m.searchM.Query()
			replacement := m.searchM.Replacement()
			if query != "" {
				return m, func() tea.Msg {
					return search.ReplaceOneMsg{Query: query, Replacement: replacement}
				}
			}
			return m, nil
		}
		if zone.Get("search-replace-all-btn").InBounds(msg) {
			query := m.searchM.Query()
			replacement := m.searchM.Replacement()
			if query != "" {
				return m, func() tea.Msg {
					return search.ReplaceAllMsg{Query: query, Replacement: replacement}
				}
			}
			return m, nil
		}
		return m.updateSearch(msg)
	}
	if m.showHelp {
		var cmd tea.Cmd
		m.helpM, cmd = m.helpM.Update(msg)
		return m, cmd
	}
	if m.showSettings {
		return m.updateSettings(msg)
	}
	if m.passiveModalVisible() {
		return m, nil
	}

	if m.treeContextMenu.Visible {
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft && contextMenuRect(m.treeContextMenu.X, m.treeContextMenu.Y, m.treeContextMenu.View()).contains(mouse.X, mouse.Y) {
			relY := mouse.Y - m.treeContextMenu.Y - 1
			if item := m.treeContextMenu.SelectAt(relY); item != nil {
				action := item.Action
				m.treeContextMenu.Hide()
				return m.handleTreeContextMenuAction(action)
			}
		}
		m.treeContextMenu.Hide()
		return m, nil
	}
	if m.gitContextMenu.Visible {
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft && contextMenuRect(m.gitContextMenu.X, m.gitContextMenu.Y, m.gitContextMenu.View()).contains(mouse.X, mouse.Y) {
			relY := mouse.Y - m.gitContextMenu.Y - 1
			if item := m.gitContextMenu.SelectAt(relY); item != nil {
				action := item.Action
				m.gitContextMenu.Hide()
				return m.handleGitContextMenuAction(action)
			}
		}
		m.gitContextMenu.Hide()
		return m, nil
	}
	mouse := msg.Mouse()
	if m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible() {
		_, contextRect, ok := m.editorContextMenuGeometry()
		if !ok || mouse.Button != tea.MouseLeft || !contextRect.contains(mouse.X, mouse.Y) {
			m.activeEditor().HideContextMenu()
			return m, nil
		}
		relY := mouse.Y - contextRect.y - 1
		editorModel := m.editors[m.activeTab]
		// Cut, Paste and Undo mutate the buffer, so this path needs the same
		// reconciliation as a keystroke; without it the tab kept a stale dirty
		// indicator and the edit was never sent to the language server.
		prevVersion := editorModel.Buffer.Version()
		prevCursor := editorModel.Buffer.Cursor
		result, cmd, action := editorModel.ClickContextMenuItem(relY)
		m.setEditor(m.activeTab, result)
		if action == "goto_definition" || action == "find_references" || action == "rename_symbol" {
			return m.handleContextMenuAction(action)
		}
		syncCmd := m.syncEditorStateAfterUpdate(m.activeTab, prevVersion, prevCursor)
		return m, tea.Batch(cmd, syncCmd)
	}
	// LSP popups: autocomplete supports mouse selection; other popups consume
	// clicks so they cannot reposition or select text underneath.
	if popup, ok := m.currentLSPOverlayPlacement(); ok && popup.contains(mouse.X, mouse.Y) {
		if ed := m.activeEditor(); ed != nil && ed.IsAutocompleteVisible() && mouse.Button == tea.MouseLeft {
			relY := mouse.Y - popup.y - 1 // -1 for box border
			// Accepting a completion by mouse is a buffer mutation and must go
			// through the same reconciliation as every other edit. Skipping it
			// left the tab undirtied and, more seriously, never sent didChange,
			// so the language server's copy silently diverged from the buffer.
			prevVersion := ed.Buffer.Version()
			prevCursor := ed.Buffer.Cursor
			retokenizeCmd, inserted := ed.AutocompleteSelectAt(relY)
			if inserted {
				syncCmd := m.syncEditorStateAfterUpdate(m.activeTab, prevVersion, prevCursor)
				return m, tea.Batch(retokenizeCmd, syncCmd)
			}
		}
		return m, nil
	}

	surface, local := m.mouseLayout().hit(mouse.X, mouse.Y)
	if m.welcome != nil && m.welcome.Active && (surface == mouseEditorTabs || surface == mouseEditorBody) {
		m.welcome.Dismiss()
	}

	if surface == mouseStatus {
		if zone.Get("status-bar-branch").InBounds(msg) && m.gitPanel.IsGitRepo() {
			m.cancelActiveEditorDrag()
			m.showBranchPicker = true
			m.branchPickerM.SetSize(m.width, m.height)
			return m, tea.Batch(
				git.ListBranchesCmd(m.gitPanel.RootDir()),
				m.branchPickerM.Focus(),
			)
		}
		return m, nil
	}

	switch surface {
	case mouseAgentBody:
		m.setFocus(FocusAgent)
		adjusted := tea.MouseClickMsg(mouseAt(mouse, local))
		var cmd tea.Cmd
		m.agentPanel, cmd = m.agentPanel.Update(adjusted)
		return m, tea.Batch(m.agentPanel.Focus(), cmd)

	case mouseSidebarTabs:
		switch {
		case zone.Get("sidebar-tab-files").InBounds(msg):
			m.sidebarTab = SidebarFiles
			m.setFocus(FocusTree)
		case zone.Get("sidebar-tab-git").InBounds(msg):
			m.sidebarTab = SidebarGit
			m.setFocus(FocusGitPanel)
		case zone.Get("sidebar-tab-problems").InBounds(msg):
			m.sidebarTab = SidebarProblems
			m.setFocus(FocusProblems)
		case zone.Get("sidebar-tab-debugger").InBounds(msg):
			m.sidebarTab = SidebarDebugger
			m.setFocus(FocusDebugger)
		}
		return m, nil

	case mouseSidebarBody:
		switch m.sidebarTab {
		case SidebarGit:
			m.setFocus(FocusGitPanel)
			if mouse.Button == tea.MouseRight {
				return m.showGitContextMenu(mouse.X, mouse.Y, local.Y)
			}
			return m.handleGitPanelClick(local.Y, msg)
		case SidebarProblems:
			m.setFocus(FocusProblems)
			return m.updateProblems(tea.MouseClickMsg(mouseAt(mouse, local)))
		case SidebarDebugger:
			m.setFocus(FocusDebugger)
			return m.updateDebugger(tea.MouseClickMsg(mouseAt(mouse, local)))
		default:
			m.setFocus(FocusTree)
			if mouse.Button == tea.MouseRight {
				return m.showTreeContextMenu(local.X, mouse.Y, local.Y)
			}
			adjusted := tea.MouseClickMsg(mouseAt(mouse, local))
			var cmd tea.Cmd
			m.tree, cmd = m.tree.Update(adjusted)
			return m, cmd
		}

	case mouseEditorTabs:
		m.setFocus(FocusEditor)
		return m.handleTabBarClick(msg)

	case mouseEditorBody:
		m.setFocus(FocusEditor)
		// Clicking a pane focuses it, so the click lands in the buffer the user
		// actually pointed at rather than in whichever pane held focus.
		m.focusSplitPaneAt(mouse.X, mouse.Y)
		updated, cmd := m.forwardToEditor(tea.MouseClickMsg(mouseAt(mouse, local)))
		result := updated.(Model)
		result.clampActiveEditorContextMenu()
		return result, cmd

	default:
		return m, nil
	}
}

func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.unsavedConfirm != nil ||
		!m.overlayStack.IsEmpty() ||
		m.showBranchPicker ||
		m.showSearch ||
		m.showHelp ||
		m.showSettings ||
		m.passiveModalVisible() {
		m.cancelActiveEditorDrag()
		return m, nil
	}
	if m.treeContextMenu.Visible ||
		m.gitContextMenu.Visible ||
		(m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible()) {
		m.cancelActiveEditorDrag()
		return m, nil
	}

	mouse := msg.Mouse()
	if popup, ok := m.currentLSPOverlayPlacement(); ok && popup.contains(mouse.X, mouse.Y) {
		return m, nil
	}
	layout := m.mouseLayout()
	surface, local := layout.hit(mouse.X, mouse.Y)
	if surface == mouseEditorBody {
		return m.forwardToEditor(tea.MouseMotionMsg(mouseAt(mouse, local)))
	}

	// Continue a selection drag after the pointer leaves the editor body. The
	// editor clamps the local coordinates and autoscrolls one row per motion,
	// so sidebars, tab bars, and tiny terminal layouts cannot produce an
	// out-of-range buffer position.
	if editor := m.activeEditor(); editor != nil && editor.IsDragging() && layout.editorBody.width > 0 && layout.editorBody.height > 0 {
		return m.forwardToEditor(tea.MouseMotionMsg(mouseAt(mouse, layout.editorBody.local(mouse.X, mouse.Y))))
	}
	return m, nil
}

func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.editorInputCaptured() {
		m.cancelActiveEditorDrag()
	}
	if m.unsavedConfirm != nil {
		updated, cmd := m.unsavedConfirm.Update(msg)
		if updated.IsDismissed() {
			m.unsavedConfirm = nil
		} else {
			m.unsavedConfirm = updated.(*overlay.Confirm)
		}
		return m, cmd
	}
	if !m.overlayStack.IsEmpty() {
		return m, m.overlayStack.Update(msg)
	}
	if m.showBranchPicker {
		return m.updateBranchPicker(msg)
	}
	if m.showSearch {
		return m.updateSearch(msg)
	}
	if m.showHelp {
		var cmd tea.Cmd
		m.helpM, cmd = m.helpM.Update(msg)
		return m, cmd
	}
	if m.showSettings {
		return m.updateSettings(msg)
	}
	if m.passiveModalVisible() ||
		m.treeContextMenu.Visible ||
		m.gitContextMenu.Visible ||
		(m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible()) {
		return m, nil
	}

	// A drag can finish over any surface. Always notify the editor so a release
	// outside its body cannot leave its selection drag active.
	mouse := msg.Mouse()
	surface, local := m.mouseLayout().hit(mouse.X, mouse.Y)
	if surface == mouseEditorBody {
		return m.forwardToEditor(tea.MouseReleaseMsg(mouseAt(mouse, local)))
	}
	return m.forwardToEditor(msg)
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.unsavedConfirm != nil {
		return m, nil
	}
	if !m.overlayStack.IsEmpty() {
		return m, m.overlayStack.Update(msg)
	}
	if m.showBranchPicker {
		return m.updateBranchPicker(msg)
	}
	if m.showSearch {
		return m.updateSearch(msg)
	}
	if m.showHelp {
		var cmd tea.Cmd
		m.helpM, cmd = m.helpM.Update(msg)
		return m, cmd
	}
	if m.showSettings {
		return m.updateSettings(msg)
	}
	if m.passiveModalVisible() {
		return m, nil
	}
	if m.treeContextMenu.Visible ||
		m.gitContextMenu.Visible ||
		(m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible()) {
		return m, nil
	}

	mouse := msg.Mouse()
	if popup, ok := m.currentLSPOverlayPlacement(); ok && popup.contains(mouse.X, mouse.Y) {
		return m, nil
	}
	surface, local := m.mouseLayout().hit(mouse.X, mouse.Y)
	switch surface {
	case mouseEditorTabs:
		if len(m.tabBar.Tabs) == 0 {
			return m, nil
		}
		next := m.activeTab
		switch mouse.Button {
		case tea.MouseWheelUp:
			next = max(0, next-1)
		case tea.MouseWheelDown:
			next = min(len(m.tabBar.Tabs)-1, next+1)
		default:
			return m, nil
		}
		m.activateTab(next)
		return m, nil

	case mouseAgentBody:
		adjusted := tea.MouseWheelMsg(mouseAt(mouse, local))
		var cmd tea.Cmd
		m.agentPanel, cmd = m.agentPanel.Update(adjusted)
		return m, cmd

	case mouseSidebarBody:
		adjusted := tea.MouseWheelMsg(mouseAt(mouse, local))
		switch m.sidebarTab {
		case SidebarGit:
			var cmd tea.Cmd
			m.gitPanel, cmd = m.gitPanel.Update(adjusted)
			return m, cmd
		case SidebarProblems:
			return m.updateProblems(adjusted)
		case SidebarDebugger:
			return m.updateDebugger(adjusted)
		default:
			var cmd tea.Cmd
			m.tree, cmd = m.tree.Update(adjusted)
			return m, cmd
		}

	case mouseEditorBody:
		return m.forwardToEditor(tea.MouseWheelMsg(mouseAt(mouse, local)))

	default:
		return m, nil
	}
}
