package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/agent"
	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/debugger"
	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/filetree"
	"teak/internal/git"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/problems"
	"teak/internal/search"
	"teak/internal/settings"
	"teak/internal/text"
	"teak/internal/ui"
)

// FocusArea indicates which panel has focus.
type FocusArea int

const (
	FocusEditor FocusArea = iota
	FocusTree
	FocusGitPanel
	FocusProblems
	FocusDebugger
	FocusAgent
)

// SidebarTab indicates which tab is active in the sidebar.
type SidebarTab int

const (
	SidebarFiles SidebarTab = iota
	SidebarGit
	SidebarProblems
	SidebarDebugger
)

// Model is the root Bubbletea model.
type Model struct {
	editors          []editor.Editor
	activeTab        int
	tabBar           editor.TabBar
	tree             filetree.Model
	theme            ui.Theme
	status           string
	width            int
	height           int
	showHelp         bool
	helpM            editor.HelpModel
	showTree         bool
	showSearch       bool
	searchMode       search.Mode
	searchM          search.Model
	focus            FocusArea
	rootDir          string
	lspMgr           *lsp.Manager
	goToLineMode     bool
	goToLineInput    string
	welcome          *editor.Welcome
	treeContextMenu  editor.ContextMenu
	treeContextPath  string
	renameMode       bool
	renameInput      string
	pendingCursor    *text.Position        // cursor to set after async file load
	fileDiagnostics  map[string]int        // path → worst severity (1=error, 2=warn, 3=info, 4=hint)
	dirDiagnostics   map[string]int        // dir path → worst child severity
	gitBranch        string                // current git branch name
	gitPanel         git.Model             // git sidebar panel
	watcher          *fileWatcher          // watches files/dirs for external changes
	newFileMode      bool                  // input mode for new file name
	newFolderMode    bool                  // input mode for new folder name
	newItemInput     string                // input buffer for new file/folder name
	newItemDir       string                // directory to create new item in
	deleteConfirm    bool                  // confirming deletion
	deleteTarget     string                // path to delete
	diffViews        map[int]diff.Model    // tab index → diff view model
	sidebarTab       SidebarTab            // active sidebar tab
	showBranchPicker bool                  // branch picker overlay visible
	branchPickerM    git.BranchPickerModel // branch picker model
	gitContextMenu   editor.ContextMenu    // context menu for git panel
	gitContextEntry  *git.StatusEntry      // entry right-clicked in git panel
	gitContextStaged bool                  // whether the right-clicked entry is in staged section
	gitContextPath   string                // path of right-clicked entry (file or dir)
	unsavedConfirm   *overlay.Confirm      // unsaved changes dialog shown on quit
	overlayStack     overlay.Stack         // stack for picker overlays (quick open, command palette)
	cachedFiles      []string              // cached file list for quick open
	cachedFilesReady bool                  // true after file list has been loaded
	problemsPanel    problems.Model        // problems panel for diagnostics
	showSettings     bool                  // settings overlay visible
	settingsM        settings.Model        // settings editor model
	closedTabs       []ClosedTab           // history of closed tabs for reopening
	debuggerPanel    debugger.Model        // debugger panel
	debugMgr         *dap.Manager          // debug session manager
	breakpoints      map[string][]int      // file path → sorted line numbers (0-based)
	currentExecFile  string                // file with current execution point
	currentExecLine  int                   // current execution line (0-based), -1 when not paused
	showAgent        bool                  // agent panel visible
	agentPanel       agent.Model           // agent chat panel
	acpMgr           *acp.Manager          // ACP agent manager
}

// ClosedTab stores information about a closed tab for reopening.
type ClosedTab struct {
	FilePath string
	Label    string
}

// NewModel creates a new app model, optionally loading a file.
func NewModel(filePath string, rootDir string, appCfg config.Config) (Model, error) {
	// Suppress LSP log output from corrupting TUI
	log.SetOutput(io.Discard)

	theme := ui.ThemeByName(appCfg.UI.Theme)
	cfg := editor.Config{
		TabSize:    appCfg.Editor.TabSize,
		InsertTabs: appCfg.Editor.InsertTabs,
		AutoIndent: appCfg.Editor.AutoIndent,
	}
	buf := text.NewBuffer()
	if filePath != "" {
		buf.FilePath = filePath
		cfg.CommentPrefix = editor.CommentPrefixForFile(filePath)
	}

	// Build LSP configs: merge user overrides with defaults
	var lspConfigs []lsp.ServerConfig
	for _, lc := range appCfg.LSP {
		lspConfigs = append(lspConfigs, lsp.ServerConfig{
			Extensions: lc.Extensions,
			Command:    lc.Command,
			Args:       lc.Args,
			LanguageID: lc.LanguageID,
		})
	}

	m := Model{
		theme:           theme,
		rootDir:         rootDir,
		tabBar:          editor.NewTabBar(theme),
		lspMgr:          lsp.NewManager(rootDir, lspConfigs),
		treeContextMenu: editor.NewContextMenu(theme),
		fileDiagnostics: make(map[string]int),
		dirDiagnostics:  make(map[string]int),
		gitBranch:       detectGitBranch(rootDir),
		gitPanel:        git.New(rootDir, theme),
		branchPickerM:   git.NewBranchPicker(theme),
		gitContextMenu:  editor.NewContextMenu(theme),
		helpM:           editor.NewHelpModel(theme),
		problemsPanel:   problems.New(theme, rootDir),
		settingsM:       settings.New(theme, appCfg, config.ConfigPath()),
		debuggerPanel:   debugger.New(theme),
		debugMgr:        dap.NewManager(rootDir),
		breakpoints:     make(map[string][]int),
		currentExecLine: -1,
		agentPanel:      agent.New(theme),
	}

	// Initialize ACP manager if agent is configured
	if appCfg.Agent.Enabled && appCfg.Agent.Command != "" {
		m.acpMgr = acp.NewManager(rootDir, appCfg.Agent.Command, appCfg.Agent.Args)
	}

	if rootDir != "" {
		m.tree = filetree.New(rootDir, theme)
		if w, err := newFileWatcher(rootDir); err == nil {
			m.watcher = w
		}
	}

	// Create initial editor + tab
	ed := editor.New(buf, theme, cfg)
	m.editors = append(m.editors, ed)

	label := "untitled"
	if filePath != "" {
		label = filepath.Base(filePath)
	}
	m.tabBar.AddTab(label, filePath)
	m.activeTab = 0

	// Show tree based on config
	m.showTree = appCfg.UI.ShowTree

	// Show welcome screen when no file is provided
	if filePath == "" {
		w := editor.NewWelcome(theme)
		m.welcome = &w
		m.showTree = true
		m.focus = FocusTree
	}

	return m, nil
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Load initial file content asynchronously
	if len(m.editors) > 0 && m.editors[0].Buffer.FilePath != "" {
		cmds = append(cmds, loadFileCmd(m.editors[0].Buffer.FilePath, 0, false))
	}

	// Start listening for LSP messages
	cmds = append(cmds, m.listenLSP())

	// Initial git panel refresh
	if refreshCmd := m.gitPanel.Refresh(); refreshCmd != nil {
		cmds = append(cmds, refreshCmd)
	}

	// Start file watcher listener
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}

	// Start welcome animation if active
	if m.welcome != nil && m.welcome.Active {
		cmds = append(cmds, m.welcome.Init())
	}

	// Start DAP event listener
	cmds = append(cmds, m.listenDAP())

	// Start ACP agent listener
	if m.acpMgr != nil {
		cmds = append(cmds, m.listenACP(), m.startAgent())
	}

	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil

	case editor.WelcomeTickMsg:
		if m.welcome != nil && m.welcome.Active {
			var cmd tea.Cmd
			m.welcome, cmd = m.welcome.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		// Unsaved changes confirm dialog captures all input when visible
		if m.unsavedConfirm != nil {
			updated, cmd := m.unsavedConfirm.Update(msg)
			if updated.IsDismissed() {
				m.unsavedConfirm = nil
			} else {
				m.unsavedConfirm = updated.(*overlay.Confirm)
			}
			return m, cmd
		}

		// Overlay stack (quick open, command palette) captures all input
		if !m.overlayStack.IsEmpty() {
			cmd := m.overlayStack.Update(msg)
			return m, cmd
		}

		// Branch picker captures all input when visible
		if m.showBranchPicker {
			return m.updateBranchPicker(msg)
		}

		// Search overlay captures all input when visible
		if m.showSearch {
			return m.updateSearch(msg)
		}

		// Go-to-line mode captures all input
		if m.goToLineMode {
			return m.handleGoToLineInput(msg)
		}

		// Rename mode captures all input
		if m.renameMode {
			return m.handleRenameInput(msg)
		}

		// New file/folder mode captures all input
		if m.newFileMode || m.newFolderMode {
			return m.handleNewItemInput(msg)
		}

		// Delete confirmation captures all input
		if m.deleteConfirm {
			return m.handleDeleteConfirm(msg)
		}

		// Tree context menu captures keys
		if m.treeContextMenu.Visible {
			switch msg.String() {
			case "up":
				m.treeContextMenu.MoveUp()
				return m, nil
			case "down":
				m.treeContextMenu.MoveDown()
				return m, nil
			case "enter":
				if item := m.treeContextMenu.Selected(); item != nil {
					action := item.Action
					m.treeContextMenu.Hide()
					return m.handleTreeContextMenuAction(action)
				}
				m.treeContextMenu.Hide()
				return m, nil
			case "esc", "escape":
				m.treeContextMenu.Hide()
				return m, nil
			default:
				m.treeContextMenu.Hide()
				return m, nil
			}
		}

		// Git context menu captures keys
		if m.gitContextMenu.Visible {
			switch msg.String() {
			case "up":
				m.gitContextMenu.MoveUp()
				return m, nil
			case "down":
				m.gitContextMenu.MoveDown()
				return m, nil
			case "enter":
				if item := m.gitContextMenu.Selected(); item != nil {
					action := item.Action
					m.gitContextMenu.Hide()
					return m.handleGitContextMenuAction(action)
				}
				m.gitContextMenu.Hide()
				return m, nil
			case "esc", "escape":
				m.gitContextMenu.Hide()
				return m, nil
			default:
				m.gitContextMenu.Hide()
				return m, nil
			}
		}

		// Help overlay: route input through help model
		if m.showHelp {
			key := msg.String()
			if key == "esc" || key == "escape" || key == "f1" {
				m.showHelp = false
				return m, nil
			}
			var cmd tea.Cmd
			m.helpM, cmd = m.helpM.Update(msg)
			return m, cmd
		}

		// Settings overlay: captures all input when visible
		if m.showSettings {
			return m.updateSettings(msg)
		}

		// Welcome screen: global shortcuts pass through, others dismiss.
		// Don't dismiss if git panel commit inputs have focus.
		gitInputFocused := m.focus == FocusGitPanel && (m.gitPanel.IsTitleFocused() || m.gitPanel.IsBodyFocused())
		if m.welcome != nil && m.welcome.Active && !gitInputFocused {
			key := msg.String()
			switch key {
			case "ctrl+q", "ctrl+b", "ctrl+f", "ctrl+shift+f", "ctrl+h", "f1":
				// Let these fall through to normal handling
			default:
				m.welcome.Dismiss()
				// Let the key fall through to normal handling
			}
		}

		switch msg.String() {
		case "ctrl+q":
			// Check for unsaved files before quitting
			var dirtyNames []string
			for i, ed := range m.editors {
				if ed.Buffer.Dirty() {
					name := filepath.Base(ed.Buffer.FilePath)
					if name == "." || ed.Buffer.FilePath == "" {
						name = m.tabBar.Tabs[i].Label
					}
					dirtyNames = append(dirtyNames, name)
				}
			}
			if len(dirtyNames) > 0 {
				msg := fmt.Sprintf("You have %d unsaved file(s):", len(dirtyNames))
				confirm := overlay.NewConfirm(
					"Unsaved Changes",
					msg,
					dirtyNames,
					[]overlay.Button{
						{Label: "Save All & Quit", Style: lipgloss.NewStyle().Background(ui.Nord14).Foreground(ui.Nord0).Padding(0, 2), Action: SaveAllAndQuitMsg{}},
						{Label: "Quit Without Saving", Style: lipgloss.NewStyle().Background(ui.Nord11).Foreground(ui.Nord6).Padding(0, 2), Action: QuitWithoutSavingMsg{}},
						{Label: "Cancel", Action: overlay.ButtonAction{Label: "Cancel"}},
					},
					m.theme,
				)
				m.unsavedConfirm = confirm
				return m, nil
			}
			m.lspMgr.ShutdownAll()
			return m, tea.Quit
		case "ctrl+s":
			if m.activeEditor() == nil {
				return m, nil
			}
			buf := m.activeEditor().Buffer
			return m, SaveFileCmd(buf.Save, buf.FilePath)
		case "f1":
			m.showHelp = true
			m.helpM = editor.NewHelpModel(m.theme)
			m.helpM.SetSize(m.width, m.height-2)
			cmd := m.helpM.Focus()
			return m, cmd
		case "ctrl+b":
			m.showTree = !m.showTree
			if m.showTree && !m.showHelp {
				m.focus = FocusTree
			} else {
				m.focus = FocusEditor
			}
			m.relayout()
			return m, nil
		case "ctrl+f":
			return m.openSearch(search.ModeText)
		case "ctrl+h":
			return m.openSearchReplace()
		case "ctrl+shift+f":
			return m.openSearch(search.ModeSemantic)
		case "ctrl+space":
			return m, m.requestCompletion()
		case "k":
			// Show hover tooltip (K for Knowledge/Documentation)
			if m.focus == FocusEditor {
				return m, m.requestHover()
			}
			return m, nil
		case "ctrl+k":
			// Code actions (quick fixes)
			if m.focus == FocusEditor {
				return m, m.requestCodeActions()
			}
			return m, nil
		case "f12":
			return m, m.requestDefinition()
		case "ctrl+alt+f":
			// Format document
			if m.focus == FocusEditor {
				return m, m.requestFormatting()
			}
			return m, nil
		case "ctrl+shift+o":
			// Document symbols (outline)
			if m.focus == FocusEditor {
				return m, m.requestDocumentSymbols()
			}
			return m, nil
		case "f5":
			// Start debugging
			if m.activeEditor() != nil && m.activeEditor().Buffer.FilePath != "" {
				program := m.activeEditor().Buffer.FilePath
				config := dap.ConfigForProgram(program)
				if config.Command == "" {
					m.status = "No debugger configured for this file type"
					return m, nil
				}
				if err := m.debugMgr.Start(config); err != nil {
					m.status = fmt.Sprintf("Debug error: %v", err)
					return m, nil
				}
				if err := m.debugMgr.Launch(); err != nil {
					m.status = fmt.Sprintf("Launch error: %v", err)
					return m, nil
				}
				m.debuggerPanel.SetState(dap.StateRunning)
				m.showTree = true
				m.sidebarTab = SidebarDebugger
				m.focus = FocusDebugger
				m.status = "Debugging started"
				m.relayout()
			}
			return m, nil
		case "shift+f5":
			// Stop debugging
			if m.debugMgr.IsRunning() {
				m.debugMgr.Stop()
				m.debuggerPanel.SetState(dap.StateInactive)
				m.currentExecFile = ""
				m.currentExecLine = -1
				m.status = "Debugging stopped"
			}
			return m, nil
		case "f9":
			// Toggle breakpoint on current line
			if ed := m.activeEditor(); ed != nil && ed.Buffer.FilePath != "" {
				cmd := m.toggleBreakpoint(ed.Buffer.FilePath, ed.Buffer.Cursor.Line)
				return m, cmd
			}
			return m, nil
		case "ctrl+w":
			return m.closeCurrentTab()
		case "ctrl+shift+t":
			// Reopen last closed tab
			if len(m.closedTabs) > 0 {
				lastClosed := m.closedTabs[len(m.closedTabs)-1]
				m.closedTabs = m.closedTabs[:len(m.closedTabs)-1]
				return m.openFilePinned(lastClosed.FilePath)
			}
			return m, nil
		case "ctrl+g":
			m.goToLineMode = true
			m.goToLineInput = ""
			return m, nil
		case "ctrl+shift+g":
			if m.gitPanel.IsGitRepo() {
				m.showTree = true
				m.sidebarTab = SidebarGit
				m.focus = FocusGitPanel
				m.relayout()
			}
			return m, nil
		case "ctrl+p":
			return m.openQuickOpen()
		case "ctrl+shift+p":
			return m.openCommandPalette()
		case "ctrl+tab":
			if len(m.editors) > 1 {
				m.activeTab = (m.activeTab + 1) % len(m.editors)
				m.tabBar.ActiveIdx = m.activeTab
			}
			return m, nil
		case "ctrl+shift+tab":
			if len(m.editors) > 1 {
				m.activeTab = (m.activeTab - 1 + len(m.editors)) % len(m.editors)
				m.tabBar.ActiveIdx = m.activeTab
			}
			return m, nil
		case "ctrl+j":
			cmd := m.toggleAgentPanel()
			return m, cmd
		case "ctrl+'":
			if m.showAgent {
				if m.focus == FocusAgent {
					m.focus = FocusEditor
					m.agentPanel.Blur()
				} else {
					m.focus = FocusAgent
					return m, m.agentPanel.Focus()
				}
			}
			return m, nil
		case "ctrl+,":
			// Open settings
			m.showSettings = true
			m.settingsM.SetSize(m.width, m.height-4)
			return m, nil
		case "f8":
			// Navigate to next problem
			if m.problemsPanel.ProblemCount() > 0 {
				m.problemsPanel.SelectNext()
				if prob := m.problemsPanel.SelectedProblem(); prob != nil {
					pos := text.Position{Line: prob.Line, Col: prob.Col}
					m.pendingCursor = &pos
					model, cmd := m.openFile(prob.FilePath)
					m2 := model.(Model)
					m2.status = fmt.Sprintf("Problem %d/%d", m2.problemsPanel.SelectedIndex()+1, m2.problemsPanel.ProblemCount())
					return m2, cmd
				}
			}
			return m, nil
		case "shift+f8":
			// Navigate to previous problem
			if m.problemsPanel.ProblemCount() > 0 {
				m.problemsPanel.SelectPrev()
				if prob := m.problemsPanel.SelectedProblem(); prob != nil {
					pos := text.Position{Line: prob.Line, Col: prob.Col}
					m.pendingCursor = &pos
					model, cmd := m.openFile(prob.FilePath)
					m2 := model.(Model)
					m2.status = fmt.Sprintf("Problem %d/%d", m2.problemsPanel.SelectedIndex()+1, m2.problemsPanel.ProblemCount())
					return m2, cmd
				}
			}
			return m, nil
		}

	case tea.MouseClickMsg:
		// Unsaved changes dialog captures all mouse clicks when visible
		if m.unsavedConfirm != nil {
			updated, cmd := m.unsavedConfirm.Update(msg)
			if updated.IsDismissed() {
				m.unsavedConfirm = nil
			} else {
				m.unsavedConfirm = updated.(*overlay.Confirm)
			}
			return m, cmd
		}

		// Overlay stack captures clicks when active
		if !m.overlayStack.IsEmpty() {
			cmd := m.overlayStack.Update(msg)
			return m, cmd
		}

		// Branch picker captures clicks when visible
		if m.showBranchPicker {
			return m.updateBranchPicker(msg)
		}

		// Search overlay captures all mouse clicks when visible
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

		// Help overlay: forward mouse events
		if m.showHelp {
			var cmd tea.Cmd
			m.helpM, cmd = m.helpM.Update(msg)
			return m, cmd
		}

		// Handle clicks on tree context menu
		if m.treeContextMenu.Visible {
			mouse0 := msg.Mouse()
			if mouse0.Button == tea.MouseLeft {
				// Account for the border (1 line top border from RoundedBorder)
				relY := mouse0.Y - m.treeContextMenu.Y - 1
				if item := m.treeContextMenu.SelectAt(relY); item != nil {
					action := item.Action
					m.treeContextMenu.Hide()
					return m.handleTreeContextMenuAction(action)
				}
			}
			m.treeContextMenu.Hide()
			return m, nil
		}

		// Handle clicks on git context menu
		if m.gitContextMenu.Visible {
			mouse0 := msg.Mouse()
			if mouse0.Button == tea.MouseLeft {
				relY := mouse0.Y - m.gitContextMenu.Y - 1
				if item := m.gitContextMenu.SelectAt(relY); item != nil {
					action := item.Action
					m.gitContextMenu.Hide()
					return m.handleGitContextMenuAction(action)
				}
			}
			m.gitContextMenu.Hide()
			return m, nil
		}

		// Handle clicks on editor context menu
		mouse0 := msg.Mouse()
		if mouse0.Button == tea.MouseLeft && m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible() {
			_, cmY := m.activeEditor().ContextMenuPosition()
			cmY += 1 // +1 for tab bar
			// Account for the border (1 line top border from RoundedBorder)
			relY := mouse0.Y - cmY - 1
			ed := m.editors[m.activeTab]
			result, cmd, action := ed.ClickContextMenuItem(relY)
			m.editors[m.activeTab] = result
			if action == "goto_definition" || action == "find_references" || action == "rename_symbol" {
				return m.handleContextMenuAction(action)
			}
			return m, cmd
		}

		// Dismiss welcome on click in editor area
		if m.welcome != nil && m.welcome.Active {
			mouse := msg.Mouse()
			editorStartX := 0
			if m.showTree {
				editorStartX = m.treeWidth() + 1
			}
			if mouse.X >= editorStartX {
				m.welcome.Dismiss()
			}
		}

		mouse := msg.Mouse()

		// Status bar branch click → open branch picker
		if zone.Get("status-bar-branch").InBounds(msg) && m.gitPanel.IsGitRepo() {
			m.showBranchPicker = true
			m.branchPickerM.SetSize(m.width, m.height)
			return m, tea.Batch(
				git.ListBranchesCmd(m.gitPanel.RootDir()),
				m.branchPickerM.Focus(),
			)
		}

		// Agent panel click detection
		if m.showAgent && m.agentPanelWidth() > 0 {
			agentStartX := m.width - m.agentPanelWidth()
			if mouse.X >= agentStartX {
				m.focus = FocusAgent
				mouse.X -= agentStartX
				adjusted := tea.MouseClickMsg(mouse)
				var cmd tea.Cmd
				m.agentPanel, cmd = m.agentPanel.Update(adjusted)
				return m, cmd
			}
		}

		if m.showTree {
			treeWidth := m.treeWidth()
			if mouse.X < treeWidth {
				// Y==0 is the sidebar tab bar
				if mouse.Y == 0 {
					// Check which sidebar tab was clicked
					if zone.Get("sidebar-tab-files").InBounds(msg) {
						m.sidebarTab = SidebarFiles
						m.focus = FocusTree
					} else if zone.Get("sidebar-tab-git").InBounds(msg) {
						m.sidebarTab = SidebarGit
						m.focus = FocusGitPanel
					} else if zone.Get("sidebar-tab-problems").InBounds(msg) {
						m.sidebarTab = SidebarProblems
						m.focus = FocusProblems
					} else if zone.Get("sidebar-tab-debugger").InBounds(msg) {
						m.sidebarTab = SidebarDebugger
						m.focus = FocusDebugger
					}
					return m, nil
				}
				// Y>0: forward to active sidebar panel with Y adjusted by -1
				if m.sidebarTab == SidebarGit {
					m.focus = FocusGitPanel
					if mouse.Button == tea.MouseRight {
						return m.showGitContextMenu(mouse.X, mouse.Y, mouse.Y-1)
					}
					// Pass original msg for zone checks, adjusted Y for positional logic
					return m.handleGitPanelClick(mouse.Y-1, msg)
				}
				mouse.Y -= 1
				// File tree
				if mouse.Button == tea.MouseRight {
					return m.showTreeContextMenu(mouse.X, mouse.Y+1) // +1 to restore absolute Y for context menu
				}
				m.focus = FocusTree
				adjusted := tea.MouseClickMsg(mouse)
				var cmd tea.Cmd
				m.tree, cmd = m.tree.Update(adjusted)
				return m, cmd
			} else {
				m.focus = FocusEditor
				// Editor area — check tab bar click (Y==0 in editor column)
				// Use original msg for zone.InBounds (zones are at absolute positions)
				if mouse.Y == 0 {
					return m.handleTabBarClick(msg)
				}
				// Adjust for tab bar + tree offset and forward to editor
				mouse.X -= treeWidth + 1
				mouse.Y -= 1
				adjusted := tea.MouseClickMsg(mouse)
				return m.forwardToEditor(adjusted)
			}
		} else {
			// No tree — check tab bar click (Y==0)
			if mouse.Y == 0 {
				return m.handleTabBarClick(msg)
			}
			// Adjust Y for tab bar and forward to editor
			mouse.Y -= 1
			adjusted := tea.MouseClickMsg(mouse)
			return m.forwardToEditor(adjusted)
		}

	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		if m.showTree {
			treeWidth := m.treeWidth()
			if mouse.X >= treeWidth+1 {
				mouse.X -= treeWidth + 1
				mouse.Y -= 1
				adjusted := tea.MouseMotionMsg(mouse)
				return m.forwardToEditor(adjusted)
			}
		} else {
			mouse.Y -= 1
			adjusted := tea.MouseMotionMsg(mouse)
			return m.forwardToEditor(adjusted)
		}

	case tea.MouseWheelMsg:
		// Overlay stack captures scroll when active
		if !m.overlayStack.IsEmpty() {
			cmd := m.overlayStack.Update(msg)
			return m, cmd
		}
		if m.showSearch {
			return m.updateSearch(msg)
		}
		if m.showHelp {
			var cmd tea.Cmd
			m.helpM, cmd = m.helpM.Update(msg)
			return m, cmd
		}
		mouse := msg.Mouse()
		// Agent panel scroll
		if m.showAgent && m.agentPanelWidth() > 0 {
			agentStartX := m.width - m.agentPanelWidth()
			if mouse.X >= agentStartX {
				mouse.X -= agentStartX
				adjusted := tea.MouseWheelMsg(mouse)
				var cmd tea.Cmd
				m.agentPanel, cmd = m.agentPanel.Update(adjusted)
				return m, cmd
			}
		}
		if m.showTree {
			treeWidth := m.treeWidth()
			if mouse.X < treeWidth {
				// Route to active sidebar panel (skip tab bar row)
				mouse.Y -= 1
				if m.sidebarTab == SidebarGit {
					adjusted := tea.MouseWheelMsg(mouse)
					var cmd tea.Cmd
					m.gitPanel, cmd = m.gitPanel.Update(adjusted)
					return m, cmd
				}
				adjusted := tea.MouseWheelMsg(mouse)
				var cmd tea.Cmd
				m.tree, cmd = m.tree.Update(adjusted)
				return m, cmd
			}
			if mouse.X >= treeWidth+1 {
				mouse.X -= treeWidth + 1
				mouse.Y -= 1
				adjusted := tea.MouseWheelMsg(mouse)
				return m.forwardToEditor(adjusted)
			}
		} else {
			mouse.Y -= 1
			adjusted := tea.MouseWheelMsg(mouse)
			return m.forwardToEditor(adjusted)
		}

	case filetree.DirExpandedMsg:
		var cmd tea.Cmd
		m.tree, cmd = m.tree.Update(msg)
		return m, cmd

	case filetree.OpenFileMsg:
		return m.openFile(msg.Path)

	case filetree.PinFileMsg:
		return m.openFilePinned(msg.Path)

	case search.OpenResultMsg:
		m.showSearch = false
		filePath := msg.FilePath
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(m.rootDir, filePath)
		}
		pos := text.Position{Line: msg.Line, Col: msg.Col}
		m.pendingCursor = &pos
		return m.openFilePinned(filePath)

	case search.CloseSearchMsg:
		m.showSearch = false
		return m, nil

	case search.ReplaceOneMsg:
		ed := m.activeEditor()
		if ed == nil {
			return m, nil
		}
		content := ed.Buffer.Content()
		cursor := ed.Buffer.Cursor
		cursorOff := ed.Buffer.Rope().PositionToOffset(cursor)
		idx := strings.Index(content[cursorOff:], msg.Query)
		if idx < 0 {
			// Wrap around: search from beginning
			idx = strings.Index(content, msg.Query)
			if idx < 0 {
				return m, nil
			}
		} else {
			idx += cursorOff
		}
		startPos := ed.Buffer.Rope().OffsetToPosition(idx)
		endPos := ed.Buffer.Rope().OffsetToPosition(idx + len(msg.Query))
		ed.Buffer.ReplaceRange(startPos, endPos, []byte(msg.Replacement))
		ed.Buffer.Cursor = ed.Buffer.Rope().OffsetToPosition(idx + len(msg.Replacement))
		version := ed.Buffer.Version()
		return m, func() tea.Msg {
			return editor.RetokenizeMsg{Version: version}
		}

	case search.ReplaceAllMsg:
		ed := m.activeEditor()
		if ed == nil {
			return m, nil
		}
		content := ed.Buffer.Content()
		if !strings.Contains(content, msg.Query) {
			return m, nil
		}
		// Find all matches and replace in reverse order to preserve offsets
		var offsets []int
		searchFrom := 0
		for {
			idx := strings.Index(content[searchFrom:], msg.Query)
			if idx < 0 {
				break
			}
			offsets = append(offsets, searchFrom+idx)
			searchFrom += idx + len(msg.Query)
		}
		for i := len(offsets) - 1; i >= 0; i-- {
			startPos := ed.Buffer.Rope().OffsetToPosition(offsets[i])
			endPos := ed.Buffer.Rope().OffsetToPosition(offsets[i] + len(msg.Query))
			ed.Buffer.ReplaceRange(startPos, endPos, []byte(msg.Replacement))
		}
		version := ed.Buffer.Version()
		return m, func() tea.Msg {
			return editor.RetokenizeMsg{Version: version}
		}

	case search.SearchIndexingMsg:
		if m.showSearch {
			var cmd tea.Cmd
			m.searchM, cmd = m.searchM.Update(msg)
			return m, cmd
		}
		return m, nil

	case search.SearchResultsMsg:
		if m.showSearch {
			var cmd tea.Cmd
			m.searchM, cmd = m.searchM.Update(msg)
			return m, cmd
		}
		return m, nil

	case spinner.TickMsg:
		var cmds []tea.Cmd
		if m.showSearch {
			var cmd tea.Cmd
			m.searchM, cmd = m.searchM.Update(msg)
			cmds = append(cmds, cmd)
		}
		if m.gitPanel.IsSpinning() {
			var cmd tea.Cmd
			m.gitPanel, cmd = m.gitPanel.Update(msg)
			cmds = append(cmds, cmd)
		}
		if m.showAgent && m.agentPanel.IsLoading() {
			var cmd tea.Cmd
			m.agentPanel, cmd = m.agentPanel.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case SwitchTabMsg:
		if msg.Index >= 0 && msg.Index < len(m.editors) {
			m.activeTab = msg.Index
			m.tabBar.ActiveIdx = msg.Index
		}
		return m, nil

	case CloseTabMsg:
		return m.closeTab(msg.Index)

	case editor.RetokenizeMsg:
		if m.activeEditor() == nil {
			return m, nil
		}
		ed := m.activeEditor()
		updated, cmd := ed.Update(msg)
		m.editors[m.activeTab] = updated
		return m, cmd

	case editor.TokenizeCompleteMsg:
		if m.activeEditor() == nil {
			return m, nil
		}
		ed := m.activeEditor()
		updated, cmd := ed.Update(msg)
		m.editors[m.activeTab] = updated
		return m, cmd

	case editor.RequestCompletionCmd:
		return m, m.requestCompletion()

	case FileSavedMsg:
		m.status = fmt.Sprintf("Saved %s", msg.Path)
		if m.activeTab < len(m.tabBar.Tabs) {
			m.tabBar.Tabs[m.activeTab].Dirty = false
		}
		var cmds []tea.Cmd
		if m.activeEditor() != nil {
			buf := m.activeEditor().Buffer
			if buf.FilePath != "" {
				if client := m.lspMgr.ClientForFile(buf.FilePath); client != nil {
					client.DidSave(lsp.FileURI(buf.FilePath))
				}
			}
		}
		// Refresh git panel after save
		if refreshCmd := m.gitPanel.Refresh(); refreshCmd != nil {
			cmds = append(cmds, refreshCmd)
		}
		return m, tea.Batch(cmds...)

	case SaveAllAndQuitMsg:
		var saveCmds []tea.Cmd
		for i := range m.editors {
			if m.editors[i].Buffer.Dirty() && m.editors[i].Buffer.FilePath != "" {
				saveCmds = append(saveCmds, SaveFileCmd(m.editors[i].Buffer.Save, m.editors[i].Buffer.FilePath))
			}
		}
		m.lspMgr.ShutdownAll()
		saveCmds = append(saveCmds, tea.Quit)
		return m, tea.Batch(saveCmds...)

	case QuitWithoutSavingMsg:
		m.lspMgr.ShutdownAll()
		return m, tea.Quit

	case overlay.PickerSelectMsg:
		m.overlayStack.Clear()
		item := msg.Item
		// Agent model picker
		if sel, ok := item.Value.(agentModelPickerSelectMsg); ok {
			if m.acpMgr != nil {
				return m, m.acpMgr.SetModel(sdk.ModelId(sel.ModelId))
			}
			return m, nil
		}
		// Agent file picker
		if sel, ok := item.Value.(agentFilePickerSelectMsg); ok {
			absPath := filepath.Join(m.rootDir, sel.Path)
			m.agentPanel.AddTaggedFile(absPath)
			return m, nil
		}
		// LSP location picker (go-to-definition / references)
		if sel, ok := item.Value.(lspLocationPickerMsg); ok {
			loc := sel.Location
			path := lsp.URIToPath(loc.URI)
			pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
			m.pendingCursor = &pos
			return m.openFilePinned(path)
		}
		// LSP symbol picker
		if sel, ok := item.Value.(lspSymbolPickerMsg); ok {
			ed := m.activeEditor()
			if ed != nil {
				ed.Buffer.Cursor.Line = sel.Symbol.SelectionRange.Start.Line
				ed.Buffer.Cursor.Col = sel.Symbol.SelectionRange.Start.Character
				ed.Viewport.EnsureCursorVisible(ed.Buffer.Cursor, ed.Buffer.LineCount())
				m.editors[m.activeTab] = *ed
			}
			return m, nil
		}
		// Quick Open: item.Value is a relative file path string
		if relPath, ok := item.Value.(string); ok {
			absPath := filepath.Join(m.rootDir, relPath)
			return m.openFilePinned(absPath)
		}
		// Command Palette: item.Value is a Command struct
		if cmd, ok := item.Value.(Command); ok {
			resultMsg := cmd.Execute()
			return m.Update(resultMsg)
		}
		return m, nil

	case overlay.PickerCloseMsg:
		m.overlayStack.Clear()
		return m, nil

	case FileListMsg:
		m.cachedFiles = msg.Files
		m.cachedFilesReady = true
		// If quick open picker is showing, update its items
		if !m.overlayStack.IsEmpty() {
			if picker, ok := m.overlayStack.Top().(*overlay.Picker); ok {
				picker.SetItems(filesToPickerItems(m.cachedFiles))
			}
		}
		return m, nil

	case commandPaletteMsg:
		return m.handleCommandPaletteAction(msg.inner)

	case git.RefreshMsg:
		var cmd tea.Cmd
		m.gitPanel, cmd = m.gitPanel.Update(msg)
		if msg.Err != nil {
			m.status = fmt.Sprintf("Git error: %v", msg.Err)
			return m, cmd
		}
		// Also update the status bar branch display
		if msg.Branch != "" {
			m.gitBranch = msg.Branch
		}
		// Update file tree git status indicators
		if msg.Err == nil {
			gitStatusMap := make(map[string]string)
			for _, e := range msg.Entries {
				// Use the most visible status (unstaged > staged)
				if e.IsUnstagedChange() {
					gitStatusMap[e.Path] = e.DisplayStatus(false)
				} else if e.IsStagedChange() {
					gitStatusMap[e.Path] = e.DisplayStatus(true)
				}
			}
			m.tree.SetGitStatus(gitStatusMap)
		}
		return m, cmd

	case git.OpenDiffMsg:
		return m.openDiff(msg.Path, msg.Status)

	case git.CommitResultMsg:
		m.gitPanel.StopSpinner()
		if msg.Err != nil {
			m.status = fmt.Sprintf("Commit failed: %v", msg.Err)
		} else {
			m.status = "Committed successfully"
		}
		return m, m.gitPanel.Refresh()

	case git.PushResultMsg:
		m.gitPanel.StopSpinner()
		if msg.Err != nil {
			m.status = fmt.Sprintf("Push failed: %v", msg.Err)
		} else {
			m.status = "Pushed successfully"
		}
		return m, m.gitPanel.Refresh()

	case git.PullResultMsg:
		m.gitPanel.StopSpinner()
		if msg.Err != nil {
			m.status = fmt.Sprintf("Pull failed: %v", msg.Err)
		} else {
			m.status = "Pulled successfully"
		}
		return m, m.gitPanel.Refresh()

	case git.OpenBranchPickerMsg:
		m.showBranchPicker = true
		m.branchPickerM.SetSize(m.width, m.height)
		return m, tea.Batch(
			git.ListBranchesCmd(m.gitPanel.RootDir()),
			m.branchPickerM.Focus(),
		)

	case git.BranchListMsg:
		if msg.Err == nil {
			m.branchPickerM.SetBranches(msg.Branches, msg.Current)
		}
		return m, nil

	case git.SwitchBranchMsg:
		m.showBranchPicker = false
		return m, git.SwitchBranchCmd(m.gitPanel.RootDir(), msg.Branch)

	case git.SwitchBranchResultMsg:
		if msg.Err != nil {
			m.status = fmt.Sprintf("Switch failed: %v", msg.Err)
		} else {
			m.gitBranch = msg.Branch
			m.status = fmt.Sprintf("Switched to %s", msg.Branch)
		}
		return m, m.gitPanel.Refresh()

	case git.CloseBranchPickerMsg:
		m.showBranchPicker = false
		return m, nil

	case DiffLoadedMsg:
		return m.handleDiffLoaded(msg)

	case FileErrorMsg:
		m.status = fmt.Sprintf("Error: %v", msg.Err)
		return m, nil

	case FileLoadedMsg:
		return m.handleFileLoaded(msg)

	case FileLoadErrorMsg:
		m.status = fmt.Sprintf("Error loading %s: %v", filepath.Base(msg.Path), msg.Err)
		return m, nil

	case FileChangedMsg:
		return m.handleExternalFileChange(msg)

	case TreeChangedMsg:
		return m.handleTreeChange(msg)

	case lsp.DiagnosticsMsg:
		return m.handleDiagnostics(msg)

	case lsp.CompletionResultMsg:
		items := make([]editor.AutocompleteItem, len(msg.Items))
		for i, item := range msg.Items {
			items[i] = editor.AutocompleteItem{
				Label:      item.Label,
				Detail:     item.Detail,
				InsertText: item.InsertText,
			}
		}
		if m.activeEditor() != nil {
			m.activeEditor().ShowAutocomplete(items)
			m.editors[m.activeTab] = *m.activeEditor()
		}
		return m, nil

	case lsp.HoverResultMsg:
		if msg.Content != "" && m.activeEditor() != nil {
			m.activeEditor().ShowHover(msg.Content)
			m.editors[m.activeTab] = *m.activeEditor()
		}
		return m, nil

	case lsp.SignatureHelpResultMsg:
		if msg.Help != nil && m.activeEditor() != nil {
			// Convert to editor.SignatureData
			sigData := &editor.SignatureData{
				ActiveSignature: msg.Help.ActiveSignature,
				ActiveParameter: msg.Help.ActiveParameter,
			}
			for _, sig := range msg.Help.Signatures {
				var params []editor.ParameterInfo
				for _, p := range sig.Parameters {
					label := ""
					switch v := p.Label.(type) {
					case string:
						label = v
					case []any:
						if len(v) >= 2 {
							label = sig.Label
						}
					}
					params = append(params, editor.ParameterInfo{
						Label:         label,
						Documentation: p.Documentation,
					})
				}
				sigData.Signatures = append(sigData.Signatures, editor.SignatureInfo{
					Label:         sig.Label,
					Documentation: sig.Documentation,
					Parameters:    params,
				})
			}
			m.activeEditor().ShowSignatureHelp(sigData)
			m.editors[m.activeTab] = *m.activeEditor()
		}
		return m, nil

	case lsp.FormatResultMsg:
		if len(msg.Edits) > 0 && m.activeEditor() != nil {
			buf := m.activeEditor().Buffer
			// Apply edits in reverse order
			for i := len(msg.Edits) - 1; i >= 0; i-- {
				ed := msg.Edits[i]
				start := text.Position{Line: ed.StartLine, Col: ed.StartCol}
				end := text.Position{Line: ed.EndLine, Col: ed.EndCol}
				buf.ReplaceRange(start, end, []byte(ed.NewText))
			}
			m.status = "Document formatted"
			m.editors[m.activeTab] = *m.activeEditor()
		}
		return m, nil

	case lsp.CodeActionResultMsg:
		if len(msg.Actions) > 0 {
			// For now, apply the first action's edit if available
			action := msg.Actions[0]
			if action.Edit != nil && action.Edit.Changes != nil {
				model, cmd := m.applyRenameEdits(action.Edit.Changes)
				m2 := model.(Model)
				m2.status = fmt.Sprintf("Applied: %s", action.Title)
				return m2, cmd
			}
			m.status = fmt.Sprintf("Code action: %s (no edit available)", action.Title)
		}
		return m, nil

	case lsp.DocumentSymbolResultMsg:
		if len(msg.Symbols) > 0 {
			items := lspSymbolsToPickerItems(msg.Symbols)
			picker := overlay.NewPicker(fmt.Sprintf("Document Symbols (%d)", len(msg.Symbols)), items, m.theme, "lsp-sym")
			m.overlayStack.Push(picker)
			return m, picker.Focus()
		}
		m.status = "No symbols found"
		return m, nil

	case lsp.DefinitionResultMsg:
		if len(msg.Locations) == 1 {
			loc := msg.Locations[0]
			path := lsp.URIToPath(loc.URI)
			pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
			m.pendingCursor = &pos
			return m.openFilePinned(path)
		} else if len(msg.Locations) > 1 {
			items := lspLocationsToPickerItems(msg.Locations, m.rootDir)
			picker := overlay.NewPicker("Go to Definition", items, m.theme, "lsp-def")
			m.overlayStack.Push(picker)
			return m, picker.Focus()
		}
		m.status = "No definition found"
		return m, nil

	case lsp.ReferencesResultMsg:
		if len(msg.Locations) == 1 {
			loc := msg.Locations[0]
			path := lsp.URIToPath(loc.URI)
			pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
			m.pendingCursor = &pos
			model, cmd := m.openFile(path)
			m2 := model.(Model)
			m2.status = "Found 1 reference"
			return m2, cmd
		} else if len(msg.Locations) > 1 {
			items := lspLocationsToPickerItems(msg.Locations, m.rootDir)
			picker := overlay.NewPicker(fmt.Sprintf("References (%d)", len(msg.Locations)), items, m.theme, "lsp-refs")
			m.overlayStack.Push(picker)
			return m, picker.Focus()
		}
		m.status = "No references found"
		return m, nil

	case lsp.RenameResultMsg:
		return m.applyRenameEdits(msg.Edits)

	case editor.ContextMenuActionMsg:
		return m.handleContextMenuAction(msg.Action)

	case LspReadyMsg:
		// LSP finished initializing — trigger a re-render so indicator updates
		return m, nil

	case lsp.LspErrorMsg:
		m.status = fmt.Sprintf("LSP error [%s]: %s (code %d)", msg.Method, msg.Message, msg.Code)
		return m, nil

	case lsp.LspShowMessageMsg:
		// Display server message in status bar
		prefix := ""
		switch msg.Type {
		case 1:
			prefix = "Error: "
		case 2:
			prefix = "Warning: "
		case 3:
			prefix = "Info: "
		}
		m.status = prefix + msg.Message
		return m, nil

	case lsp.LspProgressMsg:
		// Progress reporting - can be extended to show in UI
		// For now, just log it
		return m, nil

	case lspMsg:
		if msg.msg == nil {
			return m, m.listenLSP()
		}
		result, cmd := m.Update(msg.msg)
		m = result.(Model)
		return m, tea.Batch(cmd, m.listenLSP())

	case acpMsg:
		return m.handleACPMsg(msg)

	case dapMsg:
		return m.handleDAPMsg(msg)

	case debugStateMsg:
		m.debuggerPanel.SetStackFrames(msg.Frames)
		m.debuggerPanel.SetVariables(msg.Variables)
		if len(msg.Frames) > 0 {
			frame := msg.Frames[0]
			if frame.Source.Path != "" {
				m.currentExecFile = frame.Source.Path
				m.currentExecLine = frame.Line - 1 // DAP is 1-based, we use 0-based
			}
		}
		return m, nil

	case debugger.JumpToFrameMsg:
		// Open the file and jump to the line
		if msg.FilePath != "" {
			pos := &text.Position{Line: msg.Line, Col: 0}
			m.pendingCursor = pos
			return m, loadFileCmd(msg.FilePath, -1, false)
		}
		return m, nil

	case acp.AgentModelChangedMsg:
		m.agentPanel, _ = m.agentPanel.Update(acp.AgentSessionInfoMsg{
			Models:       m.agentPanel.AvailableModels(),
			CurrentModel: msg.ModelId,
		})
		return m, nil

	case agent.CancelRequestedMsg:
		if m.acpMgr != nil {
			m.acpMgr.Cancel()
			m.agentPanel.AddSystemMessage("Cancelled.")
		}
		return m, nil

	case toggleAgentMsg:
		cmd := m.toggleAgentPanel()
		return m, cmd

	case focusAgentMsg:
		if m.showAgent {
			if m.focus == FocusAgent {
				m.focus = FocusEditor
				m.agentPanel.Blur()
			} else {
				m.focus = FocusAgent
				return m, m.agentPanel.Focus()
			}
		}
		return m, nil

	case agentCancelMsg:
		if m.acpMgr != nil {
			m.acpMgr.Cancel()
		}
		return m, nil
	}

	// Route input to agent panel when focused
	if m.showAgent && m.focus == FocusAgent {
		if kp, ok := msg.(tea.KeyPressMsg); ok {
			key := kp.String()
			switch key {
			case "esc", "escape":
				m.focus = FocusEditor
				m.agentPanel.Blur()
				return m, nil
			case "enter":
				newM, cmd, handled := m.handleAgentEnter()
				if handled {
					return newM, cmd
				}
				// Not a slash command — let panel add user message, then send prompt
				text := strings.TrimSpace(m.agentPanel.InputValue())
				if text != "" {
					var panelCmd tea.Cmd
					m.agentPanel, panelCmd = m.agentPanel.Update(kp)
					promptCmd := m.sendAgentPrompt(text)
					return m, tea.Batch(panelCmd, promptCmd)
				}
				return m, nil
			case "ctrl+c":
				if m.acpMgr != nil {
					m.acpMgr.Cancel()
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.agentPanel, cmd = m.agentPanel.Update(kp)
				return m, cmd
			}
		}
		if wm, ok := msg.(tea.MouseWheelMsg); ok {
			var cmd tea.Cmd
			m.agentPanel, cmd = m.agentPanel.Update(wm)
			return m, cmd
		}
	}

	// Route input to focused panel
	if m.showTree && m.focus == FocusTree {
		// Tab switches between sidebar tabs
		if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "tab" {
			if m.sidebarTab == SidebarFiles {
				m.sidebarTab = SidebarGit
				m.focus = FocusGitPanel
			} else if m.sidebarTab == SidebarGit {
				m.sidebarTab = SidebarProblems
				m.focus = FocusProblems
			} else if m.sidebarTab == SidebarProblems {
				m.sidebarTab = SidebarDebugger
				m.focus = FocusDebugger
			} else {
				m.sidebarTab = SidebarFiles
				m.focus = FocusTree
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.tree, cmd = m.tree.Update(msg)
		return m, cmd
	}
	if m.focus == FocusGitPanel {
		var cmd tea.Cmd
		m.gitPanel, cmd = m.gitPanel.Update(msg)
		return m, cmd
	}
	if m.focus == FocusProblems {
		return m.updateProblems(msg)
	}
	if m.focus == FocusDebugger {
		return m.updateDebugger(msg)
	}

	// Route to diff view if active tab is a diff tab
	if m.isActiveDiffTab() {
		if dv, ok := m.diffViews[m.activeTab]; ok {
			var cmd tea.Cmd
			dv, cmd = dv.Update(msg)
			m.diffViews[m.activeTab] = dv
			return m, cmd
		}
		return m, nil
	}

	if m.activeEditor() == nil {
		return m, nil
	}

	var cmd tea.Cmd
	ed := *m.activeEditor()
	// Keep HasLSP up to date
	if ed.Buffer.FilePath != "" {
		ed.HasLSP = m.lspMgr.ClientForFile(ed.Buffer.FilePath) != nil
	}
	prevVersion := ed.Buffer.Version()
	ed, cmd = ed.Update(msg)
	m.editors[m.activeTab] = ed

	// Update tab dirty state; edits pin preview tabs
	if m.activeTab < len(m.tabBar.Tabs) {
		m.tabBar.Tabs[m.activeTab].Dirty = ed.Buffer.Dirty()
		if ed.Buffer.Dirty() && m.tabBar.Tabs[m.activeTab].Preview {
			m.tabBar.Tabs[m.activeTab].Preview = false
		}
	}

	// Notify LSP of changes
	if ed.Buffer.Version() != prevVersion && ed.Buffer.FilePath != "" {
		if client := m.lspMgr.ClientForFile(ed.Buffer.FilePath); client != nil {
			m.notifyLSPChange(client, &ed)
		}
	}

	return m, cmd
}

// notifyLSPChange sends a didChange notification using incremental sync if
// the server supports it and the buffer has change info, otherwise full sync.
func (m *Model) notifyLSPChange(client *lsp.Client, ed *editor.Editor) {
	uri := lsp.FileURI(ed.Buffer.FilePath)
	version := ed.Buffer.Version()

	if change := ed.Buffer.LastChange(); change != nil && client.GetSyncKind() == lsp.SyncIncremental {
		client.DidChangeIncremental(uri, version,
			change.StartLine, change.StartCol,
			change.EndLine, change.EndCol,
			change.Text,
		)
		return
	}

	client.DidChange(uri, version, ed.Buffer.Content())
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	// Set debug gutter state on active editor
	if ed := m.activeEditor(); ed != nil {
		filePath := ed.Buffer.FilePath
		bpLines := m.breakpoints[filePath]
		if len(bpLines) > 0 || m.currentExecLine >= 0 {
			bpMap := make(map[int]bool, len(bpLines))
			for _, l := range bpLines {
				bpMap[l] = true
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

func (m Model) isActiveDiffTab() bool {
	if m.activeTab < len(m.tabBar.Tabs) {
		return m.tabBar.Tabs[m.activeTab].Kind == editor.TabDiff
	}
	return false
}

func (m Model) activeDiffView() string {
	if dv, ok := m.diffViews[m.activeTab]; ok {
		return dv.View()
	}
	return ""
}

func (m *Model) activeEditor() *editor.Editor {
	if len(m.editors) == 0 {
		return nil
	}
	if m.activeTab < len(m.editors) {
		return &m.editors[m.activeTab]
	}
	return &m.editors[0]
}

// forwardToEditor sends an adjusted mouse message to the active editor and handles LSP updates.
func (m Model) forwardToEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Route to diff view if active tab is a diff tab
	if m.isActiveDiffTab() {
		if dv, ok := m.diffViews[m.activeTab]; ok {
			var cmd tea.Cmd
			dv, cmd = dv.Update(msg)
			m.diffViews[m.activeTab] = dv
			return m, cmd
		}
		return m, nil
	}
	if m.activeEditor() == nil {
		return m, nil
	}
	ed := *m.activeEditor()
	if ed.Buffer.FilePath != "" {
		ed.HasLSP = m.lspMgr.ClientForFile(ed.Buffer.FilePath) != nil
	}
	prevVersion := ed.Buffer.Version()
	var cmd tea.Cmd
	ed, cmd = ed.Update(msg)
	m.editors[m.activeTab] = ed

	if m.activeTab < len(m.tabBar.Tabs) {
		m.tabBar.Tabs[m.activeTab].Dirty = ed.Buffer.Dirty()
	}
	if ed.Buffer.Version() != prevVersion && ed.Buffer.FilePath != "" {
		if client := m.lspMgr.ClientForFile(ed.Buffer.FilePath); client != nil {
			m.notifyLSPChange(client, &ed)
		}
	}
	return m, cmd
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

// handleGitPanelClick routes a click in the git panel area.
// adjustedY is relative to the panel (0-based), originalMsg has absolute coords for zone checks.
func (m Model) handleGitPanelClick(adjustedY int, originalMsg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Check zone-based buttons using original absolute-coordinate message
	if zone.Get("git-commit-btn").InBounds(originalMsg) {
		result, cmd := m.gitPanel.DoCommit()
		m.gitPanel = result
		return m, cmd
	}
	if zone.Get("git-push-btn").InBounds(originalMsg) {
		spinCmd := m.gitPanel.StartSpinner("Pushing...")
		return m, tea.Batch(git.PushCmd(m.gitPanel.RootDir()), spinCmd)
	}
	if zone.Get("git-pull-btn").InBounds(originalMsg) {
		spinCmd := m.gitPanel.StartSpinner("Pulling...")
		return m, tea.Batch(git.PullCmd(m.gitPanel.RootDir()), spinCmd)
	}
	if zone.Get("git-stage-all").InBounds(originalMsg) {
		return m, git.StageAllCmd(m.gitPanel.RootDir())
	}
	if zone.Get("git-unstage-all").InBounds(originalMsg) {
		return m, git.UnstageAllCmd(m.gitPanel.RootDir())
	}
	// Click on commit title → focus title input and position cursor
	if zone.Get("git-commit-title").InBounds(originalMsg) {
		mouse := originalMsg.Mouse()
		cmd := m.gitPanel.FocusTitleAt(mouse.X)
		return m, cmd
	}
	// Click on commit body → focus body and position cursor at click location
	if zone.Get("git-commit-body").InBounds(originalMsg) {
		mouse := originalMsg.Mouse()
		m.gitPanel.FocusBodyAt(adjustedY, mouse.X)
		return m, nil
	}

	// Positional fallback for commit form clicks (zone may only track last-marked line)
	switch m.gitPanel.CommitFormHitTest(adjustedY) {
	case "title":
		mouse := originalMsg.Mouse()
		cmd := m.gitPanel.FocusTitleAt(mouse.X)
		return m, cmd
	case "body":
		mouse := originalMsg.Mouse()
		m.gitPanel.FocusBodyAt(adjustedY, mouse.X)
		return m, nil
	}

	// Forward positional click with adjusted Y
	mouse := originalMsg.Mouse()
	mouse.Y = adjustedY
	adjusted := tea.MouseClickMsg(mouse)
	var cmd tea.Cmd
	m.gitPanel, cmd = m.gitPanel.Update(adjusted)
	return m, cmd
}

// updateBranchPicker handles input when the branch picker is visible.
func (m Model) updateBranchPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.branchPickerM, cmd = m.branchPickerM.Update(msg)
	return m, cmd
}

func (m Model) treeWidth() int {
	// Fixed sidebar width for consistency across all tabs
	// This ensures the sidebar doesn't change width when switching tabs
	const fixedWidth = 25

	// Respect screen size constraints
	if m.width < 80 {
		// On small screens, use proportional width
		tw := m.width / 4
		if tw < 15 {
			return 15
		}
		if tw > fixedWidth {
			return fixedWidth
		}
		return tw
	}

	return fixedWidth
}

func (m *Model) relayout() {
	statusHeight := 2 // divider + status bar
	tabBarHeight := 1

	// Agent panel width (0 if hidden)
	aw := m.agentPanelWidth()
	agentExtra := 0
	if aw > 0 {
		agentExtra = aw + 1 // +1 for border
	}

	m.tabBar.Width = m.width // will be constrained when tree is shown

	sidebarHeight := m.height - statusHeight

	if m.showTree {
		tw := m.treeWidth()
		editorWidth := m.width - tw - 1 - agentExtra // -1 for left border
		if editorWidth < 1 {
			editorWidth = 1
		}
		editorHeight := m.height - statusHeight - tabBarHeight
		if sidebarHeight < 1 {
			sidebarHeight = 1
		}
		if editorHeight < 1 {
			editorHeight = 1
		}

		// Sidebar tab bar takes 1 line; active panel gets the rest
		panelHeight := sidebarHeight - 1
		if panelHeight < 1 {
			panelHeight = 1
		}

		m.tree.SetSize(tw, panelHeight)
		m.gitPanel.SetSize(tw, panelHeight)
		m.tabBar.Width = editorWidth
		for i := range m.editors {
			m.editors[i].SetSize(editorWidth, editorHeight)
		}
		for k, dv := range m.diffViews {
			dv.SetSize(editorWidth, editorHeight)
			m.diffViews[k] = dv
		}
		if m.welcome != nil {
			m.welcome.SetSize(editorWidth, editorHeight)
		}
	} else {
		editorWidth := m.width - agentExtra
		if editorWidth < 1 {
			editorWidth = 1
		}
		editorHeight := m.height - statusHeight - tabBarHeight
		if editorHeight < 1 {
			editorHeight = 1
		}
		m.tabBar.Width = editorWidth
		for i := range m.editors {
			m.editors[i].SetSize(editorWidth, editorHeight)
		}
		for k, dv := range m.diffViews {
			dv.SetSize(editorWidth, editorHeight)
			m.diffViews[k] = dv
		}
		if m.welcome != nil {
			m.welcome.SetSize(editorWidth, editorHeight)
		}
	}

	// Size agent panel
	if aw > 0 {
		agentHeight := sidebarHeight
		if agentHeight < 1 {
			agentHeight = 1
		}
		m.agentPanel.SetSize(aw, agentHeight)
	}
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

func (m Model) openFile(path string) (tea.Model, tea.Cmd) {
	return m.openFileAs(path, true)
}

// openFilePinned opens a file and immediately pins it (not a preview).
func (m Model) openFilePinned(path string) (tea.Model, tea.Cmd) {
	return m.openFileAs(path, false)
}

func (m Model) openFileAs(path string, preview bool) (tea.Model, tea.Cmd) {
	// Dismiss welcome screen if active
	if m.welcome != nil {
		m.welcome.Dismiss()
	}

	// Check if already open
	idx := m.tabBar.FindTab(path)
	if idx >= 0 {
		m.activeTab = idx
		m.tabBar.ActiveIdx = idx
		m.focus = FocusEditor
		// Double-open pins the tab
		if !preview {
			m.tabBar.PinTab(idx)
		}
		// Apply pending cursor if set
		if m.pendingCursor != nil {
			ed := m.activeEditor()
			ed.Buffer.Cursor = *m.pendingCursor
			ed.Viewport.EnsureCursorVisible(ed.Buffer.Cursor, ed.Buffer.LineCount())
			m.editors[m.activeTab] = *ed
			m.pendingCursor = nil
		}
		return m, nil
	}

	// Create a placeholder tab with an empty buffer, then load file async
	buf := text.NewBuffer()
	buf.FilePath = path
	cfg := editor.DefaultConfig()
	if len(m.editors) > 0 {
		cfg = m.editors[0].Config
	}
	cfg.CommentPrefix = editor.CommentPrefixForFile(path)
	ed := editor.New(buf, m.theme, cfg)

	// Try to replace an existing preview tab or empty untitled tab
	var tabIdx int
	replaceIdx := m.findReplaceableTab()
	if replaceIdx >= 0 {
		m.editors[replaceIdx] = ed
		m.tabBar.Tabs[replaceIdx].Label = filepath.Base(path)
		m.tabBar.Tabs[replaceIdx].FilePath = path
		m.tabBar.Tabs[replaceIdx].Dirty = false
		m.tabBar.Tabs[replaceIdx].Preview = preview
		m.tabBar.Tabs[replaceIdx].DiagSeverity = 0
		m.activeTab = replaceIdx
		m.tabBar.ActiveIdx = replaceIdx
		tabIdx = replaceIdx
	} else {
		m.editors = append(m.editors, ed)
		tabIdx = m.tabBar.AddTab(filepath.Base(path), path)
		m.tabBar.Tabs[tabIdx].Preview = preview
		m.activeTab = tabIdx
		m.tabBar.ActiveIdx = tabIdx
	}

	m.focus = FocusEditor
	m.relayout()

	// Read file content asynchronously
	return m, loadFileCmd(path, tabIdx, false)
}

func (m Model) handleFileLoaded(msg FileLoadedMsg) (tea.Model, tea.Cmd) {
	// Find the tab that was waiting for this file
	tabIdx := msg.TabIndex
	if tabIdx < 0 || tabIdx >= len(m.editors) {
		return m, nil
	}
	// Verify this tab still corresponds to the loaded file
	if tabIdx < len(m.tabBar.Tabs) && m.tabBar.Tabs[tabIdx].FilePath != msg.Path {
		return m, nil
	}

	// Load content into the placeholder buffer (expand tabs to spaces)
	ed := &m.editors[tabIdx]
	ed.Buffer.LoadContentWithTabSize(msg.Data, ed.Config.TabSize)

	// Set up syntax highlighting
	cfg := ed.Config
	cfg.CommentPrefix = editor.CommentPrefixForFile(msg.Path)
	ed.Config = cfg
	newEd := editor.New(ed.Buffer, m.theme, ed.Config)
	m.editors[tabIdx] = newEd
	m.relayout()
	m.status = ""

	// Apply pending cursor if set (e.g. from go-to-definition)
	if m.pendingCursor != nil && tabIdx == m.activeTab {
		m.editors[tabIdx].Buffer.Cursor = *m.pendingCursor
		m.editors[tabIdx].Viewport.EnsureCursorVisible(m.editors[tabIdx].Buffer.Cursor, m.editors[tabIdx].Buffer.LineCount())
		m.pendingCursor = nil
	}

	// Watch this file for external changes
	if m.watcher != nil && msg.Path != "" {
		m.watcher.WatchFile(msg.Path)
	}

	// Async tokenize + LSP open
	return m, tea.Batch(
		m.editors[tabIdx].ScheduleInitialTokenize(),
		m.lspDidOpen(m.editors[tabIdx].Buffer),
	)
}

// findReplaceableTab returns the index of a preview tab or empty untitled tab, or -1.
func (m Model) findReplaceableTab() int {
	// Prefer replacing an existing preview tab
	idx := m.tabBar.FindPreviewTab()
	if idx >= 0 {
		return idx
	}
	// Fall back to empty untitled tab
	for i, tab := range m.tabBar.Tabs {
		if tab.FilePath == "" && !tab.Dirty && m.editors[i].Buffer.Rope().Len() == 0 {
			return i
		}
	}
	return -1
}

func (m Model) closeCurrentTab() (tea.Model, tea.Cmd) {
	return m.closeTab(m.activeTab)
}

func (m Model) closeTab(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.editors) {
		return m, nil
	}

	// Save closed tab to history for reopening
	tab := m.tabBar.Tabs[idx]
	if tab.FilePath != "" {
		m.closedTabs = append(m.closedTabs, ClosedTab{
			FilePath: tab.FilePath,
			Label:    tab.Label,
		})
		// Keep only last 20 closed tabs
		if len(m.closedTabs) > 20 {
			m.closedTabs = m.closedTabs[1:]
		}
	}

	buf := m.editors[idx].Buffer
	if buf.FilePath != "" {
		if client := m.lspMgr.ClientForFile(buf.FilePath); client != nil {
			client.DidClose(lsp.FileURI(buf.FilePath))
		}
	}

	// If closing the last tab, show the welcome screen with no tabs
	if len(m.editors) <= 1 {
		m.editors = nil
		m.tabBar.Tabs = nil
		m.activeTab = 0
		m.tabBar.ActiveIdx = 0
		w := editor.NewWelcome(m.theme)
		m.welcome = &w
		m.relayout()
		return m, m.welcome.Init()
	}

	m.editors = append(m.editors[:idx], m.editors[idx+1:]...)
	m.tabBar.RemoveTab(idx)
	m.activeTab = m.tabBar.ActiveIdx

	// Re-key diff views: remove this index and shift higher indices down
	delete(m.diffViews, idx)
	newDiffs := make(map[int]diff.Model)
	for k, v := range m.diffViews {
		if k > idx {
			newDiffs[k-1] = v
		} else {
			newDiffs[k] = v
		}
	}
	m.diffViews = newDiffs

	return m, nil
}

func (m Model) handleTabBarClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Check close buttons first
	for i, tab := range m.tabBar.Tabs {
		if zone.Get(editor.TabCloseZoneID(tab)).InBounds(msg) {
			return m.closeTab(i)
		}
	}
	// Then check label zones for switching
	for i, tab := range m.tabBar.Tabs {
		if zone.Get(editor.TabZoneID(tab)).InBounds(msg) {
			m.activeTab = i
			m.tabBar.ActiveIdx = i
			return m, nil
		}
	}
	return m, nil
}

func (m Model) openSearch(mode search.Mode) (tea.Model, tea.Cmd) {
	m.showSearch = true
	m.searchMode = mode
	m.searchM = search.New(m.theme, m.rootDir, mode)
	m.searchM.SetSize(m.width, m.height-2)
	cmd := m.searchM.Focus()
	return m, cmd
}

func (m Model) openSearchReplace() (tea.Model, tea.Cmd) {
	m.showSearch = true
	m.searchMode = search.ModeText
	m.searchM = search.New(m.theme, m.rootDir, search.ModeText)
	m.searchM.SetShowReplace(true)
	m.searchM.SetSize(m.width, m.height-2)
	cmd := m.searchM.Focus()
	return m, cmd
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.searchM, cmd = m.searchM.Update(msg)
	return m, cmd
}

// updateProblems handles input for the Problems panel.
func (m Model) updateProblems(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.problemsPanel.SelectPrev()
			return m, nil
		case "down":
			m.problemsPanel.SelectNext()
			return m, nil
		case "pgup":
			m.problemsPanel.ScrollUp(m.problemsPanel.Height())
			return m, nil
		case "pgdown":
			m.problemsPanel.ScrollDown(m.problemsPanel.Height())
			return m, nil
		case "enter":
			// Open the selected problem location
			if prob := m.problemsPanel.SelectedProblem(); prob != nil {
				pos := text.Position{Line: prob.Line, Col: prob.Col}
				m.pendingCursor = &pos
				return m.openFilePinned(prob.FilePath)
			}
			return m, nil
		case "esc", "escape":
			// Switch back to editor focus
			m.focus = FocusEditor
			return m, nil
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			m.problemsPanel.ScrollUp(3)
		} else if mouse.Button == tea.MouseWheelDown {
			m.problemsPanel.ScrollDown(3)
		}
		return m, nil
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			// Select item at click position
			clickIdx := m.problemsPanel.ScrollY() + mouse.Y - 1 // -1 for tab bar
			if clickIdx >= 0 && clickIdx < m.problemsPanel.ProblemCount() {
				// Set selection to clicked item
				for i := 0; i < clickIdx-m.problemsPanel.ScrollY(); i++ {
					m.problemsPanel.SelectNext()
				}
			}
			return m, nil
		}
	}
	return m, nil
}

// updateDebugger handles input for the Debugger panel.
func (m Model) updateDebugger(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			m.focus = FocusEditor
			return m, nil
		case "c":
			if m.debugMgr.IsRunning() {
				if err := m.debugMgr.Continue(); err != nil {
					m.status = fmt.Sprintf("Debug error: %v", err)
				}
			}
			return m, nil
		case "n":
			if m.debugMgr.IsRunning() {
				if err := m.debugMgr.Next(); err != nil {
					m.status = fmt.Sprintf("Debug error: %v", err)
				}
			}
			return m, nil
		case "i":
			if m.debugMgr.IsRunning() {
				if err := m.debugMgr.StepIn(); err != nil {
					m.status = fmt.Sprintf("Debug error: %v", err)
				}
			}
			return m, nil
		case "o":
			if m.debugMgr.IsRunning() {
				if err := m.debugMgr.StepOut(); err != nil {
					m.status = fmt.Sprintf("Debug error: %v", err)
				}
			}
			return m, nil
		case "q":
			if m.debugMgr.IsRunning() {
				m.debugMgr.Stop()
				m.debuggerPanel.SetState(dap.StateInactive)
				m.currentExecFile = ""
				m.currentExecLine = -1
				m.status = "Debugging stopped"
			}
			return m, nil
		case "up":
			// Navigate stack frames up
			cur := m.debuggerPanel.CurrentFrame()
			if cur > 0 {
				cmd := m.debuggerPanel.SelectFrame(cur - 1)
				return m, cmd
			}
			return m, nil
		case "down":
			// Navigate stack frames down
			cur := m.debuggerPanel.CurrentFrame()
			cmd := m.debuggerPanel.SelectFrame(cur + 1)
			return m, cmd
		case "enter":
			// Jump to current frame location
			cmd := m.debuggerPanel.SelectFrame(m.debuggerPanel.CurrentFrame())
			return m, cmd
		}
	case tea.MouseClickMsg:
		return m, nil
	case tea.MouseWheelMsg:
		return m, nil
	}
	return m, nil
}

// updateSettings handles input for the Settings overlay.
func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape", "ctrl+,":
			// Close settings
			m.showSettings = false
			return m, nil
		case "up":
			m.settingsM.SelectPrevSetting()
			return m, nil
		case "down":
			m.settingsM.SelectNextSetting()
			return m, nil
		case "left":
			m.settingsM.SelectPrevCategory()
			return m, nil
		case "right":
			m.settingsM.SelectNextCategory()
			return m, nil
		case "tab":
			// Toggle between categories and settings
			// For now, just move to next category
			m.settingsM.SelectNextCategory()
			return m, nil
		case "enter":
			// Toggle boolean value or edit string/int
			setting := m.settingsM.SelectedSetting()
			if setting != nil {
				switch setting.Type {
				case settings.TypeBool:
					m.settingsM.ToggleBoolValue()
				case settings.TypeInt:
					// Could open input dialog, for now just increment
					m.settingsM.IncrementIntValue()
				}
			}
			return m, nil
		case "+":
			// Increment integer value
			m.settingsM.IncrementIntValue()
			return m, nil
		case "-":
			// Decrement integer value
			m.settingsM.DecrementIntValue()
			return m, nil
		case " ":
			// Toggle boolean
			m.settingsM.ToggleBoolValue()
			return m, nil
		case "r":
			// Reset to default
			m.settingsM.ResetCurrentValue()
			return m, nil
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			m.settingsM.SelectPrevSetting()
		} else if mouse.Button == tea.MouseWheelDown {
			m.settingsM.SelectNextSetting()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleDiagnostics(msg lsp.DiagnosticsMsg) (tea.Model, tea.Cmd) {
	path := lsp.URIToPath(msg.URI)

	for i := range m.editors {
		if m.editors[i].Buffer.FilePath == path {
			diags := make([]editor.Diagnostic, len(msg.Diagnostics))
			for j, d := range msg.Diagnostics {
				diags[j] = editor.Diagnostic{
					StartLine: d.Range.Start.Line,
					StartCol:  d.Range.Start.Character,
					EndLine:   d.Range.End.Line,
					EndCol:    d.Range.End.Character,
					Severity:  int(d.Severity),
					Message:   d.Message,
				}
			}
			m.editors[i].Diagnostics = diags
			break
		}
	}

	// Update centralized file diagnostics map
	if len(msg.Diagnostics) == 0 {
		delete(m.fileDiagnostics, path)
	} else {
		worst := 4
		for _, d := range msg.Diagnostics {
			if int(d.Severity) < worst {
				worst = int(d.Severity)
			}
		}
		m.fileDiagnostics[path] = worst
	}

	// Sync to matching tab
	for i, tab := range m.tabBar.Tabs {
		if tab.FilePath == path {
			sev := m.fileDiagnostics[path] // 0 if deleted
			m.tabBar.Tabs[i].DiagSeverity = sev
		}
	}

	// Recompute directory diagnostics and push to file tree
	m.updateDirDiagnostics()
	merged := make(map[string]int, len(m.fileDiagnostics)+len(m.dirDiagnostics))
	for k, v := range m.fileDiagnostics {
		merged[k] = v
	}
	for k, v := range m.dirDiagnostics {
		merged[k] = v
	}
	m.tree.SetDiagnostics(merged)

	// Update problems panel
	m.updateProblemsPanel()

	return m, nil
}

// updateDirDiagnostics computes worst severity for ancestor directories.
func (m *Model) updateDirDiagnostics() {
	m.dirDiagnostics = make(map[string]int)
	for path, sev := range m.fileDiagnostics {
		dir := filepath.Dir(path)
		for dir != m.rootDir && dir != "/" && dir != "." {
			if existing, ok := m.dirDiagnostics[dir]; !ok || sev < existing {
				m.dirDiagnostics[dir] = sev
			}
			dir = filepath.Dir(dir)
		}
	}
}

// updateProblemsPanel rebuilds the problems panel from file diagnostics.
func (m *Model) updateProblemsPanel() {
	// Collect all problems from editor diagnostics
	var allProblems []problems.Problem
	for _, ed := range m.editors {
		if ed.Buffer.FilePath == "" {
			continue
		}
		for _, d := range ed.Diagnostics {
			allProblems = append(allProblems, problems.Problem{
				FilePath: ed.Buffer.FilePath,
				Line:     d.StartLine,
				Col:      d.StartCol,
				EndLine:  d.EndLine,
				EndCol:   d.EndCol,
				Severity: d.Severity,
				Message:  d.Message,
			})
		}
	}

	// Sort problems by severity (errors first), then by file path, then by line
	sortProblems(allProblems)
	m.problemsPanel.SetProblems(allProblems)
}

// sortProblems sorts problems by severity, path, and line.
func sortProblems(probs []problems.Problem) {
	for i := 0; i < len(probs)-1; i++ {
		for j := i + 1; j < len(probs); j++ {
			// Sort by severity first (lower = more severe)
			if probs[i].Severity != probs[j].Severity {
				if probs[i].Severity > probs[j].Severity {
					probs[i], probs[j] = probs[j], probs[i]
				}
				continue
			}
			// Then by file path
			if probs[i].FilePath != probs[j].FilePath {
				if probs[i].FilePath > probs[j].FilePath {
					probs[i], probs[j] = probs[j], probs[i]
				}
				continue
			}
			// Then by line number
			if probs[i].Line > probs[j].Line {
				probs[i], probs[j] = probs[j], probs[i]
			}
		}
	}
}

// LSP helpers

type lspMsg struct {
	msg tea.Msg
}

// lspLocationPickerMsg is the Value payload for an LSP location picker item.
type lspLocationPickerMsg struct {
	Location lsp.Location
}

// lspSymbolPickerMsg is the Value payload for an LSP document symbol picker item.
type lspSymbolPickerMsg struct {
	Symbol lsp.DocumentSymbol
}

func (m Model) listenLSP() tea.Cmd {
	ch := m.lspMgr.MsgChan()
	return func() tea.Msg {
		raw, ok := <-ch
		if !ok {
			return nil
		}
		return lspMsg{msg: raw.(tea.Msg)}
	}
}

// DAP helpers

type dapMsg struct {
	msg any
}

// toggleBreakpoint toggles a breakpoint at the given file and line (0-based).
func (m *Model) toggleBreakpoint(filePath string, line int) tea.Cmd {
	lines := m.breakpoints[filePath]

	// Check if breakpoint already exists at this line
	idx := -1
	for i, l := range lines {
		if l == line {
			idx = i
			break
		}
	}

	if idx >= 0 {
		// Remove breakpoint
		lines = append(lines[:idx], lines[idx+1:]...)
	} else {
		// Add breakpoint in sorted position
		inserted := false
		for i, l := range lines {
			if line < l {
				lines = append(lines[:i+1], lines[i:]...)
				lines[i] = line
				inserted = true
				break
			}
		}
		if !inserted {
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		delete(m.breakpoints, filePath)
	} else {
		m.breakpoints[filePath] = lines
	}

	// Update debugger panel breakpoint display
	m.syncDebuggerBreakpoints()

	// Send to DAP if debugging
	if m.debugMgr.IsRunning() {
		return m.sendBreakpointsToDAP(filePath)
	}
	return nil
}

// syncDebuggerBreakpoints updates the debugger panel's breakpoint list.
func (m *Model) syncDebuggerBreakpoints() {
	var bps []debugger.Breakpoint
	for fp, lines := range m.breakpoints {
		for _, line := range lines {
			bps = append(bps, debugger.Breakpoint{
				FilePath: fp,
				Line:     line,
				Enabled:  true,
			})
		}
	}
	m.debuggerPanel.SetBreakpoints(bps)
}

// sendBreakpointsToDAP sends breakpoints for a file to the DAP adapter.
func (m Model) sendBreakpointsToDAP(filePath string) tea.Cmd {
	mgr := m.debugMgr
	lines := m.breakpoints[filePath]
	// DAP uses 1-based lines
	dapLines := make([]int, len(lines))
	for i, l := range lines {
		dapLines[i] = l + 1
	}
	return func() tea.Msg {
		mgr.SetBreakpoints(filePath, dapLines)
		return nil
	}
}

func (m Model) listenDAP() tea.Cmd {
	ch := m.debugMgr.MsgChan()
	return func() tea.Msg {
		raw, ok := <-ch
		if !ok {
			return nil
		}
		return dapMsg{msg: raw}
	}
}

func (m Model) handleDAPMsg(msg dapMsg) (tea.Model, tea.Cmd) {
	if msg.msg == nil {
		return m, m.listenDAP()
	}

	switch inner := msg.msg.(type) {
	case dap.StoppedEventMsg:
		m.debuggerPanel.SetState(dap.StatePaused)
		m.status = fmt.Sprintf("Stopped: %s", inner.Reason)
		// Fetch stack trace, scopes, and variables
		cmd := m.fetchDebugState()
		return m, tea.Batch(cmd, m.listenDAP())

	case dap.ContinuedEventMsg:
		m.debuggerPanel.SetState(dap.StateRunning)
		m.currentExecFile = ""
		m.currentExecLine = -1
		m.status = "Debugging"
		return m, m.listenDAP()

	case dap.TerminatedEventMsg:
		m.debugMgr.Stop()
		m.debuggerPanel.SetState(dap.StateInactive)
		m.currentExecFile = ""
		m.currentExecLine = -1
		m.status = "Debug session terminated"
		return m, m.listenDAP()

	case dap.ExitedEventMsg:
		m.debugMgr.Stop()
		m.debuggerPanel.SetState(dap.StateInactive)
		m.currentExecFile = ""
		m.currentExecLine = -1
		m.status = fmt.Sprintf("Process exited with code %d", inner.ExitCode)
		return m, m.listenDAP()

	case dap.OutputEventMsg:
		m.debuggerPanel.AppendOutput(strings.TrimRight(inner.Output, "\n"))
		return m, m.listenDAP()

	case dap.BreakpointEventMsg:
		// Breakpoint status changed — could update UI markers
		return m, m.listenDAP()
	}

	return m, m.listenDAP()
}

// fetchDebugState fetches stack trace, scopes, and variables after a stopped event.
func (m Model) fetchDebugState() tea.Cmd {
	mgr := m.debugMgr
	return func() tea.Msg {
		frames, err := mgr.GetStackTrace()
		if err != nil || len(frames) == 0 {
			return debugStateMsg{}
		}

		// Get scopes for top frame
		scopes, err := mgr.GetScopes(frames[0].Id)
		if err != nil {
			return debugStateMsg{Frames: frames}
		}

		// Get variables from the first non-expensive scope (usually "Locals")
		var vars []dap.Variable
		for _, scope := range scopes {
			if !scope.Expensive && scope.VariablesReference > 0 {
				vars, _ = mgr.GetVariables(scope.VariablesReference)
				break
			}
		}

		return debugStateMsg{Frames: frames, Variables: vars}
	}
}

// debugStateMsg carries fetched debug state back to Update.
type debugStateMsg struct {
	Frames    []dap.StackFrame
	Variables []dap.Variable
}

func (m Model) lspDidOpen(buf *text.Buffer) tea.Cmd {
	if buf.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := buf.FilePath
	content := buf.Content()
	version := buf.Version()
	return func() tea.Msg {
		client, err := mgr.EnsureClient(filePath)
		if err != nil || client == nil {
			return nil
		}
		// Wait for client to finish initializing (async init may still be in progress)
		for i := 0; i < 50; i++ { // up to 2.5 seconds
			if client.IsReady() {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !client.IsReady() {
			return nil
		}
		cfg := lsp.ConfigForFile(filePath)
		langID := ""
		if cfg != nil {
			langID = cfg.LanguageID
		}
		client.DidOpen(lsp.FileURI(filePath), langID, version, content)
		return LspReadyMsg{FilePath: filePath}
	}
}

func (m Model) requestCompletion() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		items, err := client.Completion(lsp.FileURI(filePath), line, col)
		if err != nil || len(items) == 0 {
			return nil
		}
		return lsp.CompletionResultMsg{Items: items}
	}
}

func (m Model) requestDefinition() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		locs, err := client.Definition(lsp.FileURI(filePath), line, col)
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/definition", Message: err.Error()}
		}
		return lsp.DefinitionResultMsg{Locations: locs}
	}
}

func (m Model) requestHover() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		result, err := client.Hover(lsp.FileURI(filePath), line, col)
		if err != nil || result == nil {
			return nil
		}
		return lsp.HoverResultMsg{Content: result.Content}
	}
}

func (m Model) requestHoverDelayed() tea.Cmd {
	// Debounce hover requests to avoid flickering
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return hoverTriggerMsg{}
	})
}

type hoverTriggerMsg struct{}

func (m Model) handleHoverTrigger() (tea.Model, tea.Cmd) {
	// Only show hover if we're not in the middle of typing
	ed := m.activeEditor()
	if ed == nil || ed.Buffer.FilePath == "" {
		return m, nil
	}
	return m, m.requestHover()
}

func (m Model) requestSignatureHelp() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		help, err := client.SignatureHelp(lsp.FileURI(filePath), line, col)
		if err != nil || help == nil {
			return nil
		}
		// Convert to editor.SignatureData
		sigData := &editor.SignatureData{
			ActiveSignature: help.ActiveSignature,
			ActiveParameter: help.ActiveParameter,
		}
		for _, sig := range help.Signatures {
			var params []editor.ParameterInfo
			for _, p := range sig.Parameters {
				label := ""
				switch v := p.Label.(type) {
				case string:
					label = v
				case []any:
					// Handle [start, end] format
					if len(v) >= 2 {
						label = sig.Label // Use full label for now
					}
				}
				params = append(params, editor.ParameterInfo{
					Label:         label,
					Documentation: p.Documentation,
				})
			}
			sigData.Signatures = append(sigData.Signatures, editor.SignatureInfo{
				Label:         sig.Label,
				Documentation: sig.Documentation,
				Parameters:    params,
			})
		}
		return lsp.SignatureHelpResultMsg{Help: help}
	}
}

func (m Model) requestFormatting() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		edits, err := client.Formatting(lsp.FileURI(filePath))
		if err != nil || len(edits) == 0 {
			return nil
		}
		return lsp.FormatResultMsg{Edits: edits}
	}
}

func (m Model) requestCodeActions() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		// Get diagnostics at cursor position
		var diags []lsp.Diagnostic
		for _, d := range ed.Diagnostics {
			if line >= d.StartLine && line <= d.EndLine {
				diags = append(diags, lsp.Diagnostic{
					Range: lsp.DiagRange{
						Start: lsp.DiagPosition{Line: d.StartLine, Character: d.StartCol},
						End:   lsp.DiagPosition{Line: d.EndLine, Character: d.EndCol},
					},
					Severity: lsp.DiagSeverity(d.Severity),
					Message:  d.Message,
					Source:   "",
				})
			}
		}
		actions, err := client.CodeAction(lsp.FileURI(filePath), line, col, line, col, diags)
		if err != nil || len(actions) == 0 {
			return nil
		}
		return lsp.CodeActionResultMsg{Actions: actions}
	}
}

func (m Model) requestDocumentSymbols() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		symbols, err := client.DocumentSymbol(lsp.FileURI(filePath))
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/documentSymbol", Message: err.Error()}
		}
		return lsp.DocumentSymbolResultMsg{Symbols: symbols}
	}
}

func (m Model) handleGoToLineInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.goToLineMode = false
		m.goToLineInput = ""
		return m, nil
	case "enter":
		m.goToLineMode = false
		if m.goToLineInput == "" {
			return m, nil
		}
		lineNum, err := strconv.Atoi(m.goToLineInput)
		m.goToLineInput = ""
		if err != nil {
			return m, nil
		}
		// Convert 1-based to 0-based
		lineNum--
		ed := m.activeEditor()
		maxLine := ed.Buffer.LineCount() - 1
		if lineNum < 0 {
			lineNum = 0
		}
		if lineNum > maxLine {
			lineNum = maxLine
		}
		ed.Buffer.ClearSelection()
		ed.Buffer.Cursor.Line = lineNum
		ed.Buffer.Cursor.Col = 0
		ed.Viewport.EnsureCursorVisible(ed.Buffer.Cursor, ed.Buffer.LineCount())
		m.editors[m.activeTab] = *ed
		return m, nil
	case "backspace":
		if len(m.goToLineInput) > 0 {
			m.goToLineInput = m.goToLineInput[:len(m.goToLineInput)-1]
		}
		return m, nil
	default:
		if msg.Text != "" && msg.Text >= "0" && msg.Text <= "9" {
			m.goToLineInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) handleContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "goto_definition":
		return m, m.requestDefinition()
	case "find_references":
		return m, m.requestReferences()
	case "rename_symbol":
		m.renameMode = true
		m.renameInput = ""
		return m, nil
	}
	return m, nil
}

func (m Model) requestReferences() tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		locs, err := client.References(lsp.FileURI(filePath), line, col)
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/references", Message: err.Error()}
		}
		return lsp.ReferencesResultMsg{Locations: locs}
	}
}

// lspLocationsToPickerItems converts LSP locations to picker items.
func lspLocationsToPickerItems(locs []lsp.Location, rootDir string) []overlay.PickerItem {
	items := make([]overlay.PickerItem, len(locs))
	for i, loc := range locs {
		path := lsp.URIToPath(loc.URI)
		rel := path
		if rootDir != "" {
			if r, err := filepath.Rel(rootDir, path); err == nil {
				rel = r
			}
		}
		label := fmt.Sprintf("%s:%d", filepath.Base(rel), loc.StartLine+1)
		desc := filepath.Dir(rel)
		if desc == "." {
			desc = ""
		}
		items[i] = overlay.PickerItem{
			Label:       label,
			Description: desc,
			Value:       lspLocationPickerMsg{Location: loc},
		}
	}
	return items
}

// lspSymbolsToPickerItems flattens document symbols into picker items.
func lspSymbolsToPickerItems(symbols []lsp.DocumentSymbol) []overlay.PickerItem {
	var items []overlay.PickerItem
	var flatten func(syms []lsp.DocumentSymbol, prefix string)
	flatten = func(syms []lsp.DocumentSymbol, prefix string) {
		for _, s := range syms {
			label := s.Name
			if prefix != "" {
				label = prefix + "." + s.Name
			}
			desc := s.Detail
			if desc == "" {
				desc = symbolKindName(s.Kind)
			}
			items = append(items, overlay.PickerItem{
				Label:       label,
				Description: desc,
				Value:       lspSymbolPickerMsg{Symbol: s},
			})
			if len(s.Children) > 0 {
				flatten(s.Children, label)
			}
		}
	}
	flatten(symbols, "")
	return items
}

// symbolKindName returns a human-readable name for an LSP SymbolKind value.
func symbolKindName(kind int) string {
	switch kind {
	case 1:
		return "File"
	case 2:
		return "Module"
	case 3:
		return "Namespace"
	case 4:
		return "Package"
	case 5:
		return "Class"
	case 6:
		return "Method"
	case 7:
		return "Property"
	case 8:
		return "Field"
	case 9:
		return "Constructor"
	case 10:
		return "Enum"
	case 11:
		return "Interface"
	case 12:
		return "Function"
	case 13:
		return "Variable"
	case 14:
		return "Constant"
	case 15:
		return "String"
	case 16:
		return "Number"
	case 17:
		return "Boolean"
	case 18:
		return "Array"
	case 19:
		return "Object"
	case 23:
		return "Struct"
	case 24:
		return "Event"
	case 25:
		return "Operator"
	case 26:
		return "TypeParameter"
	default:
		return "Symbol"
	}
}

func (m Model) requestRename(newName string) tea.Cmd {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		edits, err := client.Rename(lsp.FileURI(filePath), line, col, newName)
		if err != nil || len(edits) == 0 {
			return nil
		}
		return lsp.RenameResultMsg{Edits: edits}
	}
}

func (m Model) handleRenameInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.renameMode = false
		m.renameInput = ""
		return m, nil
	case "enter":
		m.renameMode = false
		if m.renameInput == "" {
			return m, nil
		}
		newName := m.renameInput
		m.renameInput = ""
		return m, m.requestRename(newName)
	case "backspace":
		if len(m.renameInput) > 0 {
			m.renameInput = m.renameInput[:len(m.renameInput)-1]
		}
		return m, nil
	default:
		if msg.Text != "" {
			m.renameInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) applyRenameEdits(edits map[string][]lsp.TextEdit) (tea.Model, tea.Cmd) {
	applied := 0
	for uri, textEdits := range edits {
		path := lsp.URIToPath(uri)
		// Find open editor for this file
		for i := range m.editors {
			if m.editors[i].Buffer.FilePath == path {
				// Apply edits in reverse order to preserve positions
				sortedEdits := make([]lsp.TextEdit, len(textEdits))
				copy(sortedEdits, textEdits)
				// Sort in descending order by position (later edits first)
				// This ensures that applying earlier edits doesn't shift positions of later edits
				for a := 0; a < len(sortedEdits); a++ {
					for b := a + 1; b < len(sortedEdits); b++ {
						// Swap if b comes after a (we want descending order)
						if sortedEdits[b].StartLine > sortedEdits[a].StartLine ||
							(sortedEdits[b].StartLine == sortedEdits[a].StartLine && sortedEdits[b].StartCol > sortedEdits[a].StartCol) {
							sortedEdits[a], sortedEdits[b] = sortedEdits[b], sortedEdits[a]
						}
					}
				}
				buf := m.editors[i].Buffer
				for _, te := range sortedEdits {
					start := text.Position{Line: te.StartLine, Col: te.StartCol}
					end := text.Position{Line: te.EndLine, Col: te.EndCol}
					buf.ReplaceRange(start, end, []byte(te.NewText))
					applied++
				}
				if m.editors[i].Highlighter != nil {
					m.editors[i].Highlighter.Invalidate()
				}
				break
			}
		}
	}
	if applied > 0 {
		m.status = fmt.Sprintf("Renamed: %d edit(s) applied", applied)
	} else {
		m.status = "Rename: no edits applied"
	}
	return m, nil
}

func (m Model) showTreeContextMenu(x, y int) (tea.Model, tea.Cmd) {
	// Get the entry at the clicked position from the tree
	entry := m.tree.EntryAtY(y)

	var items []editor.ContextMenuItem
	if entry == nil {
		// Clicked empty area — offer root-level actions
		m.treeContextPath = m.rootDir
		items = []editor.ContextMenuItem{
			{Label: "New File...", Action: "tree_new_file"},
			{Label: "New Folder...", Action: "tree_new_folder"},
		}
		m.treeContextMenu.Show(items, x, y)
		return m, nil
	}

	m.treeContextPath = entry.Path

	if entry.IsDir {
		items = []editor.ContextMenuItem{
			{Label: "New File...", Action: "tree_new_file"},
			{Label: "New Folder...", Action: "tree_new_folder"},
			{Label: ""}, // separator
			{Label: "Expand/Collapse", Action: "tree_toggle"},
			{Label: ""}, // separator
			{Label: "Copy Path", Action: "tree_copy_path"},
			{Label: "Delete", Action: "tree_delete"},
		}
	} else {
		items = []editor.ContextMenuItem{
			{Label: "Open File", Action: "tree_open"},
			{Label: "Open in New Tab", Action: "tree_open_new_tab"},
			{Label: ""}, // separator
			{Label: "New File...", Action: "tree_new_file_sibling"},
			{Label: "New Folder...", Action: "tree_new_folder_sibling"},
			{Label: ""}, // separator
			{Label: "Copy Path", Action: "tree_copy_path"},
			{Label: "Delete", Action: "tree_delete"},
		}
	}

	m.treeContextMenu.Show(items, x, y)
	return m, nil
}

func (m Model) handleTreeContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "tree_open":
		return m.openFile(m.treeContextPath)
	case "tree_open_new_tab":
		return m.openFileForceNewTab(m.treeContextPath)
	case "tree_copy_path":
		// Copy the relative path to clipboard
		relPath, err := filepath.Rel(m.rootDir, m.treeContextPath)
		if err != nil {
			relPath = m.treeContextPath
		}
		// Use the clipboard package
		m.status = fmt.Sprintf("Copied: %s", relPath)
		return m, nil
	case "tree_toggle":
		var cmd tea.Cmd
		m.tree, cmd = m.tree.ToggleEntry(m.treeContextPath)
		return m, cmd
	case "tree_new_file":
		m.newFileMode = true
		m.newItemInput = ""
		m.newItemDir = m.treeContextPath
		return m, nil
	case "tree_new_folder":
		m.newFolderMode = true
		m.newItemInput = ""
		m.newItemDir = m.treeContextPath
		return m, nil
	case "tree_new_file_sibling":
		m.newFileMode = true
		m.newItemInput = ""
		m.newItemDir = filepath.Dir(m.treeContextPath)
		return m, nil
	case "tree_new_folder_sibling":
		m.newFolderMode = true
		m.newItemInput = ""
		m.newItemDir = filepath.Dir(m.treeContextPath)
		return m, nil
	case "tree_delete":
		m.deleteConfirm = true
		m.deleteTarget = m.treeContextPath
		return m, nil
	}
	return m, nil
}

func (m Model) showGitContextMenu(x, y, panelY int) (tea.Model, tea.Cmd) {
	node, staged := m.gitPanel.NodeAtY(panelY)
	if node == nil {
		return m, nil
	}

	var items []editor.ContextMenuItem
	if node.IsDir {
		// Directory node — offer stage/unstage all files in folder
		if staged {
			items = []editor.ContextMenuItem{
				{Label: "Unstage Folder", Action: "git_unstage_dir"},
			}
		} else {
			items = []editor.ContextMenuItem{
				{Label: "Stage Folder", Action: "git_stage_dir"},
			}
		}
		m.gitContextEntry = nil
		m.gitContextStaged = staged
		m.gitContextPath = node.Path
	} else if node.Entry != nil {
		// File node
		m.gitContextEntry = node.Entry
		m.gitContextStaged = staged
		m.gitContextPath = node.Entry.Path
		if staged {
			items = []editor.ContextMenuItem{
				{Label: "Unstage File", Action: "git_unstage"},
				{Label: "View Diff", Action: "git_diff"},
			}
		} else {
			items = []editor.ContextMenuItem{
				{Label: "Stage File", Action: "git_stage"},
				{Label: "View Diff", Action: "git_diff"},
			}
		}
	} else {
		return m, nil
	}

	m.gitContextMenu.Show(items, x, y)
	return m, nil
}

func (m Model) handleGitContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "git_stage":
		if m.gitContextEntry != nil {
			return m, git.StageCmd(m.gitPanel.RootDir(), m.gitContextEntry.Path)
		}
	case "git_unstage":
		if m.gitContextEntry != nil {
			return m, git.UnstageCmd(m.gitPanel.RootDir(), m.gitContextEntry.Path)
		}
	case "git_diff":
		if m.gitContextEntry != nil {
			status := m.gitContextEntry.DisplayStatus(m.gitContextStaged)
			return m.openDiff(m.gitContextEntry.Path, status)
		}
	case "git_stage_dir":
		// Stage all files under the directory
		return m, git.StageCmd(m.gitPanel.RootDir(), m.gitContextPath)
	case "git_unstage_dir":
		// Unstage all files under the directory
		return m, git.UnstageCmd(m.gitPanel.RootDir(), m.gitContextPath)
	}
	return m, nil
}

func (m Model) openFileForceNewTab(path string) (tea.Model, tea.Cmd) {
	// Create a placeholder tab with an empty buffer, then load file async
	buf := text.NewBuffer()
	buf.FilePath = path
	cfg := editor.DefaultConfig()
	if len(m.editors) > 0 {
		cfg = m.editors[0].Config
	}
	cfg.CommentPrefix = editor.CommentPrefixForFile(path)
	ed := editor.New(buf, m.theme, cfg)

	m.editors = append(m.editors, ed)
	idx := m.tabBar.AddTab(filepath.Base(path), path)
	m.activeTab = idx
	m.tabBar.ActiveIdx = idx
	m.focus = FocusEditor
	m.relayout()

	return m, loadFileCmd(path, idx, true)
}

func (m Model) openDiff(relPath, status string) (tea.Model, tea.Cmd) {
	// Dismiss welcome screen if active
	if m.welcome != nil {
		m.welcome.Dismiss()
	}

	// Use a synthetic path to avoid collision with normal file tabs
	diffKey := "diff://" + relPath

	// Check if already open — pin it (double-open behavior)
	idx := m.tabBar.FindTab(diffKey)
	if idx >= 0 {
		m.activeTab = idx
		m.tabBar.ActiveIdx = idx
		m.tabBar.PinTab(idx)
		m.focus = FocusEditor
		return m, nil
	}

	// Create a placeholder editor (unused, but keeps editors slice in sync with tabs)
	buf := text.NewBuffer()
	ed := editor.New(buf, m.theme, editor.DefaultConfig())

	// Try to reuse a preview tab (same as file opening behavior)
	var tabIdx int
	label := "\u0394 " + filepath.Base(relPath)
	replaceIdx := m.findReplaceableTab()
	if replaceIdx >= 0 {
		// Clean up any old diff view for this slot
		delete(m.diffViews, replaceIdx)
		m.editors[replaceIdx] = ed
		m.tabBar.Tabs[replaceIdx].Label = label
		m.tabBar.Tabs[replaceIdx].FilePath = diffKey
		m.tabBar.Tabs[replaceIdx].Dirty = false
		m.tabBar.Tabs[replaceIdx].Preview = true
		m.tabBar.Tabs[replaceIdx].Kind = editor.TabDiff
		m.tabBar.Tabs[replaceIdx].DiagSeverity = 0
		m.activeTab = replaceIdx
		m.tabBar.ActiveIdx = replaceIdx
		tabIdx = replaceIdx
	} else {
		m.editors = append(m.editors, ed)
		tabIdx = m.tabBar.AddTab(label, diffKey)
		m.tabBar.Tabs[tabIdx].Kind = editor.TabDiff
		m.tabBar.Tabs[tabIdx].Preview = true
		m.activeTab = tabIdx
		m.tabBar.ActiveIdx = tabIdx
	}

	m.focus = FocusEditor
	m.relayout()

	return m, loadDiffCmd(m.rootDir, relPath, status, tabIdx)
}

func (m Model) handleDiffLoaded(msg DiffLoadedMsg) (tea.Model, tea.Cmd) {
	tabIdx := msg.TabIndex
	if tabIdx < 0 || tabIdx >= len(m.tabBar.Tabs) {
		return m, nil
	}
	// Verify this tab is still a diff tab for the right path
	if m.tabBar.Tabs[tabIdx].FilePath != "diff://"+msg.Path {
		return m, nil
	}

	if msg.Err != nil {
		m.status = fmt.Sprintf("Diff error: %v", msg.Err)
		return m, nil
	}

	dv := diff.New(msg.Path, msg.Lines, m.theme)
	if m.diffViews == nil {
		m.diffViews = make(map[int]diff.Model)
	}
	m.diffViews[tabIdx] = dv
	m.relayout()
	m.status = ""
	return m, nil
}

// detectGitBranch returns the current git branch name, or "" if not in a repo.
func detectGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// handleExternalFileChange reloads a file that was modified externally.
func (m Model) handleExternalFileChange(msg FileChangedMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for i, ed := range m.editors {
		if ed.Buffer.FilePath == msg.Path && !ed.Buffer.Dirty() {
			// Reload content into the buffer
			m.editors[i].Buffer.LoadContentWithTabSize(msg.Data, ed.Config.TabSize)
			if m.editors[i].Highlighter != nil {
				m.editors[i].Highlighter.Invalidate()
			}
			m.status = fmt.Sprintf("Reloaded: %s (external change)", filepath.Base(msg.Path))
			// Re-tokenize
			cmds = append(cmds, m.editors[i].ScheduleInitialTokenize())
		}
	}
	// Continue listening for more file events
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

// handleTreeChange refreshes the file tree when the directory structure changes.
func (m Model) handleTreeChange(msg TreeChangedMsg) (tea.Model, tea.Cmd) {
	// Invalidate cached file list for quick open
	m.cachedFilesReady = false
	m.cachedFiles = nil
	// Refresh the file tree by rebuilding it
	m.tree.RefreshDir(msg.Dir)
	var cmds []tea.Cmd
	// Also refresh git panel
	if refreshCmd := m.gitPanel.Refresh(); refreshCmd != nil {
		cmds = append(cmds, refreshCmd)
	}
	// Continue listening
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

// handleNewItemInput handles keyboard input for creating new files/folders.
func (m Model) handleNewItemInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.newFileMode = false
		m.newFolderMode = false
		m.newItemInput = ""
		return m, nil
	case "enter":
		name := m.newItemInput
		isFolder := m.newFolderMode
		dir := m.newItemDir
		m.newFileMode = false
		m.newFolderMode = false
		m.newItemInput = ""
		if name == "" {
			return m, nil
		}
		fullPath := filepath.Join(dir, name)
		if isFolder {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				m.status = fmt.Sprintf("Error creating folder: %v", err)
				return m, nil
			}
			m.status = fmt.Sprintf("Created folder: %s", name)
			m.tree.RefreshDir(dir)
			if m.watcher != nil {
				m.watcher.WatchDir(fullPath)
			}
		} else {
			if err := os.WriteFile(fullPath, []byte(""), 0o644); err != nil {
				m.status = fmt.Sprintf("Error creating file: %v", err)
				return m, nil
			}
			m.status = fmt.Sprintf("Created: %s", name)
			m.tree.RefreshDir(dir)
			return m.openFilePinned(fullPath)
		}
		return m, nil
	case "backspace":
		if len(m.newItemInput) > 0 {
			m.newItemInput = m.newItemInput[:len(m.newItemInput)-1]
		}
		return m, nil
	default:
		if msg.Text != "" {
			m.newItemInput += msg.Text
		}
		return m, nil
	}
}

// handleDeleteConfirm handles the delete confirmation prompt.
func (m Model) handleDeleteConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		target := m.deleteTarget
		m.deleteConfirm = false
		m.deleteTarget = ""
		// Close any open tabs for this file
		for i := len(m.editors) - 1; i >= 0; i-- {
			if m.editors[i].Buffer.FilePath == target {
				m2, _ := m.closeTab(i)
				m = m2.(Model)
			}
		}
		if err := os.RemoveAll(target); err != nil {
			m.status = fmt.Sprintf("Error deleting: %v", err)
			return m, nil
		}
		m.status = fmt.Sprintf("Deleted: %s", filepath.Base(target))
		m.tree.RefreshDir(filepath.Dir(target))
		return m, nil
	default:
		m.deleteConfirm = false
		m.deleteTarget = ""
		return m, nil
	}
}

func lipglossWidth(s string) int {
	n := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		n++
	}
	return n
}

// openQuickOpen pushes a Picker overlay for quick file opening.
func (m Model) openQuickOpen() (tea.Model, tea.Cmd) {
	picker := overlay.NewPicker("Open File", nil, m.theme, "quickopen")
	picker.SetSize(min(m.width-4, 60), m.height-4)

	var cmds []tea.Cmd
	cmds = append(cmds, picker.Focus())

	if m.cachedFilesReady {
		picker.SetItems(filesToPickerItems(m.cachedFiles))
	} else {
		cmds = append(cmds, quickOpenCmd(m.rootDir))
	}

	m.overlayStack.Push(picker)
	return m, tea.Batch(cmds...)
}

// openCommandPalette pushes a Picker overlay with available commands.
func (m Model) openCommandPalette() (tea.Model, tea.Cmd) {
	items := m.buildCommandList()
	picker := overlay.NewPicker("Command Palette", items, m.theme, "cmdpalette")
	picker.SetSize(min(m.width-4, 60), m.height-4)
	cmd := picker.Focus()
	m.overlayStack.Push(picker)
	return m, cmd
}

// handleCommandPaletteAction dispatches an action from the command palette.
func (m Model) handleCommandPaletteAction(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case saveRequestMsg:
		if m.activeEditor() == nil {
			return m, nil
		}
		buf := m.activeEditor().Buffer
		return m, SaveFileCmd(buf.Save, buf.FilePath)
	case toggleTreeMsg:
		m.showTree = !m.showTree
		if m.showTree {
			m.focus = FocusTree
		} else {
			m.focus = FocusEditor
		}
		m.relayout()
		return m, nil
	case toggleGitMsg:
		if m.gitPanel.IsGitRepo() {
			m.showTree = true
			m.sidebarTab = SidebarGit
			m.focus = FocusGitPanel
			m.relayout()
		}
		return m, nil
	case toggleProblemsMsg:
		m.showTree = true
		m.sidebarTab = SidebarProblems
		m.focus = FocusProblems
		m.relayout()
		return m, nil
	case openSearchMsg:
		innerMsg := msg.(openSearchMsg)
		return m.openSearch(innerMsg.mode)
	case openSearchReplaceMsg:
		return m.openSearchReplace()
	case goToLineMsg:
		m.goToLineMode = true
		m.goToLineInput = ""
		return m, nil
	case quickOpenMsg:
		return m.openQuickOpen()
	case showHelpMsg:
		m.showHelp = true
		m.helpM = editor.NewHelpModel(m.theme)
		m.helpM.SetSize(m.width, m.height-2)
		cmd := m.helpM.Focus()
		return m, cmd
	case openSettingsMsg:
		m.showSettings = true
		m.settingsM.SetSize(m.width, m.height-4)
		return m, nil
	case reopenTabMsg:
		// Reopen last closed tab
		if len(m.closedTabs) > 0 {
			lastClosed := m.closedTabs[len(m.closedTabs)-1]
			m.closedTabs = m.closedTabs[:len(m.closedTabs)-1]
			return m.openFilePinned(lastClosed.FilePath)
		}
		m.status = "No closed tabs to reopen"
		return m, nil
	case debugStartMsg:
		// Start debugging
		if m.activeEditor() != nil && m.activeEditor().Buffer.FilePath != "" {
			program := m.activeEditor().Buffer.FilePath
			config := dap.ConfigForProgram(program)
			if config.Command == "" {
				m.status = "No debugger configured for this file type"
				return m, nil
			}
			if err := m.debugMgr.Start(config); err != nil {
				m.status = fmt.Sprintf("Debug error: %v", err)
				return m, nil
			}
			if err := m.debugMgr.Launch(); err != nil {
				m.status = fmt.Sprintf("Launch error: %v", err)
				return m, nil
			}
			m.debuggerPanel.SetState(dap.StateRunning)
			m.showTree = true
			m.sidebarTab = SidebarDebugger
			m.focus = FocusDebugger
			m.status = "Debugging started"
			m.relayout()
		}
		return m, nil
	case debugStopMsg:
		// Stop debugging
		if m.debugMgr.IsRunning() {
			m.debugMgr.Stop()
			m.debuggerPanel.SetState(dap.StateInactive)
			m.status = "Debugging stopped"
		}
		return m, nil
	case quitMsg:
		m.lspMgr.ShutdownAll()
		return m, tea.Quit
	}
	return m, nil
}
