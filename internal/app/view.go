package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

const narrowViewportClipWidth = 40

// compactTerminalHeight is the smallest height that can show the normal
// tab/editor/divider/status layout without drawing more rows than the PTY.
const compactTerminalHeight = 4

// editorInputCaptured reports whether a surface above the editor owns user
// input. The native terminal cursor must never remain behind such a surface:
// it otherwise looks editable and can blink through small modal overlays.
func (m Model) editorInputCaptured() bool {
	return m.unsavedConfirm != nil ||
		!m.overlayStack.IsEmpty() ||
		m.showBranchPicker ||
		m.showSearch ||
		m.showHelp ||
		m.showSettings ||
		m.passiveModalVisible() ||
		m.treeContextMenu.Visible ||
		m.gitContextMenu.Visible ||
		(m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible())
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.modelState == nil {
		return tea.NewView("")
	}
	// The full layout reserves a divider and a status row. Below two terminal
	// rows there is no renderable editor surface; returning an empty view avoids
	// negative sidebar heights while the terminal is being resized.
	if m.width <= 0 || m.height < 2 {
		return tea.NewView("")
	}
	// A zero-value Model is a supported embedding/test boundary. Update lazily
	// installs enough bookkeeping to accept messages, but it intentionally does
	// not start BubbleZone or construct a real editor owner. Keep that inert
	// state renderable without depending on cmd/teak's global zone setup.
	if len(m.editors) == 0 && m.welcome == nil && len(m.tabBar.Tabs) == 0 {
		return tea.NewView("")
	}

	var content string
	statusBar := m.renderStatusBar()

	welcomeActive := m.welcome != nil && m.welcome.Active

	if m.height < compactTerminalHeight {
		// At two rows there is room only for tabs plus a compact status line;
		// at three rows one editor row also fits. Side panels and their borders
		// are intentionally hidden until the full vertical chrome fits.
		content = m.tabBar.View()
		if m.height == compactTerminalHeight-1 {
			var editorView string
			if welcomeActive {
				editorView = m.welcome.View()
			} else if m.isActiveDiffTab() {
				editorView = m.activeDiffView()
			} else if m.activeEditor() != nil {
				editorView = m.activeEditor().View()
			}
			content += "\n" + editorView
		}
		content += "\n" + m.renderCompactStatusBar()
	} else if m.treeVisible() {
		content = m.viewWithTree() + "\n" + statusBar
	} else {
		tabBarView := m.tabBar.View()
		var editorView string
		if m.split.enabled && m.split.secondTab >= 0 && m.split.secondTab < len(m.editors) {
			editorView = m.renderSplitPanes()
		} else if welcomeActive {
			editorView = m.welcome.View()
		} else if m.isActiveDiffTab() {
			editorView = m.activeDiffView()
		} else if m.activeEditor() != nil {
			editorView = m.activeEditor().View()
		}
		editorCol := tabBarView + "\n" + editorView
		// Agent panel on the right (no-tree mode)
		if m.showAgent && m.agentPanelWidth() > 0 {
			sidebarHeight := m.height - 2
			rightBorder := m.agentBorderColumn(sidebarHeight)
			agentView := m.agentPanel.View()
			editorCol = lipgloss.JoinHorizontal(lipgloss.Top, editorCol, rightBorder, agentView)
		}
		content = editorCol + "\n" + statusBar
	}

	// LSP popups are editor-local surfaces rather than global modals. Compose
	// only the highest-priority one inside the editor body before menus and
	// global overlays take precedence over it.
	if !m.isActiveDiffTab() {
		if popup, ok := m.currentLSPOverlayPlacement(); ok {
			content = ui.PlaceOverlayAt(content, popup.content, popup.x, popup.y, m.width, m.height)
		}
	}

	// Overlay context menus (rendered before help/search so they show in normal view).
	//
	// These are three independent surfaces, not alternatives: the sidebar menus
	// belong to the tree and git panels, which are visible alongside any tab.
	// Chaining them behind the editor menu's diff-tab check made both sidebar
	// menus unreachable on a normal tab — right-clicking the sidebar entered the
	// menu's input-capturing state while drawing nothing.
	if !m.isActiveDiffTab() {
		if cmView, cmRect, ok := m.editorContextMenuGeometry(); ok {
			content = ui.PlaceOverlayAt(content, cmView, cmRect.x, cmRect.y, m.width, m.height)
		}
	}
	if m.gitContextMenu.Visible {
		cmView := m.gitContextMenu.View()
		content = ui.PlaceOverlayAt(content, cmView, m.gitContextMenu.X, m.gitContextMenu.Y, m.width, m.height)
	}
	if m.treeContextMenu.Visible {
		cmView := m.treeContextMenu.View()
		content = ui.PlaceOverlayAt(content, cmView, m.treeContextMenu.X, m.treeContextMenu.Y, m.width, m.height)
	}

	// Branch picker overlay
	if m.showBranchPicker {
		pickerView := m.branchPickerM.View()
		content = ui.RenderOverlay(content, pickerView, m.width, m.height)
	}

	// Overlay help, search, or go-to-line
	if m.showHelp {
		helpContent := m.helpM.View()
		content = ui.RenderOverlay(content, helpContent, m.width, m.height)
	} else if m.showSettings {
		// Settings overlay shares its geometry with mouse hit-testing.
		settingsView := m.settingsM.View()
		hint := m.theme.Gutter.Render("\n\n↑↓ select  •  click a control to change  •  Ctrl+S save  •  Esc close")
		settingsView += hint

		centerX, centerY, modalWidth, _ := m.settingsModalGeometry()

		// Wrap in a box with border
		settingsBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(1, 2).
			Width(modalWidth).
			Render(settingsView)

		content = ui.PlaceOverlayAt(content, settingsBox, centerX, centerY, m.width, m.height)
	} else if m.showSearch {
		searchView := m.searchM.View()
		content = ui.RenderOverlay(content, searchView, m.width, m.height)
	} else if m.goToLineMode {
		goToBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("Go to Line: %s_", m.goToLineInput))
		content = ui.RenderOverlay(content, goToBox, m.width, m.height)
	} else if m.renameMode {
		renameBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("Rename Symbol: %s_", m.renameInput))
		content = ui.RenderOverlay(content, renameBox, m.width, m.height)
	} else if m.treeRenameMode || m.treeCopyMode || m.treeMoveMode {
		prompt := "Move to workspace directory"
		if m.treeRenameMode {
			prompt = "Rename file or folder"
		} else if m.treeCopyMode {
			prompt = "Duplicate as"
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("%s: %s_", prompt, m.treeEditInput))
		content = ui.RenderOverlay(content, box, m.width, m.height)
	} else if m.newFileMode {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("New File: %s_", m.newItemInput))
		content = ui.RenderOverlay(content, box, m.width, m.height)
	} else if m.newFolderMode {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("New Folder: %s_", m.newItemInput))
		content = ui.RenderOverlay(content, box, m.width, m.height)
	} else if m.deleteConfirm {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord11).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("Delete %s? (y/N)", filepath.Base(m.deleteTarget)))
		content = ui.RenderOverlay(content, box, m.width, m.height)
	} else if m.saveAsMode {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ui.Nord3).
			Background(ui.Nord1).
			Padding(0, 1).
			Render(fmt.Sprintf("Save As: %s_", m.saveAsInput))
		content = ui.RenderOverlay(content, box, m.width, m.height)
	}

	// Overlay stack (quick open, command palette)
	if !m.overlayStack.IsEmpty() {
		content = ui.RenderOverlay(content, m.overlayStack.View(), m.width, m.height)
	}

	// Unsaved changes confirm dialog (highest priority overlay)
	if m.unsavedConfirm != nil {
		content = ui.RenderOverlay(content, m.unsavedConfirm.View(), m.width, m.height)
	}

	// Several child components have irreducible chrome (line-number gutters,
	// tab labels and status hints). During a live resize those minimums can be
	// wider than the whole terminal. Enforce the outer viewport contract before
	// BubbleZone scans it, so rendered cells and mouse zones are derived from the
	// same clipped content. Restrict this extra ANSI pass to narrow terminals to
	// avoid adding work to the normal render hot path.
	if m.width < narrowViewportClipWidth {
		content = clipViewLines(content, m.width)
	}
	content = clipViewRows(content, m.height)
	scanned := zone.Scan(content)
	v := tea.NewView(scanned)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	if !m.editorInputCaptured() && !welcomeActive && !m.isActiveDiffTab() && m.focus == FocusEditor && m.activeEditor() != nil {
		cx, cy := m.activeEditor().CursorPosition()
		if m.height >= compactTerminalHeight && m.treeVisible() {
			cx += m.treeWidth() + 1
		}
		if m.split.enabled && !m.split.vertical && m.split.focused == 1 && m.split.secondTab == m.activeTab {
			// Pane B is rendered after pane A and the divider. The editor's
			// CursorPosition is local to its own viewport, so the terminal
			// cursor needs the same horizontal offset as the split renderer.
			cx += m.split.paneAWidth(m.splitEditorWidth()) + 1
		}
		cy += 1 // +1 for tab bar
		if cy >= 0 && cy < m.height-1 && cx >= 0 && cx < m.width {
			cursor := tea.NewCursor(cx, cy)
			cursor.Shape = tea.CursorBar
			cursor.Blink = true
			v.Cursor = cursor
		}
	}

	return v
}

// renderSplitPanes renders two editor panes side by side with a divider.
func (m Model) renderSplitPanes() string {
	totalWidth := m.splitEditorWidth()

	paneAW := m.split.paneAWidth(totalWidth)
	paneBW := m.split.paneBWidth(totalWidth)
	statusHeight := 2
	tabBarHeight := 1
	paneH := m.height - statusHeight - tabBarHeight
	if paneH < 1 {
		paneH = 1
	}

	// Each pane renders the tab it owns. Rendering pane A from activeEditor()
	// meant that focusing pane B — which moves activeTab — made both panes show
	// the same buffer.
	paneAView := ""
	if tab := m.split.firstTab; tab >= 0 && tab < len(m.editors) {
		paneAView = m.editors[tab].View()
	}
	paneAView = clipViewRows(paneAView, paneH)
	paneAView = clipViewLines(paneAView, paneAW)

	paneBView := ""
	if m.split.secondTab >= 0 && m.split.secondTab < len(m.editors) {
		paneBView = m.editors[m.split.secondTab].View()
	}
	paneBView = clipViewRows(paneBView, paneH)
	paneBView = clipViewLines(paneBView, paneBW)

	dividerStyle := lipgloss.NewStyle().Foreground(ui.Nord3)
	dividerLines := make([]string, paneH)
	for i := range dividerLines {
		dividerLines[i] = dividerStyle.Render("│")
	}
	divider := strings.Join(dividerLines, "\n")

	return lipgloss.JoinHorizontal(lipgloss.Top, paneAView, divider, paneBView)
}

// splitEditorWidth returns the horizontal space available to the two panes.
// Keeping this calculation in one place prevents the renderer and native
// cursor from disagreeing about pane-B's terminal offset when side panels are
// visible.
func (m Model) splitEditorWidth() int {
	agentExtra := 0
	if m.showAgent {
		if width := m.agentPanelWidth(); width > 0 {
			agentExtra = width + 1
		}
	}
	treeExtra := 0
	if m.treeVisible() {
		treeExtra = m.treeWidth() + 1
	}
	return max(1, m.width-treeExtra-agentExtra)
}

func clipViewLines(content string, width int) string {
	if width <= 0 || content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

func clipViewRows(content string, height int) string {
	if height <= 0 || content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m Model) activeDiffView() string {
	if dv, ok := m.diffViews[m.activeTab]; ok {
		return dv.View()
	}
	return ""
}

// viewWithTree: sidebar tab bar + active panel on left, tab bar + editor on right.
func (m Model) viewWithTree() string {
	tabBarView := m.tabBar.View()

	// Editor column: tab bar + editor content (possibly split)
	var editorColumn string
	if m.split.enabled && m.split.secondTab >= 0 && m.split.secondTab < len(m.editors) {
		editorColumn = tabBarView + "\n" + m.renderSplitPanes()
	} else {
		var editorView string
		if m.welcome != nil && m.welcome.Active {
			editorView = m.welcome.View()
		} else if m.isActiveDiffTab() {
			editorView = m.activeDiffView()
		} else if m.activeEditor() != nil {
			editorView = m.activeEditor().View()
		}
		editorColumn = tabBarView + "\n" + editorView
	}

	// Build sidebar: tab bar (1 line) + active panel
	sidebarHeight := m.height - 2 // minus divider + status bar

	tw := m.treeWidth()
	tabBar := m.sidebarTabBar()

	var panelView string
	switch m.sidebarTab {
	case SidebarGit:
		panelView = lipgloss.NewStyle().Width(tw).Render(m.gitPanel.View())
	case SidebarProblems:
		panelView = lipgloss.NewStyle().Width(tw).Render(m.problemsPanel.View())
	case SidebarDebugger:
		panelView = lipgloss.NewStyle().Width(tw).Render(m.debuggerPanel.View())
	default:
		panelView = lipgloss.NewStyle().Width(tw).Render(m.tree.View())
	}

	sidebarView := tabBar + "\n" + panelView

	// Border column: full height
	borderLines := make([]string, sidebarHeight)
	for i := range sidebarHeight {
		borderLines[i] = m.theme.TreeBorder.Render("│")
	}
	borderCol := strings.Join(borderLines, "\n")

	result := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, borderCol, editorColumn)

	// Agent panel on the right
	if m.showAgent && m.agentPanelWidth() > 0 {
		rightBorder := m.agentBorderColumn(sidebarHeight)
		agentView := m.agentPanel.View()
		result = lipgloss.JoinHorizontal(lipgloss.Top, result, rightBorder, agentView)
	}

	return result
}

// sidebarTabBar renders the 1-line icon bar at the top of the sidebar.
func (m Model) sidebarTabBar() string {
	tw := m.treeWidth()

	fileIcon := " \uf413 "     // nf-oct-file_directory_fill
	gitIcon := " \ue725 "      // nf-dev-git_branch
	problemsIcon := " \uea88 " // nf-cod-problems
	debuggerIcon := " \ueb0c " // nf-cod-debug
	if !ui.NerdFontEnabled() {
		fileIcon = " F "
		gitIcon = " G "
		problemsIcon = " ! "
		debuggerIcon = " D "
	}

	var fileTab, gitTab, problemsTab, debuggerTab string
	if m.sidebarTab == SidebarFiles {
		fileTab = m.theme.SidebarTabActive.Render(fileIcon)
	} else {
		fileTab = m.theme.SidebarTabInactive.Render(fileIcon)
	}
	if m.sidebarTab == SidebarGit {
		gitTab = m.theme.SidebarTabActive.Render(gitIcon)
	} else {
		gitTab = m.theme.SidebarTabInactive.Render(gitIcon)
	}
	if m.sidebarTab == SidebarProblems {
		problemsTab = m.theme.SidebarTabActive.Render(problemsIcon)
	} else {
		problemsTab = m.theme.SidebarTabInactive.Render(problemsIcon)
	}
	if m.sidebarTab == SidebarDebugger {
		debuggerTab = m.theme.SidebarTabActive.Render(debuggerIcon)
	} else {
		debuggerTab = m.theme.SidebarTabInactive.Render(debuggerIcon)
	}

	fileTab = zone.Mark("sidebar-tab-files", fileTab)
	gitTab = zone.Mark("sidebar-tab-git", gitTab)
	problemsTab = zone.Mark("sidebar-tab-problems", problemsTab)
	debuggerTab = zone.Mark("sidebar-tab-debugger", debuggerTab)

	// Keep the tab chrome inside the sidebar at tiny terminal widths. In
	// particular, never rely on the renderer to crop zone-marked content: that
	// can make the visual layout and mouse coordinates disagree.
	bar := ""
	for _, tab := range []string{fileTab, gitTab, problemsTab, debuggerTab} {
		if lipgloss.Width(bar)+lipgloss.Width(tab) > tw {
			break
		}
		bar += tab
	}
	// Pad to full sidebar width
	padWidth := tw - lipgloss.Width(bar)
	if padWidth > 0 {
		bar += lipgloss.NewStyle().Background(ui.Nord0).Render(strings.Repeat(" ", padWidth))
	}
	return bar
}

func (m Model) renderStatusBar() string {
	// Left: F1 Help + git branch (or project name fallback)
	helpHint := m.theme.TabInactive.Render(" F1 Help ")
	var branchPart string
	if m.gitBranch != "" {
		branchLabel := fmt.Sprintf("  %s", m.gitBranch)
		branchPart = zone.Mark("status-bar-branch", branchLabel)
	} else if m.rootDir != "" {
		branchPart = "  " + filepath.Base(m.rootDir)
	}
	left := helpHint + branchPart

	var right string
	if ed := m.activeEditor(); ed != nil {
		buf := ed.Buffer
		tabInfo := fmt.Sprintf("Spaces: %d", ed.Config.TabSize)
		scrollPos := m.scrollIndicator()
		lspStatus := m.lspIndicator()
		procStatus := m.procMonIndicator()
		problemsStatus := m.problemsStatus()
		agentStatus := m.agentIndicator()
		right = m.theme.StatusText.Render(
			fmt.Sprintf(" Ln %d, Col %d  %s  LF  UTF-8  %s%s%s ",
				buf.Cursor.Line+1, ed.StatusColumn(), tabInfo, scrollPos, lspStatus+procStatus, problemsStatus),
		) + agentStatus
	}

	// Center: status message; when idle, surface the diagnostic under the
	// cursor so the common case does not require opening the problems panel.
	center := m.status
	if center == "" {
		if ed := m.activeEditor(); ed != nil {
			center = ed.DiagnosticMessageAtLine(ed.Buffer.Cursor.Line, m.width/2)
		}
	}

	// Calculate padding
	usedWidth := ansi.StringWidth(left) + ansi.StringWidth(right) + ansi.StringWidth(center)
	padding := max(0, m.width-usedWidth)

	bar := left + " " + center + strings.Repeat(" ", max(0, padding-1)) + right
	// Lipgloss Width wraps overlong content. A status bar must remain exactly
	// one terminal row even when its fixed hints cannot fit.
	bar = ansi.Truncate(bar, m.width, "")
	if missing := m.width - ansi.StringWidth(bar); missing > 0 {
		bar += strings.Repeat(" ", missing)
	}

	// Divider line above status bar
	divider := m.theme.TreeBorder.Render(strings.Repeat("─", m.width))
	return divider + "\n" + m.theme.StatusBar.Width(m.width).Render(bar)
}

func (m Model) renderCompactStatusBar() string {
	label := "F1 Help"
	if m.status != "" {
		label = m.status
	}
	label = ansi.Truncate(label, m.width, "")
	if missing := m.width - ansi.StringWidth(label); missing > 0 {
		label += strings.Repeat(" ", missing)
	}
	return m.theme.StatusBar.Render(label)
}

func (m Model) scrollIndicator() string {
	if m.activeEditor() == nil {
		return ""
	}
	ed := m.activeEditor()
	buf := ed.Buffer
	totalLines := buf.LineCount()
	viewHeight := ed.Viewport.Height
	scrollY := ed.Viewport.ScrollY
	if ed.Config.WordWrap && ed.Wrap != nil {
		// Word-wrap scrolls through visual rows, not logical source lines. The
		// layout total is deliberately a safe estimate until pages are visited,
		// matching the editor scrollbar without eagerly measuring a large file.
		totalLines = ed.Wrap.TotalRows()
		scrollY = ed.Viewport.WrapScrollY
	}

	if totalLines <= viewHeight {
		return "All"
	}
	if scrollY == 0 {
		return "Top"
	}
	maxScroll := totalLines - viewHeight
	if scrollY >= maxScroll {
		return "Bot"
	}
	pct := scrollY * 100 / maxScroll
	return fmt.Sprintf("%d%%", pct)
}

func (m Model) lspIndicator() string {
	if m.activeEditor() == nil {
		return ""
	}
	buf := m.activeEditor().Buffer
	if buf.FilePath == "" {
		return ""
	}
	health := m.lspMgr.ServerHealth(buf.FilePath)
	if health.Name == "" {
		return ""
	}
	switch health.State {
	case "ready":
		return "  " + health.Name + " ●"
	case "starting", "running":
		return "  " + health.Name + " ◐"
	case "retrying":
		return "  " + health.Name + " retrying"
	case "failed":
		return "  " + health.Name + " failed"
	case "stopped":
		return "  " + health.Name + " stopped"
	default:
		return "  " + health.Name + " ○"
	}
}

func (m Model) procMonIndicator() string {
	if m.procMon == nil || !m.procMon.Available() {
		return ""
	}
	label, rss, warning := m.procMon.Status()
	if label == "" {
		return ""
	}
	if warning {
		return " (" + label + " " + rss + "!)"
	}
	return " (" + label + " " + rss + ")"
}

// problemsStatus returns a string showing the problem count for the status bar.
func (m Model) problemsStatus() string {
	errors := m.problemsPanel.ErrorCount()
	warnings := m.problemsPanel.WarningCount()
	total := m.problemsPanel.ProblemCount()

	if total == 0 {
		return ""
	}

	parts := []string{}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("✗ %d", errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("⚠ %d", warnings))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("ℹ %d", total))
	}

	return "  " + strings.Join(parts, "  ")
}
