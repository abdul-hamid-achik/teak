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

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"charm.land/bubbles/v2/spinner"
	"teak/internal/editor"
	"teak/internal/filetree"
	"teak/internal/git"
	"teak/internal/lsp"
	"teak/internal/search"
	"teak/internal/text"
	"teak/internal/ui"
)

// FocusArea indicates which panel has focus.
type FocusArea int

const (
	FocusEditor FocusArea = iota
	FocusTree
	FocusGitPanel
)

// Model is the root Bubbletea model.
type Model struct {
	editors      []editor.Editor
	activeTab    int
	tabBar       editor.TabBar
	tree         filetree.Model
	theme        ui.Theme
	status       string
	width        int
	height       int
	showHelp     bool
	helpM        editor.HelpModel
	showTree     bool
	showSearch   bool
	searchMode   search.Mode
	searchM      search.Model
	focus        FocusArea
	rootDir      string
	lspMgr       *lsp.Manager
	goToLineMode    bool
	goToLineInput   string
	welcome         *editor.Welcome
	treeContextMenu editor.ContextMenu
	treeContextPath string
	renameMode      bool
	renameInput     string
	pendingCursor    *text.Position // cursor to set after async file load
	fileDiagnostics  map[string]int // path → worst severity (1=error, 2=warn, 3=info, 4=hint)
	dirDiagnostics   map[string]int // dir path → worst child severity
	gitBranch        string         // current git branch name
	gitPanel         git.Model      // git sidebar panel
	watcher          *fileWatcher   // watches files/dirs for external changes
	newFileMode      bool           // input mode for new file name
	newFolderMode    bool           // input mode for new folder name
	newItemInput     string         // input buffer for new file/folder name
	newItemDir       string         // directory to create new item in
	deleteConfirm    bool           // confirming deletion
	deleteTarget     string         // path to delete
}

// NewModel creates a new app model, optionally loading a file.
func NewModel(filePath string, rootDir string) (Model, error) {
	// Suppress LSP log output from corrupting TUI
	log.SetOutput(io.Discard)

	theme := ui.DefaultTheme()
	cfg := editor.DefaultConfig()
	buf := text.NewBuffer()
	if filePath != "" {
		buf.FilePath = filePath
		cfg.CommentPrefix = editor.CommentPrefixForFile(filePath)
	}

	m := Model{
		theme:           theme,
		rootDir:         rootDir,
		tabBar:          editor.NewTabBar(theme),
		lspMgr:          lsp.NewManager(rootDir),
		treeContextMenu: editor.NewContextMenu(theme),
		fileDiagnostics: make(map[string]int),
		dirDiagnostics:  make(map[string]int),
		gitBranch:       detectGitBranch(rootDir),
		gitPanel:        git.New(rootDir, theme),
		helpM:           editor.NewHelpModel(theme),
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

		// Welcome screen: global shortcuts pass through, others dismiss
		if m.welcome != nil && m.welcome.Active {
			key := msg.String()
			switch key {
			case "ctrl+q", "ctrl+b", "ctrl+f", "ctrl+shift+f", "f1":
				// Let these fall through to normal handling
			default:
				m.welcome.Dismiss()
				// Let the key fall through to normal handling
			}
		}

		switch msg.String() {
		case "ctrl+q":
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
		case "ctrl+shift+f":
			return m.openSearch(search.ModeSemantic)
		case "ctrl+space":
			return m, m.requestCompletion()
		case "f12":
			return m, m.requestDefinition()
		case "ctrl+w":
			return m.closeCurrentTab()
		case "ctrl+g":
			m.goToLineMode = true
			m.goToLineInput = ""
			return m, nil
		case "ctrl+shift+g":
			if m.gitPanel.IsGitRepo() {
				m.gitPanel.ToggleCollapsed()
				m.relayout()
			}
			return m, nil
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
		}

	case tea.MouseClickMsg:
		// Search overlay captures all mouse clicks when visible
		if m.showSearch {
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

		if m.showTree {
			treeWidth := m.treeWidth()
			if mouse.X < treeWidth {
				// Right-click in tree area
				if mouse.Button == tea.MouseRight {
					return m.showTreeContextMenu(mouse.X, mouse.Y)
				}
				m.focus = FocusTree
				var cmd tea.Cmd
				m.tree, cmd = m.tree.Update(msg)
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
		if m.showSearch {
			return m.updateSearch(msg)
		}
		if m.showHelp {
			var cmd tea.Cmd
			m.helpM, cmd = m.helpM.Update(msg)
			return m, cmd
		}
		mouse := msg.Mouse()
		if m.showTree {
			treeWidth := m.treeWidth()
			if mouse.X < treeWidth {
				var cmd tea.Cmd
				m.tree, cmd = m.tree.Update(msg)
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
		if m.showSearch {
			var cmd tea.Cmd
			m.searchM, cmd = m.searchM.Update(msg)
			return m, cmd
		}
		return m, nil

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

	case git.RefreshMsg:
		var cmd tea.Cmd
		m.gitPanel, cmd = m.gitPanel.Update(msg)
		// Also update the status bar branch display
		if msg.Branch != "" {
			m.gitBranch = msg.Branch
		}
		return m, cmd

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

	case lsp.DefinitionResultMsg:
		if len(msg.Locations) > 0 {
			loc := msg.Locations[0]
			path := lsp.URIToPath(loc.URI)
			pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
			m.pendingCursor = &pos
			return m.openFilePinned(path)
		}
		return m, nil

	case lsp.ReferencesResultMsg:
		if len(msg.Locations) > 0 {
			loc := msg.Locations[0]
			path := lsp.URIToPath(loc.URI)
			pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
			m.pendingCursor = &pos
			model, cmd := m.openFile(path)
			m2 := model.(Model)
			m2.status = fmt.Sprintf("Found %d reference(s)", len(msg.Locations))
			return m2, cmd
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

	case lspMsg:
		if msg.msg == nil {
			return m, m.listenLSP()
		}
		result, cmd := m.Update(msg.msg)
		m = result.(Model)
		return m, tea.Batch(cmd, m.listenLSP())
	}

	// Route input to focused panel
	if m.showTree && m.focus == FocusTree {
		var cmd tea.Cmd
		m.tree, cmd = m.tree.Update(msg)
		return m, cmd
	}
	if m.focus == FocusGitPanel {
		var cmd tea.Cmd
		m.gitPanel, cmd = m.gitPanel.Update(msg)
		return m, cmd
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
			client.DidChange(
				lsp.FileURI(ed.Buffer.FilePath),
				ed.Buffer.Version(),
				ed.Buffer.Content(),
			)
		}
	}

	return m, cmd
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
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
		} else if m.activeEditor() != nil {
			editorView = m.activeEditor().View()
		}
		content = tabBarView + "\n" + editorView + "\n" + statusBar
	}

	// Overlay context menus (rendered before help/search so they show in normal view)
	if m.activeEditor() != nil && m.activeEditor().IsContextMenuVisible() {
		cmView := m.activeEditor().ContextMenuView()
		cmX, cmY := m.activeEditor().ContextMenuPosition()
		if m.showTree {
			cmX += m.treeWidth() + 1
		}
		cmY += 1 // +1 for tab bar
		content = ui.PlaceOverlayAt(content, cmView, cmX, cmY, m.width, m.height)
	} else if m.treeContextMenu.Visible {
		cmView := m.treeContextMenu.View()
		content = ui.PlaceOverlayAt(content, cmView, m.treeContextMenu.X, m.treeContextMenu.Y, m.width, m.height)
	}

	// Overlay help, search, or go-to-line
	if m.showHelp {
		helpContent := m.helpM.View()
		content = ui.RenderOverlay(content, helpContent, m.width, m.height)
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

	scanned := zone.Scan(content)
	v := tea.NewView(scanned)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	if !m.showHelp && !m.showSearch && !m.renameMode && !welcomeActive && m.focus == FocusEditor && m.activeEditor() != nil {
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
			client.DidChange(
				lsp.FileURI(ed.Buffer.FilePath),
				ed.Buffer.Version(),
				ed.Buffer.Content(),
			)
		}
	}
	return m, cmd
}

// viewWithTree: tree takes full height (no tab bar above it).
// Tab bar sits above the editor only.
func (m Model) viewWithTree() string {
	tabBarView := m.tabBar.View()
	var editorView string
	if m.welcome != nil && m.welcome.Active {
		editorView = m.welcome.View()
	} else if m.activeEditor() != nil {
		editorView = m.activeEditor().View()
	}

	// Editor column: tab bar + editor content
	editorColumn := tabBarView + "\n" + editorView

	// Build sidebar: tree + optional git panel
	sidebarHeight := m.height - 2 // minus divider + status bar
	var sidebarView string

	if m.gitPanel.IsGitRepo() && !m.gitPanel.Collapsed {
		// Split sidebar between tree and git panel
		gitPanelHeight := min(8, sidebarHeight/3)
		if gitPanelHeight < 1 {
			gitPanelHeight = 1
		}
		separatorHeight := 1
		treeHeight := sidebarHeight - gitPanelHeight - separatorHeight
		if treeHeight < 1 {
			treeHeight = 1
		}

		// Resize tree and git panel
		tw := m.treeWidth()
		m.tree.SetSize(tw, treeHeight)
		m.gitPanel.SetSize(tw, gitPanelHeight)

		separator := m.theme.TreeBorder.Render(strings.Repeat("─", tw))
		sidebarView = m.tree.View() + "\n" + separator + "\n" + m.gitPanel.View()
	} else {
		sidebarView = m.tree.View()
		if m.gitPanel.IsGitRepo() && m.gitPanel.Collapsed {
			// Show collapsed header in one line at the bottom
			tw := m.treeWidth()
			m.gitPanel.SetSize(tw, 1)
			separator := m.theme.TreeBorder.Render(strings.Repeat("─", tw))
			sidebarView = m.tree.View() + "\n" + separator + "\n" + m.gitPanel.View()
		}
	}

	// Border column: full height
	borderLines := make([]string, sidebarHeight)
	for i := range sidebarHeight {
		borderLines[i] = m.theme.TreeBorder.Render("│")
	}
	borderCol := strings.Join(borderLines, "\n")

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, borderCol, editorColumn)
}

func (m Model) treeWidth() int {
	tw := m.width / 4
	if tw > 30 {
		tw = 30
	}
	if tw < 15 {
		tw = 15
	}
	return tw
}

func (m *Model) relayout() {
	statusHeight := 2 // divider + status bar
	tabBarHeight := 1

	m.tabBar.Width = m.width // will be constrained when tree is shown

	if m.showTree {
		tw := m.treeWidth()
		editorWidth := m.width - tw - 1 // -1 for border
		if editorWidth < 1 {
			editorWidth = 1
		}
		sidebarHeight := m.height - statusHeight
		editorHeight := m.height - statusHeight - tabBarHeight
		if sidebarHeight < 1 {
			sidebarHeight = 1
		}
		if editorHeight < 1 {
			editorHeight = 1
		}

		// Calculate tree height accounting for git panel
		treeHeight := sidebarHeight
		if m.gitPanel.IsGitRepo() {
			if m.gitPanel.Collapsed {
				// Collapsed: separator (1) + header (1) = 2
				treeHeight = sidebarHeight - 2
			} else {
				// Expanded: separator (1) + panel height
				gitPanelHeight := min(8, sidebarHeight/3)
				if gitPanelHeight < 1 {
					gitPanelHeight = 1
				}
				treeHeight = sidebarHeight - gitPanelHeight - 1
			}
			if treeHeight < 1 {
				treeHeight = 1
			}
		}

		m.tree.SetSize(tw, treeHeight)
		m.tabBar.Width = editorWidth
		for i := range m.editors {
			m.editors[i].SetSize(editorWidth, editorHeight)
		}
		if m.welcome != nil {
			m.welcome.SetSize(editorWidth, editorHeight)
		}
	} else {
		editorHeight := m.height - statusHeight - tabBarHeight
		if editorHeight < 1 {
			editorHeight = 1
		}
		m.tabBar.Width = m.width
		for i := range m.editors {
			m.editors[i].SetSize(m.width, editorHeight)
		}
		if m.welcome != nil {
			m.welcome.SetSize(m.width, editorHeight)
		}
	}
}

func (m Model) renderStatusBar() string {
	// Left: F1 Help + git branch (or project name fallback)
	helpHint := m.theme.TabInactive.Render(" F1 Help ")
	var branchPart string
	if m.gitBranch != "" {
		branchPart = fmt.Sprintf("  %s", m.gitBranch)
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
		right = m.theme.StatusText.Render(
			fmt.Sprintf(" Ln %d, Col %d  %s  LF  UTF-8  %s%s ",
				buf.Cursor.Line+1, buf.Cursor.Col+1, tabInfo, scrollPos, lspStatus),
		)
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

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.searchM, cmd = m.searchM.Update(msg)
	return m, cmd
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

// LSP helpers

type lspMsg struct {
	msg tea.Msg
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
		if err != nil || len(locs) == 0 {
			return nil
		}
		return lsp.DefinitionResultMsg{Locations: locs}
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
		if err != nil || len(locs) == 0 {
			return nil
		}
		return lsp.ReferencesResultMsg{Locations: locs}
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
				// Sort reverse by position (later edits first)
				for a := 0; a < len(sortedEdits); a++ {
					for b := a + 1; b < len(sortedEdits); b++ {
						if sortedEdits[a].StartLine < sortedEdits[b].StartLine ||
							(sortedEdits[a].StartLine == sortedEdits[b].StartLine && sortedEdits[a].StartCol < sortedEdits[b].StartCol) {
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
