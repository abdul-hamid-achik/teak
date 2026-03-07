package app

import (
	"teak/internal/agent"
	"teak/internal/debugger"
	"teak/internal/editor"
	"teak/internal/filetree"
	"teak/internal/git"
	"teak/internal/problems"
	"teak/internal/ui"
)

// SidebarManager handles all sidebar panels and their interactions.
type SidebarManager struct {
	tree          filetree.Model
	showTree      bool
	activeTab     SidebarTab
	gitPanel      git.Model
	problemsPanel problems.Model
	debuggerPanel debugger.Model
	showAgent     bool
	agentPanel    agent.Model

	// Context menus
	treeContextMenu  editor.ContextMenu
	treeContextPath  string
	gitContextMenu   editor.ContextMenu
	gitContextEntry  *git.StatusEntry
	gitContextStaged bool
	gitContextPath   string
}

// NewSidebarManager creates a new sidebar manager.
func NewSidebarManager(rootDir string, theme ui.Theme) *SidebarManager {
	return &SidebarManager{
		tree:            filetree.New(rootDir, theme),
		showTree:        true,
		activeTab:       SidebarFiles,
		gitPanel:        git.New(rootDir, theme),
		problemsPanel:   problems.New(theme, rootDir),
		debuggerPanel:   debugger.New(theme),
		showAgent:       false,
		agentPanel:      agent.New(theme),
		treeContextMenu: editor.NewContextMenu(theme),
		gitContextMenu:  editor.NewContextMenu(theme),
	}
}

// GetTree returns the file tree model.
func (sm *SidebarManager) GetTree() *filetree.Model {
	return &sm.tree
}

// ShowTree returns whether the tree is visible.
func (sm *SidebarManager) ShowTree() bool {
	return sm.showTree
}

// ToggleTree toggles tree visibility.
func (sm *SidebarManager) ToggleTree() {
	sm.showTree = !sm.showTree
}

// SetTreeVisible sets tree visibility.
func (sm *SidebarManager) SetTreeVisible(visible bool) {
	sm.showTree = visible
}

// GetActiveTab returns the active sidebar tab.
func (sm *SidebarManager) GetActiveTab() SidebarTab {
	return sm.activeTab
}

// SwitchTab switches to the given sidebar tab.
func (sm *SidebarManager) SwitchTab(tab SidebarTab) {
	sm.activeTab = tab
}

// GetGitPanel returns the git panel model.
func (sm *SidebarManager) GetGitPanel() *git.Model {
	return &sm.gitPanel
}

// GetProblemsPanel returns the problems panel model.
func (sm *SidebarManager) GetProblemsPanel() *problems.Model {
	return &sm.problemsPanel
}

// GetDebuggerPanel returns the debugger panel model.
func (sm *SidebarManager) GetDebuggerPanel() *debugger.Model {
	return &sm.debuggerPanel
}

// ShowAgent returns whether agent panel is visible.
func (sm *SidebarManager) ShowAgent() bool {
	return sm.showAgent
}

// ToggleAgent toggles agent panel visibility.
func (sm *SidebarManager) ToggleAgent() {
	sm.showAgent = !sm.showAgent
}

// GetAgentPanel returns the agent panel model.
func (sm *SidebarManager) GetAgentPanel() *agent.Model {
	return &sm.agentPanel
}

// ShowTreeContextMenu shows the context menu for tree items.
func (sm *SidebarManager) ShowTreeContextMenu(path string, items []editor.ContextMenuItem, x, y int) {
	sm.treeContextPath = path
	sm.treeContextMenu.Show(items, x, y)
}

// HideTreeContextMenu hides the tree context menu.
func (sm *SidebarManager) HideTreeContextMenu() {
	sm.treeContextMenu.Hide()
	sm.treeContextPath = ""
}

// IsTreeContextMenuOpen returns whether tree context menu is open.
func (sm *SidebarManager) IsTreeContextMenuOpen() bool {
	return sm.treeContextMenu.Visible
}

// GetTreeContextPath returns the path for tree context menu.
func (sm *SidebarManager) GetTreeContextPath() string {
	return sm.treeContextPath
}

// ShowGitContextMenu shows the context menu for git items.
func (sm *SidebarManager) ShowGitContextMenu(entry *git.StatusEntry, staged bool, path string, items []editor.ContextMenuItem, x, y int) {
	sm.gitContextEntry = entry
	sm.gitContextStaged = staged
	sm.gitContextPath = path
	sm.gitContextMenu.Show(items, x, y)
}

// HideGitContextMenu hides the git context menu.
func (sm *SidebarManager) HideGitContextMenu() {
	sm.gitContextMenu.Hide()
	sm.gitContextEntry = nil
	sm.gitContextPath = ""
}

// IsGitContextMenuOpen returns whether git context menu is open.
func (sm *SidebarManager) IsGitContextMenuOpen() bool {
	return sm.gitContextMenu.Visible
}

// GetGitContextEntry returns the git entry for context menu.
func (sm *SidebarManager) GetGitContextEntry() *git.StatusEntry {
	return sm.gitContextEntry
}

// IsGitContextStaged returns whether the git context entry is staged.
func (sm *SidebarManager) IsGitContextStaged() bool {
	return sm.gitContextStaged
}

// GetGitContextPath returns the path for git context menu.
func (sm *SidebarManager) GetGitContextPath() string {
	return sm.gitContextPath
}

// GetActivePanel returns the currently active panel based on active tab.
func (sm *SidebarManager) GetActivePanel() interface{} {
	switch sm.activeTab {
	case SidebarFiles:
		return &sm.tree
	case SidebarGit:
		return &sm.gitPanel
	case SidebarProblems:
		return &sm.problemsPanel
	case SidebarDebugger:
		return &sm.debuggerPanel
	default:
		return &sm.tree
	}
}

// Resize updates dimensions for all sidebar components.
func (sm *SidebarManager) Resize(width, height int) {
	sm.tree.SetSize(width, height)
	sm.gitPanel.SetSize(width, height)
	sm.problemsPanel.SetSize(width, height)
	sm.debuggerPanel.SetSize(width, height)
	sm.agentPanel.SetSize(width, height)
}

// Refresh refreshes all sidebar components that need updating.
func (sm *SidebarManager) Refresh() {
	sm.gitPanel.Refresh()
}

// SetSize sets the size for all panels.
func (sm *SidebarManager) SetSize(width, height int) {
	sm.tree.SetSize(width, height)
	sm.gitPanel.SetSize(width, height)
	sm.problemsPanel.SetSize(width, height)
	sm.debuggerPanel.SetSize(width, height)
	sm.agentPanel.SetSize(width, height)
}

// UpdateTreeDiagnostics updates diagnostics in the tree.
func (sm *SidebarManager) UpdateTreeDiagnostics(fileDiags, dirDiags map[string]int) {
	sm.tree.SetDiagnostics(fileDiags)
}

// ClearContextMenus clears all context menus.
func (sm *SidebarManager) ClearContextMenus() {
	sm.HideTreeContextMenu()
	sm.HideGitContextMenu()
}

// IsAnyContextMenuOpen returns whether any context menu is open.
func (sm *SidebarManager) IsAnyContextMenuOpen() bool {
	return sm.treeContextMenu.Visible || sm.gitContextMenu.Visible
}
