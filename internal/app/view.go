package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/editor"
	"teak/internal/ui"
)

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	// Set debug gutter state on active editor
	if ed := m.activeEditor(); ed != nil {
		filePath := ed.Buffer.FilePath
		bpEntries := m.breakpoints[filePath]
		if len(bpEntries) > 0 || m.currentExecLine >= 0 {
			bpMap := make(map[int]editor.BreakpointState, len(bpEntries))
			for _, bp := range bpEntries {
				if bp.Enabled {
					bpMap[bp.Line] = editor.BPActive
				} else {
					bpMap[bp.Line] = editor.BPDisabled
				}
			}
			execLine := -1
			if m.currentExecFile == filePath {
				execLine = m.currentExecLine
			}
			ed.DebugGutter = &editor.GutterOpts{
				Breakpoints: bpMap,
				ExecLine:    execLine,
			}
		} else {
			ed.DebugGutter = nil
		}
	}

	var content string
	statusBar := m.renderStatusBar()

	welcomeActive := m.welcome != nil && m.welcome.Active

	if m.showTree {
		content = m.viewWithTree() + "\n" + statusBar
	} else {
		tabBarView := m.tabBar.View()
		var editorView string
		if welcomeActive {
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

	// Overlay context menus (rendered before help/search so they show in normal view)
	if !m.isActiveDiffTab() && m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible() {
		cmView := m.activeEditor().ContextMenuView()
		cmX, cmY := m.activeEditor().ContextMenuPosition()
		if m.showTree {
			cmX += m.treeWidth() + 1
		}
		cmY += 1 // +1 for tab bar
		content = ui.PlaceOverlayAt(content, cmView, cmX, cmY, m.width, m.height)
	} else if m.gitContextMenu.Visible {
		cmView := m.gitContextMenu.View()
		content = ui.PlaceOverlayAt(content, cmView, m.gitContextMenu.X, m.gitContextMenu.Y, m.width, m.height)
	} else if m.treeContextMenu.Visible {
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
		// Settings overlay with fixed size and centered position
		settingsView := m.settingsM.View()
		// Add hint at the bottom
		hint := m.theme.Gutter.Render("\n\nPress 'r' to reset, '+'/'-' to change, ESC to close")
		settingsView += hint

		// Fixed modal dimensions
		modalWidth := 72
		modalHeight := 22

		// Center the modal
		centerX := (m.width - modalWidth) / 2
		centerY := (m.height - modalHeight) / 2
		if centerX < 0 {
			centerX = 0
		}
		if centerY < 0 {
			centerY = 0
		}

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

	scanned := zone.Scan(content)
	v := tea.NewView(scanned)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	if !m.showHelp && !m.showSearch && !m.renameMode && !welcomeActive && !m.isActiveDiffTab() && m.overlayStack.IsEmpty() && m.unsavedConfirm == nil && m.focus == FocusEditor && m.activeEditor() != nil {
		cx, cy := m.activeEditor().CursorPosition()
		if m.showTree {
			cx += m.treeWidth() + 1
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

func (m Model) activeDiffView() string {
	if dv, ok := m.diffViews[m.activeTab]; ok {
		return dv.View()
	}
	return ""
}

// viewWithTree: sidebar tab bar + active panel on left, tab bar + editor on right.
func (m Model) viewWithTree() string {
	tabBarView := m.tabBar.View()
	var editorView string
	if m.welcome != nil && m.welcome.Active {
		editorView = m.welcome.View()
	} else if m.isActiveDiffTab() {
		editorView = m.activeDiffView()
	} else if m.activeEditor() != nil {
		editorView = m.activeEditor().View()
	}

	// Editor column: tab bar + editor content
	editorColumn := tabBarView + "\n" + editorView

	// Build sidebar: tab bar (1 line) + active panel
	sidebarHeight := m.height - 2    // minus divider + status bar
	panelHeight := sidebarHeight - 1 // minus sidebar tab bar
	if panelHeight < 1 {
		panelHeight = 1
	}

	tw := m.treeWidth()
	tabBar := m.sidebarTabBar()

	var panelView string
	switch m.sidebarTab {
	case SidebarGit:
		m.gitPanel.SetSize(tw, panelHeight)
		panelView = lipgloss.NewStyle().Width(tw).Render(m.gitPanel.View())
	case SidebarProblems:
		m.problemsPanel.SetSize(tw, panelHeight)
		panelView = lipgloss.NewStyle().Width(tw).Render(m.problemsPanel.View())
	case SidebarDebugger:
		m.debuggerPanel.SetSize(tw, panelHeight)
		panelView = lipgloss.NewStyle().Width(tw).Render(m.debuggerPanel.View())
	default:
		m.tree.SetSize(tw, panelHeight)
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

	bar := fileTab + gitTab + problemsTab + debuggerTab
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
		problemsStatus := m.problemsStatus()
		agentStatus := m.agentIndicator()
		right = m.theme.StatusText.Render(
			fmt.Sprintf(" Ln %d, Col %d  %s  LF  UTF-8  %s%s%s ",
				buf.Cursor.Line+1, buf.Cursor.Col+1, tabInfo, scrollPos, lspStatus, problemsStatus),
		) + agentStatus
	}

	// Center: status message
	center := m.status

	// Calculate padding
	usedWidth := lipglossWidth(left) + lipglossWidth(right) + len(center)
	padding := max(0, m.width-usedWidth)

	bar := left + " " + center + strings.Repeat(" ", max(0, padding-1)) + right

	// Divider line above status bar
	divider := m.theme.TreeBorder.Render(strings.Repeat("─", m.width))
	return divider + "\n" + m.theme.StatusBar.Width(m.width).Render(bar)
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
	name, running, ready := m.lspMgr.ServerStatus(buf.FilePath)
	if name == "" {
		return ""
	}
	if running && ready {
		return "  " + name + " ●"
	}
	if running {
		return "  " + name + " ◐"
	}
	return "  " + name + " ○"
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
