package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	log "github.com/charmbracelet/log"
	"github.com/charmbracelet/x/ansi"
	sdk "github.com/coder/acp-go-sdk"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/agent"
	agentruntime "teak/internal/agent/runtime"
	"teak/internal/codemap"
	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/debugger"
	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/editor/overlays"
	"teak/internal/execpolicy"
	"teak/internal/filetree"
	"teak/internal/git"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/plugin"
	"teak/internal/problems"
	"teak/internal/procmon"
	"teak/internal/search"
	"teak/internal/session"
	"teak/internal/settings"
	"teak/internal/text"
	"teak/internal/toolpath"
	"teak/internal/ui"
	"teak/internal/vault"
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

// maxLSPRestarts bounds automatic relaunches of a language server that keeps
// crashing; beyond it the exit is reported and the server stays down until
// the user restarts it from the palette.
const maxLSPRestarts = 3

// setFocus moves keyboard focus to area, releasing whatever the area being left
// was holding.
//
// Focus used to be assigned by writing m.focus directly from more than fifty
// places, only a handful of which remembered to release the previous area. That
// left the agent panel showing a phantom caret after focus moved elsewhere, and
// left the git commit box internally focused so it silently ate navigation keys
// once the user came back to it. Routing every transition through here keeps
// that bookkeeping in one place.
//
// Entering an area is deliberately not handled here: the sites that focus the
// agent panel need its tea.Cmd, and returning one from this method would force
// every caller to thread a command it does not otherwise need.
func (m *Model) setFocus(area FocusArea) {
	if m.focus == area {
		return
	}
	switch m.focus {
	case FocusTree:
		// The tree filter owns keyboard input only while the tree is focused.
		// Closing it when focus leaves prevents a stale prompt from remaining
		// visible while Escape and text are routed to the editor.
		m.tree.ClearFilter()
	case FocusAgent:
		m.agentPanel.Blur()
	case FocusGitPanel:
		m.gitPanel.UnfocusCommit()
	}
	m.focus = area
}

// sizeEditors gives each editor the dimensions of the area it is rendered into.
//
// Every editor used to be sized to the full editor width even while split, and
// the panes were merely clipped at render time. The editors therefore laid out
// and scrolled against a width wider than the user could see, so the cursor
// could sit in a clipped-away column.
func (m *Model) sizeEditors(editorWidth, editorHeight int) {
	for i := range m.editors {
		width, height := editorWidth, editorHeight
		if m.split.enabled {
			switch i {
			case m.split.firstTab:
				width = m.split.paneAWidth(editorWidth)
				height = m.split.paneAHeight(editorHeight)
			case m.split.secondTab:
				width = m.split.paneBWidth(editorWidth)
				height = m.split.paneBHeight(editorHeight)
			}
		}
		// The in-buffer find widget renders as the editor's first row; give
		// the text area one row less so the widget never pushes the status
		// bar out of the frame.
		if m.editors[i].IsFindVisible() {
			height = max(1, height-1)
		}
		m.editors[i].SetSize(width, height)
	}
}

// textInputFocused reports whether a text field currently owns typing.
//
// bubbles' textinput and textarea bind the emacs-style chords ctrl+w (delete
// word back), ctrl+f / ctrl+b (character forward/back), ctrl+h (backspace) and
// ctrl+k (kill to end of line). Those chords are also global shortcuts here, and
// the global handler runs first, so pressing ctrl+w while typing a commit
// message or a search query closed the current tab instead of deleting a word.
func (m *Model) textInputFocused() bool {
	if m.agentPanel.IsInputFocused() {
		return true
	}
	if m.gitPanel.IsTitleFocused() || m.gitPanel.IsBodyFocused() {
		return true
	}
	if ed := m.activeEditor(); ed != nil && ed.IsFindVisible() {
		return true
	}
	return false
}

// sidebarFocus returns the focus area that owns keyboard input for the sidebar
// tab currently on screen. Focus and the visible tab must agree, otherwise keys
// drive a panel the user cannot see.
func (m *Model) sidebarFocus() FocusArea {
	switch m.sidebarTab {
	case SidebarGit:
		return FocusGitPanel
	case SidebarProblems:
		return FocusProblems
	case SidebarDebugger:
		return FocusDebugger
	default:
		return FocusTree
	}
}

// SidebarTab indicates which tab is active in the sidebar.
type SidebarTab int

const (
	SidebarFiles SidebarTab = iota
	SidebarGit
	SidebarProblems
	SidebarDebugger
)

// Model is the root Bubble Tea value passed through tea.Model.
//
// It is intentionally a small handle. Bubble Tea boxes the value returned by
// every Update in an interface, so keeping the complete UI graph directly in
// Model used to copy roughly 650 KiB for every event. The mutable graph lives
// in modelState, which has exactly one owner: the running Bubble Tea program.
//
// A copied Model aliases that owner; it is not a state snapshot. Commands must
// capture only immutable document snapshots and scalar request metadata, never
// a Model, when their result depends on the state at dispatch time. NewModel
// always allocates a distinct modelState. The zero value is supported only for
// View/Update safety and is initialized lazily by Update.
type Model struct {
	*modelState
}

// modelState holds the root UI graph. Keep it private so direct state owners
// can only be created inside this package; copying Model is explicitly an
// aliasing operation, not a fork.
type modelState struct {
	editors                   []editor.Editor
	activeTab                 int
	split                     splitLayout
	tabBar                    editor.TabBar
	tree                      filetree.Model
	theme                     ui.Theme
	activeThemeName           string // theme represented by the live, cached style models
	status                    string
	width                     int
	height                    int
	showHelp                  bool
	helpM                     editor.HelpModel
	showTree                  bool
	showSearch                bool
	searchMode                search.Mode
	searchM                   search.Model
	focus                     FocusArea
	rootDir                   string
	lspMgr                    *lsp.Manager
	lspRestarts               map[string]int // per-server crash restarts this session
	goToLineMode              bool
	goToLineInput             string
	welcome                   *editor.Welcome
	treeContextMenu           editor.ContextMenu
	treeContextPath           string
	renameMode                bool
	renameInput               string
	pendingCursor             *pendingNavigation         // navigation tied to a target path
	startupCursors            map[string]text.Position   // CLI/startup line:col applied after first load
	startupFiles              []StartupFile              // CLI files merged with session restore
	pendingFileLoads          map[uint64]pendingFileLoad // request identity -> target editor
	nextFileLoadID            uint64
	pendingDiffLoads          map[uint64]pendingDiffLoad // request identity -> target editor
	nextDiffLoadID            uint64
	restoredTabs              map[uint64]session.TabState     // editor identity -> state applied after load
	sessionRestoreCtx         context.Context                 // cancellation token owned by startup restoration
	sessionRestoreCancel      context.CancelFunc              // cancels a superseded disk scan
	sessionRestoreGeneration  uint64                          // rejects late session load results
	sessionRestoreEligible    bool                            // false after user-created UI state wins
	sessionUnrestoredTabs     []session.TabState              // tabs that failed to restore; kept in snapshots so they are not silently dropped
	recoverySuppressed        bool                            // user quit without saving; final recovery write clears instead of persisting
	sidebarDragging           bool                            // sidebar divider drag in progress
	sidebarDragStartX         int                             // pointer X when the sidebar drag started
	sidebarDragStartWidth     int                             // sidebar width when the sidebar drag started
	fileDiagnostics           map[string]int                  // path → worst severity (1=error, 2=warn, 3=info, 4=hint)
	dirDiagnostics            map[string]int                  // dir path → worst child severity
	dirDiagnosticCounts       map[string]map[int]int          // dir path → severity reference counts
	treeDiagnostics           map[string]int                  // file and directory diagnostics shared with the tree
	gitBranch                 string                          // current git branch name
	gitPanel                  git.Model                       // git sidebar panel
	watcher                   *fileWatcher                    // watches files/dirs for external changes
	externalConflicts         map[string]externalFileConflict // dirty buffers changed on disk; saving requires a decision
	externalConflictPrompt    string                          // path owned by the currently visible external-conflict dialog
	externalConflictBytes     int                             // aggregate retained immutable conflict snapshots
	externalConflictGen       uint64                          // rejects stale async conflict re-reads
	externalChangeObserved    map[string]uint64               // newest applied watcher observation per path
	lastSaveWatcherWatermarks map[string]uint64               // observations older than a completed Teak save
	externalReads             externalReadState               // one bounded latest-wins disk re-read at a time
	externalFileReader        externalFileReader              // testable boundary; defaults to readEditorFile
	newFileMode               bool                            // input mode for new file name
	newFolderMode             bool                            // input mode for new folder name
	newItemInput              string                          // input buffer for new file/folder name
	newItemDir                string                          // directory to create new item in
	treeRenameMode            bool                            // input mode for filesystem rename
	treeCopyMode              bool                            // input mode for filesystem duplicate
	treeMoveMode              bool                            // input mode for workspace-relative move destination
	treeEditInput             string                          // input for the active filesystem tree operation
	treeEditTarget            string                          // source path for the active filesystem tree operation
	deleteConfirm             bool                            // confirming deletion
	deleteTarget              string                          // path to delete
	diffViews                 map[int]diff.Model              // tab index → diff view model
	sidebarTab                SidebarTab                      // active sidebar tab
	showBranchPicker          bool                            // branch picker overlay visible
	branchPickerM             git.BranchPickerModel           // branch picker model
	gitContextMenu            editor.ContextMenu              // context menu for git panel
	gitContextEntry           *git.StatusEntry                // entry right-clicked in git panel
	gitContextStaged          bool                            // whether the right-clicked entry is in staged section
	gitContextPath            string                          // path of right-clicked entry (file or dir)
	unsavedConfirm            *overlay.Confirm                // unsaved changes dialog shown on quit
	quitApproved              bool                            // allows the terminal tea.QuitMsg through QuitFilter
	shutdownStarted           bool                            // cleanup runs asynchronously before terminal quit
	sessionSaves              sessionSaveState                // one in-flight disk write plus a latest-wins snapshot
	sessionSaver              func(session.State) error       // session.Save in production; replaceable by tests
	overlayStack              overlay.Stack                   // stack for picker overlays (quick open, command palette)
	healthDashboard           *healthDashboardOverlay         // read-only workspace health overlay
	healthDashboardGeneration uint64
	healthDashboardCancel     context.CancelFunc
	healthDashboardRunner     healthDashboardRunner         // testable command boundary
	pluginFloats              map[int]*overlay.Float        // plugin-owned read-only floating panels
	cachedFiles               []string                      // cached file list for quick open
	cachedFilesReady          bool                          // true after file list has been loaded
	fileListGeneration        int                           // invalidates stale async file scans
	fileListCancel            context.CancelFunc            // stops a superseded Quick Open scan
	problemsPanel             problems.Model                // problems panel for diagnostics
	showSettings              bool                          // settings overlay visible
	settingsM                 settings.Model                // settings editor model
	settingsSaving            bool                          // settings persistence is in flight
	closedTabs                []ClosedTab                   // history of closed tabs for reopening
	debuggerPanel             debugger.Model                // debugger panel
	debugMgr                  *dap.Manager                  // debug session manager
	procMon                   *procmon.Monitor              // process resource monitor
	debugRunner               debugRunner                   // testable boundary for blocking DAP commands
	debugLifecycle            debugLifecycleState           // DAP command generation and pending state
	breakpoints               map[string][]breakpointEntry  // file path → sorted breakpoint entries (0-based)
	debugGutterBreakpoints    map[string]*editor.GutterOpts // projection cache rebuilt only on breakpoint/execution state changes
	currentExecFile           string                        // file with current execution point
	currentExecLine           int                           // current execution line (0-based), -1 when not paused
	showAgent                 bool                          // agent panel visible
	agentPanel                agent.Model                   // agent chat panel
	agentWrites               agentWriteState               // serializes accepted/rejected ACP write decisions
	agentWriteRoot            *os.Root                      // pinned workspace root for confined ACP writes
	pluginMgr                 *plugin.Manager               // Lua plugin manager
	pluginLoadGeneration      uint64                        // rejects stale asynchronous Lua loads
	pluginLoading             bool                          // true until the startup Lua load returns
	pluginKeySequence         string                        // pending plugin key sequence
	pluginKeyBuffer           []tea.KeyPressMsg             // raw keys consumed by a pending plugin sequence
	pluginFeedDepth           int                           // nested synthetic key dispatch from plugins
	acpMgr                    *acp.Manager                  // ACP agent manager
	coordinator               *Coordinator                  // orchestrates LSP/DAP/ACP coordinators
	logFile                   *os.File                      // log file handle for cleanup
	pendingCloseTab           int                           // tab index pending close-unsaved confirm (-1 = none)
	untitledCounter           int                           // counter for "Untitled-N" tabs
	saveAsMode                bool                          // save-as input mode
	saveAsInput               string                        // save-as path input buffer
	saveAsDestinationPromptID int                           // pending Save As overwrite confirmation, 0 when none
	lastSearchResults         []search.Result               // saved results from last search
	lastSearchIndex           int                           // current index in lastSearchResults
	pendingSaves              map[int]pendingSaveRequest    // request id -> save continuation state
	nextSaveRequestID         int                           // monotonically increasing save request id
	appCfg                    config.Config                 // app config for feature flags
	gitRefreshGeneration      int
	codemapGeneration         uint64
	codemapCancel             context.CancelFunc
	treeRefreshGeneration     uint64
	treeRefreshCancel         context.CancelFunc
	treeActionGeneration      uint64             // identity for the one serialized filesystem mutation
	treeActionCancel          context.CancelFunc // stops work only during shutdown or before logical commit
	treeActionInFlight        bool               // destructive tree actions are rejected while one is active
	overlayRequests           overlayRequestTracker
	documentRequests          documentRequestTracker
	workspaceEdits            workspaceEditState // serial background workspace-edit preparation/commit
	replaces                  replaceAsyncState  // latest-wins background search/replace preparation
	lspChanges                *lspChangePreparer // serializes immutable full-text preparation per LSP document
	codeActionRequester       codeActionRequester
	codeActionCommandGen      uint64
	codeActionCommandStop     context.CancelFunc
}

// ensureState makes the zero Model safe at the Bubble Tea boundary. It is not
// used for normal construction: NewModel installs the fully configured state
// graph. Keeping this minimal avoids doing I/O or starting subsystems for a
// zero-value view used by tests and embedding hosts.
func (m Model) ensureState() Model {
	if m.modelState != nil {
		return m
	}
	theme := ui.DefaultTheme()
	m.modelState = &modelState{
		theme:                     theme,
		tabBar:                    editor.NewTabBar(theme),
		pendingCloseTab:           -1,
		pendingSaves:              make(map[int]pendingSaveRequest),
		pendingFileLoads:          make(map[uint64]pendingFileLoad),
		pendingDiffLoads:          make(map[uint64]pendingDiffLoad),
		restoredTabs:              make(map[uint64]session.TabState),
		externalChangeObserved:    make(map[string]uint64),
		lastSaveWatcherWatermarks: make(map[string]uint64),
		fileDiagnostics:           make(map[string]int),
		dirDiagnostics:            make(map[string]int),
		dirDiagnosticCounts:       make(map[string]map[int]int),
		treeDiagnostics:           make(map[string]int),
		breakpoints:               make(map[string][]breakpointEntry),
		debugGutterBreakpoints:    make(map[string]*editor.GutterOpts),
		currentExecLine:           -1,
		pluginFloats:              make(map[int]*overlay.Float),
		nextSaveRequestID:         1,
		lspChanges:                newLSPChangePreparer(),
	}
	return m
}

// ClosedTab stores information about a closed tab for reopening.
type ClosedTab struct {
	FilePath string
	Label    string
}

type gitRefreshDebounceMsg struct {
	generation int
}

// Replace All is prepared outside the UI update path, but remains bounded so a
// generated/minified file cannot reserve excessive transient memory.
const (
	maxReplaceAllBytes    = 8 << 20
	maxReplaceAllMatches  = 10_000
	maxReplaceResultBytes = 64 << 20
)

// NewModel creates a new app model, optionally loading a file.
// StartupFile is an initial document requested at editor launch (CLI paths).
// Line/Col are 0-based. A negative Line means no preferred cursor position.
type StartupFile struct {
	Path string
	Line int
	Col  int
}

// NewModel creates the root application model. filePath may be empty (welcome /
// session restore) or a single absolute file path to open.
func NewModel(filePath string, rootDir string, appCfg config.Config) (Model, error) {
	var files []StartupFile
	if filePath != "" {
		files = []StartupFile{{Path: filePath, Line: -1}}
	}
	return NewModelWithFiles(files, rootDir, appCfg)
}

// NewModelWithFiles creates the root model with zero or more initial file tabs.
// The first file is active. An empty slice shows the welcome screen and may
// restore the previous session for rootDir.
func NewModelWithFiles(files []StartupFile, rootDir string, appCfg config.Config) (Model, error) {
	if err := appCfg.Validate(); err != nil {
		return Model{}, fmt.Errorf("invalid config: %w", err)
	}
	// Apply explicit tool overrides before managers or startup commands can
	// resolve an LSP, agent, codemap, or auxiliary executable.
	toolpath.Configure(appCfg.Tools)

	// Configure charmbracelet logger to file (not stderr, which Bubbletea owns)
	logFile, err := openPrivateLogFile(os.Getenv("HOME"))
	if err != nil {
		logFile = nil // fall back to discarding logs rather than corrupting TUI
	}
	var logWriter *os.File
	if logFile != nil {
		logWriter = logFile
	}
	logger := log.NewWithOptions(logWriter, log.Options{
		Prefix:     "teak",
		Level:      log.InfoLevel,
		TimeFormat: "15:04:05",
	})
	log.SetDefault(logger)
	// Force the logger's lazy colour-profile detection to run once, here,
	// before newFileWatcher or any manager below can start a goroutine that
	// might log. See warmUpStyledLogger for why this prevents a data race.
	warmUpStyledLogger(logger, logFile)

	theme := ui.ThemeByName(appCfg.UI.Theme)
	cfg := editor.Config{
		TabSize:      appCfg.Editor.TabSize,
		InsertTabs:   appCfg.Editor.InsertTabs,
		AutoIndent:   appCfg.Editor.AutoIndent,
		WordWrap:     appCfg.Editor.WordWrap,
		ScrollMargin: appCfg.Editor.ScrollMargin,
	}

	// Drop blank path entries; keep order for tab layout.
	cleaned := make([]StartupFile, 0, len(files))
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		if f.Line < 0 {
			f.Line = -1
		}
		cleaned = append(cleaned, f)
	}
	files = cleaned

	// Build LSP configs: merge user overrides with defaults
	var lspConfigs []lsp.ServerConfig
	for _, lc := range appCfg.LSP {
		lspConfigs = append(lspConfigs, lsp.ServerConfig{
			Extensions: lc.Extensions,
			Command:    lc.Command,
			Args:       lc.Args,
			LanguageID: lc.LanguageID,
			Env:        cloneStringMap(lc.Env),
		})
	}

	m := Model{modelState: &modelState{
		theme:                     theme,
		activeThemeName:           appCfg.UI.Theme,
		rootDir:                   rootDir,
		tabBar:                    editor.NewTabBar(theme),
		split:                     defaultSplitLayout(),
		lspMgr:                    lsp.NewManager(rootDir, lspConfigs),
		treeContextMenu:           editor.NewContextMenu(theme),
		fileDiagnostics:           make(map[string]int),
		dirDiagnostics:            make(map[string]int),
		dirDiagnosticCounts:       make(map[string]map[int]int),
		treeDiagnostics:           make(map[string]int),
		logFile:                   logFile,
		pendingCloseTab:           -1,
		pendingSaves:              make(map[int]pendingSaveRequest),
		startupCursors:            make(map[string]text.Position),
		pendingFileLoads:          make(map[uint64]pendingFileLoad),
		pendingDiffLoads:          make(map[uint64]pendingDiffLoad),
		restoredTabs:              make(map[uint64]session.TabState),
		externalChangeObserved:    make(map[string]uint64),
		lastSaveWatcherWatermarks: make(map[string]uint64),
		nextSaveRequestID:         1,
		appCfg:                    appCfg,
		sessionSaver:              session.Save,
		gitBranch:                 "",
		gitPanel:                  git.New(rootDir, theme),
		branchPickerM:             git.NewBranchPicker(theme),
		gitContextMenu:            editor.NewContextMenu(theme),
		helpM:                     editor.NewHelpModel(theme),
		problemsPanel:             problems.New(theme, rootDir),
		settingsM:                 settings.New(theme, appCfg, config.ConfigPath()),
		debuggerPanel:             debugger.New(theme),
		debugMgr:                  dap.NewManager(rootDir),
		procMon:                   procmon.New(),
		breakpoints:               make(map[string][]breakpointEntry),
		debugGutterBreakpoints:    make(map[string]*editor.GutterOpts),
		currentExecLine:           -1,
		pluginFloats:              make(map[int]*overlay.Float),
		agentPanel:                agent.New(theme),
		pluginLoadGeneration:      1,
		pluginLoading:             true,
		lspChanges:                newLSPChangePreparer(),
	}}
	if rootDir != "" {
		m.agentWriteRoot, err = os.OpenRoot(rootDir)
		if err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			return Model{}, fmt.Errorf("open workspace root: %w", err)
		}
	}

	// Initialize ACP manager if agent is configured. The runtime store is
	// workspace-scoped and shared with `teak headless agent list`, so an
	// interactive prompt remains observable after the UI exits or restarts.
	if appCfg.Agent.Enabled && appCfg.Agent.Command != "" {
		var runManager *agentruntime.Manager
		if rootDir != "" {
			storePath := agentruntime.ResolveWorkspaceStorePath(rootDir, session.StateHome())
			runManager, err = agentruntime.NewManager(agentruntime.ManagerConfig{
				Store: agentruntime.FileStore{Path: storePath},
			})
			if err != nil {
				if logFile != nil {
					_ = logFile.Close()
				}
				return Model{}, fmt.Errorf("load agent runtime: %w", err)
			}
		}
		sandboxMode := execpolicy.Mode(appCfg.Agent.Sandbox)
		if sandboxMode == "" {
			sandboxMode = execpolicy.ModeAuto
		}
		m.acpMgr = acp.NewManagerWithRuntimeAndPolicy(
			rootDir,
			appCfg.Agent.Command,
			appCfg.Agent.Args,
			runManager,
			execpolicy.Policy{Root: rootDir, Mode: sandboxMode},
		)
	}
	// Create coordinator to orchestrate LSP/DAP/ACP
	m.coordinator = NewCoordinator(m.lspMgr, m.debugMgr, m.acpMgr)

	if rootDir != "" {
		m.tree = filetree.NewEmpty(rootDir, theme)
		m.tree.SetDiagnostics(m.treeDiagnostics)
		if w, err := newFileWatcher(rootDir); err == nil {
			m.watcher = w
		}
	}

	// Show tree based on config
	m.showTree = appCfg.UI.ShowTree

	if len(files) == 0 {
		// Empty buffer + welcome; session I/O is deferred to Init.
		buf := text.NewBuffer()
		ed := editor.New(buf, theme, cfg)
		m.editors = append(m.editors, ed)
		m.tabBar.AddTab("untitled", "")
		m.activeTab = 0

		w := editor.NewWelcome(theme)
		m.welcome = &w
		m.showTree = true
		m.setFocus(FocusTree)
		if appCfg.Session.Enabled && rootDir != "" {
			m.sessionRestoreCtx, m.sessionRestoreCancel = context.WithCancel(context.Background())
			m.sessionRestoreGeneration = 1
			m.sessionRestoreEligible = true
		}
		return m, nil
	}

	// One tab per startup file; first is active. Session restore (when enabled)
	// may later expand this set with previously open tabs for the same root.
	m.startupFiles = append([]StartupFile(nil), files...)
	for i, f := range files {
		fileCfg := cfg
		fileCfg.CommentPrefix = editor.CommentPrefixForFile(f.Path)
		buf := text.NewBuffer()
		buf.FilePath = f.Path
		ed := editor.New(buf, theme, fileCfg)
		m.editors = append(m.editors, ed)
		m.tabBar.AddTab(filepath.Base(f.Path), f.Path)
		if i > 0 {
			// Extra startup files are pinned so preview-replace does not drop them.
			m.tabBar.PinTab(i)
		}
		if f.Line >= 0 {
			m.startupCursors[filepath.Clean(f.Path)] = text.Position{Line: f.Line, Col: max(0, f.Col)}
		}
	}
	m.activeTab = 0
	m.setFocus(FocusEditor)

	// Still restore the workspace session so other tabs come back; the CLI file
	// stays active and its line:col overrides the saved cursor for that path.
	if appCfg.Session.Enabled && rootDir != "" {
		m.sessionRestoreCtx, m.sessionRestoreCancel = context.WithCancel(context.Background())
		m.sessionRestoreGeneration = 1
		m.sessionRestoreEligible = true
	}

	// Config load problems are invisible once the alternate screen takes over;
	// give the user one startup notice instead of silently keeping defaults.
	if len(appCfg.LoadWarnings) > 0 {
		m.status = "Config warnings: " + strings.Join(appCfg.LoadWarnings, "; ")
		log.Warn("config loaded with warnings", "warnings", appCfg.LoadWarnings)
	}

	return m, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// cleanup closes resources before quitting.
func (m *Model) cleanup() {
	cleanupModelState(m.modelState)
}

// cleanupModelState is intentionally state-only so the asynchronous shutdown
// command never captures a live Model handle. At this point the state is no
// longer routed through Update and is exclusively owned by teardown.
func cleanupModelState(m *modelState) {
	if m == nil {
		return
	}
	m.overlayRequests.cancelAll()
	m.documentRequests.cancelAll()
	if m.sessionRestoreCancel != nil {
		m.sessionRestoreCancel()
		m.sessionRestoreCancel = nil
	}
	if m.treeRefreshCancel != nil {
		m.treeRefreshCancel()
		m.treeRefreshCancel = nil
	}
	if m.treeActionCancel != nil {
		m.treeActionCancel()
		m.treeActionCancel = nil
	}
	if m.replaces.cancel != nil {
		m.replaces.cancel()
		m.replaces.cancel = nil
	}
	if m.workspaceEdits.cancel != nil {
		m.workspaceEdits.cancel()
		m.workspaceEdits.cancel = nil
	}
	if m.codeActionCommandStop != nil {
		m.codeActionCommandStop()
		m.codeActionCommandStop = nil
	}
	if m.externalReads.cancel != nil {
		m.externalReads.cancel()
		m.externalReads.cancel = nil
	}
	if m.codemapCancel != nil {
		m.codemapCancel()
		m.codemapCancel = nil
	}
	if m.healthDashboardCancel != nil {
		m.healthDashboardCancel()
		m.healthDashboardCancel = nil
	}
	// Stop search commands before stopping their shared indexers. Otherwise a
	// semantic-search waiter that is scheduled late can become a new setup
	// leader after CancelIndexing has already taken its snapshot.
	m.searchM.Cancel()
	// A semantic index build deliberately outlives the search that started it,
	// so nothing else cancels it. Without this it would keep running as an
	// orphaned process after Teak exits.
	search.CancelIndexing()
	codemap.CancelIndexing()

	// Shutdown all protocol children outside the Bubble Tea Update path, but
	// never wait without a global bound. The managers kill and reap stubborn
	// children before this returns whenever the deadline permits.
	if m.coordinator != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if !m.coordinator.ShutdownAndWait(ctx) {
			log.Warn("protocol shutdown deadline reached; forced teardown may still be unwinding")
		}
		cancel()
	}

	// Reap the index build after the protocol shutdown above, so both teardowns
	// overlap rather than running their timeouts back to back.
	indexCtx, cancelIndexWait := context.WithTimeout(context.Background(), 2*time.Second)
	if !search.WaitForIndexingShutdown(indexCtx) {
		log.Warn("semantic index build did not stop within the shutdown deadline")
	}
	if !codemap.WaitForIndexingShutdown(indexCtx) {
		log.Warn("codemap index build did not stop within the shutdown deadline")
	}
	cancelIndexWait()
	if m.logFile != nil {
		_ = m.logFile.Close()
	}
	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.agentWriteRoot != nil {
		_ = m.agentWriteRoot.Close()
		m.agentWriteRoot = nil
	}
	if m.pluginMgr != nil {
		m.pluginMgr.Shutdown()
	}
}

// sessionSnapshot captures the editor state before a background session save.
// It only reads model state, so the returned value is safe for a tea.Cmd.
func (m Model) sessionSnapshot() (session.State, bool) {
	if !m.appCfg.Session.Enabled {
		return session.State{}, false
	}
	state := session.State{
		Version:   1,
		RootDir:   m.rootDir,
		ActiveTab: -1,
	}
	for i, ed := range m.editors {
		fp := ed.Buffer.FilePath
		if fp == "" {
			continue // skip untitled/diff tabs
		}
		if _, isDiff := m.diffViews[i]; isDiff {
			continue
		}
		if i == m.activeTab {
			state.ActiveTab = len(state.Tabs)
		}
		state.Tabs = append(state.Tabs, session.TabState{
			FilePath:    fp,
			CursorLine:  ed.Buffer.Cursor.Line,
			CursorCol:   ed.Buffer.Cursor.Col,
			ScrollY:     ed.Viewport.ScrollY,
			WrapScrollY: ed.Viewport.WrapScrollY,
			Pinned:      i < len(m.tabBar.Tabs) && !m.tabBar.Tabs[i].Preview,
		})
	}
	known := make(map[string]struct{}, len(state.Tabs))
	for _, tab := range state.Tabs {
		known[filepath.Clean(tab.FilePath)] = struct{}{}
	}
	for _, tab := range m.sessionUnrestoredTabs {
		if tab.FilePath == "" {
			continue
		}
		clean := filepath.Clean(tab.FilePath)
		if _, exists := known[clean]; exists {
			continue
		}
		known[clean] = struct{}{}
		state.Tabs = append(state.Tabs, tab)
	}
	return state, true
}

type sessionAutoSaveMsg struct{}

type sessionSaveResultMsg struct {
	generation uint64
	err        error
}

type sessionSaveState struct {
	inFlight       bool
	generation     uint64
	queued         *session.State
	queuedRecovery []recoveryPrep
}

// recoveryPrep is a rope snapshot of a buffer that would be lost in a crash.
// Ropes are immutable pointers, so gathering them inside Update is cheap; the
// bytes are only materialized by the save command.
type recoveryPrep struct {
	FilePath string
	Untitled bool
	CRLF     bool
	Rope     *text.Rope
}

// recoveryPreps gathers the dirty and untitled buffers that should survive a
// crash. Returns nil when recovery is suppressed (the user explicitly quit
// without saving), so the final write clears stale records instead of
// resurrecting discarded work.
func (m Model) recoveryPreps() []recoveryPrep {
	if m.recoverySuppressed {
		return nil
	}
	var preps []recoveryPrep
	total := 0
	for _, ed := range m.editors {
		if len(preps) >= session.MaxRecoveryRecords {
			break
		}
		buf := ed.Buffer
		if buf == nil || buf.Rope() == nil {
			continue
		}
		size := buf.Rope().Len()
		if size == 0 || size > session.MaxRecoveryRecordBytes || size > session.MaxRecoveryContentBytes-total {
			continue
		}
		if buf.FilePath == "" {
			preps = append(preps, recoveryPrep{Untitled: true, CRLF: buf.LineEnding() == text.CRLF, Rope: buf.Rope()})
			total += size
			continue
		}
		if buf.Dirty() {
			preps = append(preps, recoveryPrep{FilePath: buf.FilePath, CRLF: buf.LineEnding() == text.CRLF, Rope: buf.Rope()})
			total += size
		}
	}
	return preps
}

// writeRecoveryRecords materializes recovery preps and persists them. It runs
// off the UI goroutine as part of the session save command.
func writeRecoveryRecords(rootDir string, preps []recoveryPrep) error {
	if rootDir == "" {
		return nil
	}
	records := make([]session.RecoveryRecord, 0, len(preps))
	now := time.Now()
	total := 0
	for _, prep := range preps {
		if len(records) >= session.MaxRecoveryRecords {
			break
		}
		if prep.Rope == nil {
			continue
		}
		size := prep.Rope.Len()
		if size == 0 || size > session.MaxRecoveryRecordBytes || size > session.MaxRecoveryContentBytes-total {
			continue
		}
		records = append(records, session.RecoveryRecord{
			FilePath: prep.FilePath,
			Untitled: prep.Untitled,
			CRLF:     prep.CRLF,
			Modified: now,
			Content:  prep.Rope.Bytes(),
		})
		total += size
	}
	return session.SaveRecovery(rootDir, records)
}

// startSessionSave starts exactly one background write. Callers must either
// have no write in flight or deliberately replace the queued latest snapshot.
func (m Model) startSessionSave(state session.State, recovery []recoveryPrep) (Model, tea.Cmd) {
	m.sessionSaves.inFlight = true
	m.sessionSaves.generation++
	generation := m.sessionSaves.generation
	saver := m.sessionSaver
	if saver == nil {
		saver = session.Save
	}
	rootDir := m.rootDir
	return m, func() tea.Msg {
		err := saver(state)
		if recErr := writeRecoveryRecords(rootDir, recovery); recErr != nil && err == nil {
			err = recErr
		}
		return sessionSaveResultMsg{generation: generation, err: err}
	}
}

func (m Model) requestSessionSave() (Model, tea.Cmd) {
	state, ok := m.sessionSnapshot()
	if !ok {
		return m, nil
	}
	recovery := m.recoveryPreps()
	if m.sessionSaves.inFlight {
		m.sessionSaves.queued = &state
		m.sessionSaves.queuedRecovery = recovery
		return m, nil
	}
	return m.startSessionSave(state, recovery)
}

func (m Model) nextSessionAutoSaveTick() tea.Cmd {
	if !m.appCfg.Session.Enabled || m.appCfg.Session.AutoSaveInterval <= 0 || m.shutdownStarted {
		return nil
	}
	interval := time.Duration(m.appCfg.Session.AutoSaveInterval) * time.Second
	return tea.Tick(interval, func(time.Time) tea.Msg { return sessionAutoSaveMsg{} })
}

func (m Model) handleSessionSaveResult(msg sessionSaveResultMsg) (Model, tea.Cmd) {
	if !m.sessionSaves.inFlight || msg.generation != m.sessionSaves.generation {
		return m, nil
	}
	m.sessionSaves.inFlight = false
	if msg.err != nil {
		log.Warn("session save failed", "err", msg.err)
	}
	if m.sessionSaves.queued != nil {
		state := *m.sessionSaves.queued
		recovery := m.sessionSaves.queuedRecovery
		m.sessionSaves.queued = nil
		m.sessionSaves.queuedRecovery = nil
		return m.startSessionSave(state, recovery)
	}
	if m.shutdownStarted {
		return m, m.shutdownCmd()
	}
	return m, m.nextSessionAutoSaveTick()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.modelState == nil {
		return nil
	}
	var cmds []tea.Cmd

	// Load initial file content asynchronously
	for i, ed := range m.editors {
		if ed.Buffer.FilePath != "" {
			// Init has a value receiver, so mutations to pendingFileLoads would
			// not survive into Bubble Tea's stored model. The stable editor ID is
			// sufficient here; all subsequent user-triggered loads have a tracked
			// cancellable request identity.
			cmds = append(cmds, loadFileCmd(context.Background(), ed.Buffer.FilePath, ed.ID(), 0, i > 0))
		}
	}
	if m.sessionRestoreEligible && m.sessionRestoreCtx != nil {
		cmds = append(cmds, sessionRestoreCmd(m.sessionRestoreCtx, m.sessionRestoreGeneration, m.rootDir))
	} else if m.appCfg.Session.Enabled {
		// Session restore is not running (no saved session, or superseded
		// before dispatch), but crash recovery still applies on its own.
		cmds = append(cmds, recoveryLoadCmd(m.rootDir))
	}

	// Start listening for LSP messages
	cmds = append(cmds, m.listenLSP())

	// Initial git panel refresh
	cmds = append(cmds, git.DetectRepositoryCmd(m.rootDir))
	if m.rootDir != "" {
		rootDir, theme, generation := m.rootDir, m.theme, m.treeRefreshGeneration
		cmds = append(cmds, func() tea.Msg {
			tree := filetree.New(rootDir, theme)
			return treeLoadedMsg{Tree: tree, Generation: generation}
		})
	}

	// Start file watcher listener
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}

	// Start welcome animation if active
	if m.welcome != nil && m.welcome.Active {
		cmds = append(cmds, m.welcome.Init())
	}

	// Start periodic session auto-save
	if m.appCfg.Session.Enabled && m.appCfg.Session.AutoSaveInterval > 0 {
		interval := time.Duration(m.appCfg.Session.AutoSaveInterval) * time.Second
		cmds = append(cmds, tea.Tick(interval, func(t time.Time) tea.Msg {
			return sessionAutoSaveMsg{}
		}))
	}

	// Start DAP event listener
	cmds = append(cmds, m.listenDAP())

	// Start ACP agent listener
	if m.acpMgr != nil {
		cmds = append(cmds, m.listenACP(), m.startAgent())
	}

	// Plugin initialization executes user-provided Lua and is intentionally
	// deferred until after the model can render its first frame.
	cmds = append(cmds, pluginLoadCmd(plugin.DefaultDir(), m.pluginLoadGeneration))

	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m = m.ensureState()
	if _, ok := msg.(shutdownCompleteMsg); ok {
		m.quitApproved = true
		return m, tea.Quit
	}
	if m.shutdownStarted {
		if result, ok := msg.(sessionSaveResultMsg); ok {
			return m.handleSessionSaveResult(result)
		}
		// Once cleanup begins, no further UI mutation is allowed. In particular,
		// an external interrupt must not bypass the process-reaping command.
		return m, nil
	}

	m.agentPanel.PruneCancelledWrites()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case editor.WelcomeTickMsg:
		return m.handleWelcomeTick(msg)

	case pluginEventMsg:
		return m.handlePluginEvent(msg)

	case pluginDispatchResultMsg:
		return m.handlePluginDispatchResult(msg)

	case pluginKeyDispatchResultMsg:
		return m.handlePluginKeyDispatchResult(msg)

	case pluginUIConfirmResultMsg:
		return m.handlePluginUIConfirmResult(msg)

	case pluginUIInputResultMsg:
		return m.handlePluginUIInputResult(msg)

	case pluginUISelectResultMsg:
		return m.handlePluginUISelectResult(msg)

	case healthDashboardResultMsg:
		return m.handleHealthDashboardResult(msg)

	case healthDashboardCloseMsg:
		return m.handleHealthDashboardClose()

	case overlay.FloatCloseMsg:
		m.handlePluginFloatClosed(msg)
		return m, nil

	case treeLoadedMsg:
		return m.handleTreeLoaded(msg)

	case treeRefreshDebounceMsg:
		return m.handleTreeRefreshDebounce(msg)

	case treeRefreshResultMsg:
		return m.handleTreeRefreshResult(msg)

	case sessionRestoreResultMsg:
		cmd := m.applySessionRestore(msg)
		return m, cmd

	case recoveryLoadedMsg:
		return m.handleRecoveryLoaded(msg)
	case recoveryComparisonsMsg:
		return m.handleRecoveryComparisons(msg)

	case pluginLoadResultMsg:
		return m.handlePluginLoadResult(msg)

	case tea.KeyPressMsg:
		// Unhandled key presses fall through to routeFocusedInput below, so the
		// mutated model has to be carried out of the switch rather than returned.
		model, cmd, handled := m.handleKeyPress(msg)
		m = model
		if handled {
			return model, cmd
		}

	case tea.MouseClickMsg:
		m.cancelSessionRestore()
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case settingsSaveResultMsg:
		return m.handleSettingsSaveResult(msg)

	case settingsDiscardMsg:
		return m.handleSettingsDiscard(msg)

	case settingsKeepEditingMsg:
		return m.handleSettingsKeepEditing(msg)

	case filetree.DirExpandedMsg:
		return m.handleDirExpanded(msg)

	case filetree.FilterReadyMsg:
		return m.handleTreeFilterReady(msg)

	case filetree.OpenFileMsg:
		return m.openFile(msg.Path)

	case filetree.PinFileMsg:
		return m.openFilePinned(msg.Path)

	case search.OpenResultMsg:
		return m.handleSearchOpenResult(msg)

	case search.CloseSearchMsg:
		return m.handleSearchClose(msg)

	case search.ReplaceOneMsg:
		return m, m.startSearchReplace(msg.Query, msg.Replacement, false, search.SearchOpts{Regex: msg.Regex, CaseSensitive: msg.CaseSensitive})

	case search.ReplaceAllMsg:
		return m, m.startSearchReplace(msg.Query, msg.Replacement, true, search.SearchOpts{Regex: msg.Regex, CaseSensitive: msg.CaseSensitive})

	case replacePreparedMsg:
		return m.handleReplacePrepared(msg)

	case search.SearchIndexingMsg:
		return m.handleSearchOverlayMsg(msg)

	case search.DebounceTickMsg:
		return m.handleSearchOverlayMsg(msg)

	case search.SearchResultsMsg:
		return m.handleSearchOverlayMsg(msg)

	case sessionAutoSaveMsg:
		return m.requestSessionSave()

	case sessionSaveResultMsg:
		return m.handleSessionSaveResult(msg)

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)

	case SwitchTabMsg:
		return m.handleSwitchTab(msg)

	case CloseTabMsg:
		return m.handleCloseTab(msg)

	case ForceCloseTabMsg:
		return m.closeTab(msg.Index)

	case SaveAndCloseTabMsg:
		return m.handleSaveAndCloseTab(msg)

	case editor.RetokenizeMsg:
		return m.handleEditorAsyncMsg(msg.EditorID, msg)

	case editor.FindDebounceMsg:
		// The find widget scans off the UI goroutine; both its debounce tick and
		// its result are addressed to a specific editor, which validates the
		// generation before applying anything.
		return m.handleEditorAsyncMsg(msg.EditorID, msg)

	case editor.FindResultsMsg:
		return m.handleEditorAsyncMsg(msg.EditorID, msg)

	case editor.TokenizeCompleteMsg:
		return m.handleEditorAsyncMsg(msg.EditorID, msg)

	case editor.ClipboardPasteResultMsg:
		return m.handleClipboardPasteResult(msg)

	case editor.PastePreparedMsg:
		return m.handlePastePrepared(msg)

	case editor.ClipboardCopyPreparedMsg:
		return m.handleClipboardCopyPrepared(msg)

	case editor.ClipboardCopyResultMsg:
		return m.handleClipboardCopyResult(msg)

	case editor.RequestCompletionCmd:
		return m.requestCompletion()

	case editor.OccurrenceSearchLimitMsg:
		m.status = fmt.Sprintf("Select occurrences is limited to files up to %d MiB", msg.MaxBytes>>20)
		return m, nil

	case editor.ClipboardOperationLimitMsg:
		m.status = fmt.Sprintf("%s is limited to %d MiB", msg.Operation, msg.MaxBytes>>20)
		return m, nil

	case editor.MultilineEditLimitMsg:
		m.status = fmt.Sprintf("%s is limited to %d lines", msg.Operation, msg.MaxLines)
		return m, nil

	case FileSavedMsg:
		return m.handleFileSaved(msg)

	case SaveAllAndQuitMsg:
		return m.handleSaveAllAndQuit(msg)

	case QuitWithoutSavingMsg:
		// The user explicitly discarded these buffers; the final session write
		// must clear their recovery records instead of resurrecting them on the
		// next launch.
		m.recoverySuppressed = true
		return m.finalizeQuit()

	case requestQuitMsg:
		return m.requestQuit()

	case overlay.PickerSelectMsg:
		return m.handlePickerSelect(msg)

	case overlay.PickerCloseMsg:
		return m.handlePickerClose(msg)

	case overlay.PickerFilterReadyMsg:
		return m.handlePickerFilterReady(msg)

	case overlay.PickerItemsReadyMsg:
		return m.handlePickerItemsReady(msg)

	case FileListMsg:
		return m.handleFileList(msg)

	case git.RepositoryDetectedMsg:
		return m.handleGitRepositoryDetected(msg)

	case commandPaletteMsg:
		return m.handleCommandPaletteAction(msg.inner)

	case git.RefreshMsg:
		return m.handleGitRefresh(msg)

	case git.PreparedRefreshMsg:
		return m.handlePreparedGitRefresh(msg)

	case git.OpenDiffMsg:
		return m.openDiff(msg.Path, msg.Status)

	case git.CommitResultMsg:
		return m.handleGitCommitResult(msg)

	case git.PushResultMsg:
		return m.handleGitPushResult(msg)

	case git.PullResultMsg:
		return m.handleGitPullResult(msg)

	case git.OpenBranchPickerMsg:
		return m.handleGitOpenBranchPicker(msg)

	case git.BranchListMsg:
		return m.handleGitBranchList(msg)

	case git.SwitchBranchMsg:
		m.showBranchPicker = false
		return m, git.SwitchBranchCmd(m.gitPanel.RootDir(), msg.Branch)

	case git.SwitchBranchResultMsg:
		return m.handleGitSwitchBranchResult(msg)

	case git.CloseBranchPickerMsg:
		m.showBranchPicker = false
		return m, nil

	case DiffLoadedMsg:
		return m.handleDiffLoaded(msg)

	case FileErrorMsg:
		return m.handleFileError(msg)

	case savePreconditionFailedMsg:
		return m.handleSavePreconditionFailed(msg)

	case FileLoadedMsg:
		return m.handleFileLoaded(msg)

	case FileLoadErrorMsg:
		return m.handleFileLoadError(msg)

	case FileChangedMsg:
		return m.handleExternalFileChange(msg)

	case externalFileReadPreparedMsg:
		return m.handleExternalFileReadPrepared(msg)

	case saveAsDestinationResolutionMsg:
		return m.handleSaveAsDestinationResolution(msg)

	case externalConflictResolutionMsg:
		return m.handleExternalConflictResolution(msg)

	case externalConflictReloadPreparedMsg:
		return m.handleExternalConflictReloadPrepared(msg)

	case externalFoldRegionsPreparedMsg:
		return m.handleExternalFoldRegionsPrepared(msg)

	case TreeChangedMsg:
		return m.handleTreeChange(msg)

	case watcherLimitMsg:
		return m.handleWatcherLimit()

	case treeCopyPathResultMsg:
		return m.handleTreeCopyPathResult(msg)

	case treeActionResultMsg:
		return m.handleTreeActionResult(msg)

	case gitRefreshDebounceMsg:
		return m.handleGitRefreshDebounce(msg)

	case lsp.DiagnosticsMsg:
		return m.handleDiagnostics(msg)

	case lsp.CompletionResultMsg:
		return m.handleCompletionResult(msg)

	case lsp.HoverResultMsg:
		return m.handleHoverResult(msg)

	case lsp.SignatureHelpResultMsg:
		return m.handleSignatureHelpResult(msg)

	case lsp.FormatResultMsg:
		return m.handleFormatResult(msg)

	case lsp.CodeActionResultMsg:
		return m.handleCodeActionResult(msg)

	case lsp.ApplyEditRequestMsg:
		// The protocol reader only transports this request. Filesystem access
		// and rope preparation happen in a command; Update later performs the
		// version-checked pointer swap serially with every other buffer change.
		return m.startWorkspaceEditAsync(msg.Edit, workspaceEditContinuation{Context: msg.Context, Claim: msg.Claim, Respond: msg.Respond})

	case workspaceEditPreparedMsg:
		return m.handleWorkspaceEditPrepared(msg)

	case workspaceEditCommitResultMsg:
		return m.handleWorkspaceEditCommitResult(msg)

	case lspCodeActionCommandResultMsg:
		return m.handleCodeActionCommandResult(msg)

	case lsp.DocumentSymbolResultMsg:
		return m.handleDocumentSymbolResult(msg)

	case lsp.DefinitionResultMsg:
		return m.handleDefinitionResult(msg)

	case lsp.ReferencesResultMsg:
		return m.handleReferencesResult(msg)

	case lsp.RenameResultMsg:
		return m.handleRenameResult(msg)

	case editor.ContextMenuActionMsg:
		return m.handleContextMenuAction(msg.Action)

	case editor.BreakpointClickMsg:
		return m.handleBreakpointClick(msg)

	case LspReadyMsg:
		return m.handleLspReady(msg)

	case lsp.FoldingRangeResultMsg:
		return m.handleFoldingRangeResult(msg)

	case lsp.LspErrorMsg:
		m.status = fmt.Sprintf("LSP error [%s]: %s (code %d)", msg.Method, msg.Message, msg.Code)
		return m, nil

	case lsp.LspShowMessageMsg:
		return m.handleLspShowMessage(msg)

	case lsp.LspProgressMsg:
		// Progress reporting - can be extended to show in UI
		// For now, just log it
		return m, nil

	case lsp.ServerExitedMsg:
		return m.handleServerExited(msg)

	case lspMsg:
		return m.routeLSPMsg(msg)

	case acpMsg:
		return m.routeACPMsg(msg)

	case dapMsg:
		return m.routeDAPMsg(msg)

	case debugStateMsg:
		return m.handleDebugState(msg)

	case debugStartResultMsg:
		return m.handleDebugStartResult(msg)

	case debugStopResultMsg:
		return m.handleDebugStopResult(msg)

	case debugActionResultMsg:
		return m.handleDebugActionResult(msg)

	case debugger.JumpToFrameMsg:
		return m.handleJumpToFrame(msg)

	case stashSavedMsg:
		m.status = fmt.Sprintf("Stashed → %s", msg.result.ID)
		return m, nil

	case stashErrMsg:
		m.status = fmt.Sprintf("Stash failed: %v", msg.err)
		return m, nil

	case codemapCallersMsg:
		return m.startCodemapQuery("callers")
	case codemapCalleesMsg:
		return m.startCodemapQuery("callees")
	case codemapImpactMsg:
		return m.startCodemapQuery("impact")

	case bobPlanMsg:
		return m, m.runBobPlan()
	case bobCheckMsg:
		return m, m.runBobCheck()

	case bobResultMsg:
		m.handleBobResult(msg)
		return m, nil

	case codemapResultMsg:
		return m.handleCodemapResult(msg)

	case acp.AgentModelChangedMsg:
		m.agentPanel, _ = m.agentPanel.Update(msg)
		return m, nil

	case acp.AgentModeChangedMsg:
		return m.handleAgentPanelModeChanged(msg)

	case agent.CancelRequestedMsg:
		return m.handleAgentCancelRequested(msg)

	case agent.WriteDecisionMsg:
		return m.handleAgentWriteDecision(msg)

	case agentWriteResultMsg:
		return m.handleAgentWriteResult(msg)

	case toggleAgentMsg:
		cmd := m.toggleAgentPanel()
		return m, cmd

	case focusAgentMsg:
		return m.handleFocusAgent(msg)

	case agentCancelMsg:
		return m.handleAgentCancel(msg)
	}

	if model, cmd, handled := m.routeFocusedInput(msg); handled {
		return model, cmd
	}
	return m, nil
}

// --- Update message handlers -------------------------------------------------
//
// One method per message type routed by Update. Each mirrors exactly what its
// former inline case arm did; Update itself is pure routing. Model wraps a
// *modelState, so a value receiver here mutates the same shared state the
// inline bodies did.

// --- layout, welcome screen and plugins ---

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	if m.healthDashboard != nil {
		m.healthDashboard.SetSize(msg.Width, msg.Height)
	}
	m.relayout()
	return m, nil
}

func (m Model) handleWelcomeTick(msg editor.WelcomeTickMsg) (tea.Model, tea.Cmd) {
	if m.welcome != nil && m.welcome.Active {
		var cmd tea.Cmd
		m.welcome, cmd = m.welcome.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handlePluginEvent(msg pluginEventMsg) (tea.Model, tea.Cmd) {
	if m.pluginMgr == nil || len(msg.Events) == 0 {
		return m, nil
	}
	events := make([]plugin.EventContext, 0, len(msg.Events))
	for _, event := range msg.Events {
		if ctx := m.enrichPluginEventContext(event); ctx.Event != "" {
			events = append(events, ctx)
		}
	}
	if len(events) == 0 {
		return m, nil
	}
	return m, pluginEventDispatchCmd(m.pluginMgr, newPluginAsyncRuntime(m), events)
}

func (m Model) handlePluginDispatchResult(msg pluginDispatchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = fmt.Sprintf("Plugin event error: %v", msg.Err)
	}
	if msg.Runtime == nil {
		return m, nil
	}
	cmd := msg.Runtime.apply(&m)
	return m, cmd
}

func (m Model) handlePluginKeyDispatchResult(msg pluginKeyDispatchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = fmt.Sprintf("Plugin key error: %v", msg.Err)
	}
	if msg.Runtime == nil {
		return m, nil
	}
	cmd := msg.Runtime.apply(&m)
	return m, cmd
}

func (m Model) handlePluginLoadResult(msg pluginLoadResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.pluginLoadGeneration {
		if msg.Manager != nil {
			msg.Manager.Shutdown()
		}
		return m, nil
	}
	m.pluginLoading = false
	if msg.Err != nil {
		m.status = fmt.Sprintf("Plugin load failed: %v", msg.Err)
		if msg.Manager != nil {
			msg.Manager.Shutdown()
		}
		return m, nil
	}
	m.pluginMgr = msg.Manager
	return m, pluginEventCmd(plugin.EventContext{Event: plugin.EventVimEnter})
}

// --- file tree refresh ---

func (m Model) handleTreeLoaded(msg treeLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.treeRefreshGeneration {
		return m, nil
	}
	showHidden := m.tree.ShowHidden()
	showGitIgnored := m.tree.ShowGitIgnored()
	filter := m.tree.Filter()
	filterActive := m.tree.FilterActive()
	m.tree = msg.Tree
	m.tree.SetShowHidden(showHidden)
	m.tree.SetShowGitIgnored(showGitIgnored)
	m.tree.SetFilter(filter)
	if filterActive {
		// SetFilter intentionally does not own input; restore the prompt state
		// separately when a full tree snapshot replaces the live model.
		m.tree.StartFilter()
	}
	m.tree.SetDiagnostics(m.treeDiagnostics)
	gitStatusMap := make(map[string]string)
	for _, entry := range m.gitPanel.Entries {
		if entry.IsUnstagedChange() {
			gitStatusMap[entry.Path] = entry.DisplayStatus(false)
		} else if entry.IsStagedChange() {
			gitStatusMap[entry.Path] = entry.DisplayStatus(true)
		}
	}
	m.tree.SetGitStatus(gitStatusMap)
	m.relayout()
	return m, nil
}

func (m Model) handleTreeRefreshDebounce(msg treeRefreshDebounceMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.treeRefreshGeneration {
		return m, nil
	}
	return m, m.startTreeRefresh(msg.Generation)
}

func (m Model) handleTreeRefreshResult(msg treeRefreshResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.treeRefreshGeneration {
		return m, nil
	}
	m.treeRefreshCancel = nil
	if msg.Err != nil {
		if !errors.Is(msg.Err, context.Canceled) {
			m.status = fmt.Sprintf("Could not refresh file tree: %v", msg.Err)
		}
		return m, nil
	}
	m.tree.ApplyRefresh(msg.Refresh)
	return m, nil
}

// --- keyboard ---

// handleKeyPress runs the two key-routing stages. The bool reports whether the
// key was consumed; when it is false the caller must keep the returned model so
// the fallback input routing sees the same mutations the stages performed.
func (m Model) handleKeyPress(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	// Any explicit keyboard interaction belongs to the user, not a delayed
	// startup snapshot. Do not let the latter replace a tab or buffer later.
	m.cancelSessionRestore()
	model, cmd, handled := m.handleKeyPressPrecedence(msg)
	m = model
	if handled {
		return model, cmd, true
	}
	globalModel, cmd, handled := m.handleGlobalKey(msg)
	m = globalModel.(Model)
	if handled {
		return m, cmd, true
	}
	return m, nil, false
}

// --- settings ---

func (m Model) handleSettingsSaveResult(msg settingsSaveResultMsg) (tea.Model, tea.Cmd) {
	m.settingsSaving = false
	if msg.Err != nil {
		m.settingsM.SetStatus(fmt.Sprintf("Could not save settings: %v", msg.Err))
		m.status = "Settings were not saved"
		return m, nil
	}
	m.applySavedSettings(msg.Config)
	status := "Settings saved and applied"
	if msg.Config.UI.Theme != m.activeThemeName {
		status = "Settings saved; restart Teak to apply the new theme"
	}
	if msg.Outcome == config.SavedWithBackup {
		// The annotated file could not be patched in place; the rewrite kept a
		// copy so the user's comments are not gone, just moved.
		status += " (original config backed up to config.toml.bak)"
	}
	m.settingsM.MarkSaved(msg.Config, status)
	m.status = status
	return m, nil
}

func (m Model) handleSettingsDiscard(_ settingsDiscardMsg) (tea.Model, tea.Cmd) {
	m.unsavedConfirm = nil
	m.discardSettingsChanges()
	return m, nil
}

func (m Model) handleSettingsKeepEditing(_ settingsKeepEditingMsg) (tea.Model, tea.Cmd) {
	m.unsavedConfirm = nil
	m.settingsM.SetStatus("Changes kept — press Ctrl+S to save")
	return m, nil
}

func (m Model) handleDirExpanded(msg filetree.DirExpandedMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.tree, cmd = m.tree.Update(msg)
	return m, cmd
}

func (m Model) handleTreeFilterReady(msg filetree.FilterReadyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.tree, cmd = m.tree.Update(msg)
	return m, cmd
}

// --- search overlay ---

func (m Model) handleSearchOpenResult(msg search.OpenResultMsg) (tea.Model, tea.Cmd) {
	m.showSearch = false
	validated, err := search.ValidateResults(m.rootDir, []search.Result{{
		FilePath: msg.FilePath,
		Line:     msg.Line,
		Col:      msg.Col,
	}})
	if err != nil {
		m.status = fmt.Sprintf("Cannot open search result: %v", err)
		return m, nil
	}
	filePath := filepath.Join(m.rootDir, filepath.FromSlash(validated[0].FilePath))
	pos := text.Position{Line: msg.Line, Col: msg.Col}
	m.setPendingCursor(filePath, pos)
	return m.openFilePinned(filePath)
}

func (m Model) handleSearchClose(_ search.CloseSearchMsg) (tea.Model, tea.Cmd) {
	// Save results for F3/Shift+F3 navigation
	if results := m.searchM.Results(); len(results) > 0 {
		m.lastSearchResults = results
		m.lastSearchIndex = 0
	}
	m.showSearch = false
	return m, nil
}

// handleSearchOverlayMsg forwards an async search message to the search model,
// but only while the overlay is open: results that land after it closed must
// not revive it.
func (m Model) handleSearchOverlayMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.showSearch {
		var cmd tea.Cmd
		m.searchM, cmd = m.searchM.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
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
}

// --- tabs ---

func (m Model) handleSwitchTab(msg SwitchTabMsg) (tea.Model, tea.Cmd) {
	if msg.Index >= 0 && msg.Index < len(m.editors) && msg.Index != m.activeTab {
		oldPath := m.editors[m.activeTab].Buffer.FilePath
		newPath := m.editors[msg.Index].Buffer.FilePath
		m.activateTab(msg.Index)
		return m, tea.Batch(
			m.triggerPluginEvents(
				m.pluginEvent(plugin.EventBufLeave, oldPath),
				m.pluginEvent(plugin.EventBufEnter, newPath),
			),
		)
	}
	if msg.Index >= 0 && msg.Index < len(m.editors) {
		m.activateTab(msg.Index)
	}
	return m, nil
}

func (m Model) handleCloseTab(msg CloseTabMsg) (tea.Model, tea.Cmd) {
	idx := msg.Index
	if idx == -1 {
		idx = m.activeTab
	}
	return m.closeTabSafe(idx)
}

func (m Model) handleSaveAndCloseTab(msg SaveAndCloseTabMsg) (tea.Model, tea.Cmd) {
	if msg.Index >= 0 && msg.Index < len(m.editors) {
		buf := m.editors[msg.Index].Buffer
		if buf.FilePath == "" {
			m.status = "Save & Close requires a file path"
			return m, nil
		}
		return m, m.beginSaveForTab(msg.Index, true, false)
	}
	return m, nil
}

// --- editor async results ---

// handleEditorAsyncMsg forwards a message addressed to one editor to that
// editor, dropping it when the tab is gone. The recipient validates its own
// generation before applying anything.
func (m Model) handleEditorAsyncMsg(editorID uint64, msg tea.Msg) (tea.Model, tea.Cmd) {
	idx := m.editorIndexForAsyncMessage(editorID)
	if idx < 0 {
		return m, nil
	}
	updated, cmd := m.editors[idx].Update(msg)
	m.setEditor(idx, updated)
	return m, cmd
}

func (m Model) handleClipboardPasteResult(msg editor.ClipboardPasteResultMsg) (tea.Model, tea.Cmd) {
	// Clipboard reads run outside Update and may finish after focus changes
	// or after the target tab is gone. The editor ID and generation are
	// validated by the recipient before any text is inserted.
	idx := m.editorIndexForAsyncMessage(msg.EditorID)
	if idx < 0 {
		return m, nil
	}
	previousVersion := m.editors[idx].Buffer.Version()
	previousCursor := m.editors[idx].Buffer.Cursor
	updated, cmd := m.editors[idx].Update(msg)
	m.setEditor(idx, updated)
	if updated.Buffer.Version() == previousVersion {
		// Large clipboard payloads only queue background preparation here;
		// retain that command even though no edit has committed yet.
		return m, cmd
	}
	if msg.Err != nil {
		m.status = "Clipboard integration unavailable; pasted local copy"
	}
	return m, tea.Batch(cmd, m.syncEditorStateAfterUpdate(idx, previousVersion, previousCursor))
}

func (m Model) handlePastePrepared(msg editor.PastePreparedMsg) (tea.Model, tea.Cmd) {
	idx := m.editorIndexForAsyncMessage(msg.EditorID)
	if idx < 0 {
		return m, nil
	}
	previousVersion := m.editors[idx].Buffer.Version()
	previousCursor := m.editors[idx].Buffer.Cursor
	updated, cmd := m.editors[idx].Update(msg)
	m.setEditor(idx, updated)
	if updated.Buffer.Version() == previousVersion {
		return m, cmd
	}
	if msg.SourceErr != nil {
		m.status = "Clipboard integration unavailable; pasted local copy"
	}
	return m, tea.Batch(cmd, m.syncEditorStateAfterUpdate(idx, previousVersion, previousCursor))
}

func (m Model) handleClipboardCopyPrepared(msg editor.ClipboardCopyPreparedMsg) (tea.Model, tea.Cmd) {
	idx := m.editorIndexForAsyncMessage(msg.EditorID)
	if idx < 0 {
		return m, nil
	}
	previousVersion := m.editors[idx].Buffer.Version()
	previousCursor := m.editors[idx].Buffer.Cursor
	updated, cmd := m.editors[idx].Update(msg)
	m.setEditor(idx, updated)
	if updated.Buffer.Version() == previousVersion {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.syncEditorStateAfterUpdate(idx, previousVersion, previousCursor))
}

func (m Model) handleClipboardCopyResult(msg editor.ClipboardCopyResultMsg) (tea.Model, tea.Cmd) {
	if m.editorIndexForAsyncMessage(msg.EditorID) < 0 {
		return m, nil
	}
	if msg.OSC52Sequence != "" {
		// The OS tools are missing (typically over SSH), but the terminal on
		// the other end owns the user's clipboard: hand it the payload.
		m.status = "Copied to terminal clipboard (OSC 52)"
		return m, tea.Printf("%s", msg.OSC52Sequence)
	}
	if msg.Err == nil {
		return m, nil
	}
	if !msg.FallbackStored {
		m.status = fmt.Sprintf("Clipboard copy skipped: %v", msg.Err)
		return m, nil
	}
	m.status = fmt.Sprintf("Copied locally; OS clipboard unavailable: %v", msg.Err)
	return m, nil
}

// --- saving and quitting ---

func (m Model) handleFileSaved(msg FileSavedMsg) (tea.Model, tea.Cmd) {
	if msg.WatcherWatermark != 0 {
		path := cleanExternalConflictPath(msg.Path)
		m.lastSaveWatcherWatermarks[path] = max(
			m.lastSaveWatcherWatermarks[path],
			msg.WatcherWatermark,
		)
	}
	req, hadPendingSave := m.completeSaveRequest(msg.RequestID)
	if hadPendingSave && req.AuthorizedConflictGeneration != 0 {
		path := cleanExternalConflictPath(req.Path)
		if conflict, ok := m.externalConflicts[path]; ok &&
			conflict.Generation == req.AuthorizedConflictGeneration {
			m.clearExternalConflict(path)
		}
	}
	currentSnapshot := false
	savedEditorIdx := -1
	var saveAsCmd tea.Cmd
	var queuedSaveCmd tea.Cmd
	if hadPendingSave {
		savedEditorIdx = m.saveRequestEditorIndex(req)
		if savedEditorIdx >= 0 && req.Snapshot != nil {
			buf := m.editors[savedEditorIdx].Buffer
			currentSnapshot = buf.Version() == req.SnapshotVersion && buf.Rope() == req.Snapshot
			buf.MarkSavedSnapshot(req.Path, req.Snapshot)
			m.tabBar.Tabs[savedEditorIdx].Dirty = buf.Dirty()
			if req.SaveAs {
				saveAsCmd = m.reconcileSaveAs(savedEditorIdx, req.PreviousPath, req.Path)
			}
		}
	}
	if hadPendingSave && !currentSnapshot {
		if req.QueuedSnapshot != nil {
			// A newer snapshot targeting the same successful Save As path is
			// now an ordinary save: the editor identity has already moved.
			if req.SaveAs && req.QueuedPath == req.Path {
				req.QueuedSaveAs = false
				req.QueuedPreviousPath = ""
			}
			m.status = fmt.Sprintf("Saved %s snapshot; saving newer edits", msg.Path)
			queuedSaveCmd = m.startQueuedSave(msg.RequestID, req)
		} else {
			m.status = fmt.Sprintf("Saved %s; newer edits remain unsaved", msg.Path)
		}
		if req.QuitAfter && queuedSaveCmd == nil {
			m.cancelQuitAfterSaves()
		}
	} else if hadPendingSave && req.SaveAs {
		m.status = fmt.Sprintf("Saved as %s", msg.Path)
		if req.StatusNote != "" {
			m.status = fmt.Sprintf("%s (%s)", m.status, req.StatusNote)
		}
	} else {
		m.status = saveSuccessStatus(msg.Path, req.StatusNote)
	}
	search.InvalidateSemanticIndex(m.rootDir)
	if !hadPendingSave {
		// Compatibility path for callers that still use SaveFileCmd directly.
		for i := range m.tabBar.Tabs {
			if m.tabBar.Tabs[i].FilePath == msg.Path {
				m.tabBar.Tabs[i].Dirty = m.editors[i].Buffer.Dirty()
			}
		}
	}
	var cmds []tea.Cmd
	if saveAsCmd != nil {
		cmds = append(cmds, saveAsCmd)
	}
	if (!hadPendingSave || currentSnapshot) && m.lspMgr != nil {
		if client := m.lspMgr.ClientForFile(msg.Path); client != nil {
			// A new Save As URI may still need its didOpen command to run.
			// Sending didSave first creates an invalid LSP notification order;
			// an already-open formatting document is the sole safe exception.
			if !hadPendingSave || !req.SaveAs {
				cmds = append(cmds, lspDidSaveCmd(m.lspMgr, msg.Path))
			} else if _, isOpen := client.DocumentVersion(lsp.FileURI(msg.Path)); isOpen {
				cmds = append(cmds, lspDidSaveCmd(m.lspMgr, msg.Path))
			}
		}
	}
	if hadPendingSave && currentSnapshot && req.CloseAfter {
		if savedEditorIdx >= 0 {
			model, closeCmd := m.closeTab(savedEditorIdx)
			m = model.(Model)
			if closeCmd != nil {
				cmds = append(cmds, closeCmd)
			}
		}
	}
	// Refresh git panel after save
	if refreshCmd := m.gitPanel.Refresh(); refreshCmd != nil {
		cmds = append(cmds, refreshCmd)
	}
	if !hadPendingSave || currentSnapshot {
		cmds = append(cmds, m.triggerPluginEvents(m.pluginEvent(plugin.EventBufWrite, msg.Path)))
	}
	if queuedSaveCmd != nil {
		cmds = append(cmds, queuedSaveCmd)
	}
	if hadPendingSave && currentSnapshot && req.QuitAfter && !m.hasPendingQuitAfterSaves() {
		cmds = append(cmds, func() tea.Msg { return QuitWithoutSavingMsg{} })
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleSaveAllAndQuit(msg SaveAllAndQuitMsg) (tea.Model, tea.Cmd) {
	var saveCmds []tea.Cmd
	unsaveable := 0
	for i := range m.editors {
		if !m.editors[i].Buffer.Dirty() {
			continue
		}
		if m.editors[i].Buffer.FilePath == "" {
			unsaveable++
			continue
		}
		if cmd := m.beginSaveForTab(i, false, true); cmd != nil {
			saveCmds = append(saveCmds, cmd)
		}
	}
	if unsaveable > 0 {
		m.cancelQuitAfterSaves()
		if len(saveCmds) > 0 {
			m.status = "Saved file-backed tabs; use Save As for untitled tabs before quitting"
			return m, tea.Batch(saveCmds...)
		}
		m.status = "Use Save As for untitled tabs before quitting"
		return m, nil
	}
	if len(saveCmds) == 0 {
		return m.finalizeQuit()
	}
	return m, tea.Batch(saveCmds...)
}

func (m Model) handleFileError(msg FileErrorMsg) (tea.Model, tea.Cmd) {
	req, hadPendingSave := m.completeSaveRequest(msg.RequestID)
	if hadPendingSave && m.watcher != nil {
		m.watcher.cancelOwnWrite(req.Path, req.Snapshot)
	}
	if hadPendingSave && req.QuitAfter {
		m.cancelQuitAfterSaves()
	}
	switch {
	case errors.Is(msg.Err, text.ErrDestinationReadOnly):
		// A bare "permission denied" would be puzzling here, because the save
		// would have succeeded through the atomic rename. Say what is actually
		// wrong and what to do about it.
		m.status = fmt.Sprintf("%s is read-only; change its permissions to save (chmod +w)",
			filepath.Base(msg.Path))
	case msg.Path != "":
		m.status = fmt.Sprintf("Error saving %s: %v", msg.Path, msg.Err)
	default:
		m.status = fmt.Sprintf("Error: %v", msg.Err)
	}
	return m, nil
}

func (m Model) handleSavePreconditionFailed(msg savePreconditionFailedMsg) (tea.Model, tea.Cmd) {
	req, hadPendingSave := m.pendingSaves[msg.RequestID]
	if !hadPendingSave {
		return m, nil
	}
	if req.SaveAs {
		switch {
		case msg.Missing && req.DiskExpectation == saveDiskExact:
			// The user already authorized replacing the inspected version,
			// which disappeared before the exact comparison. Requiring the
			// destination to remain missing still prevents a concurrent
			// recreation from being overwritten.
			req.DiskExpectation = saveDiskMissing
			req.ExpectedDiskSnapshot = nil
			m.pendingSaves[msg.RequestID] = req
			return m, m.startSaveRequest(msg.RequestID)
		case msg.Snapshot != nil:
			req.DiskExpectation = saveDiskExact
			req.ExpectedDiskSnapshot = msg.Snapshot
			m.pendingSaves[msg.RequestID] = req
			m = m.showSaveAsDestinationConfirmation(msg.RequestID, req.Path)
			m.status = fmt.Sprintf(
				"Save As destination %s already exists; confirm before replacing it",
				filepath.Base(req.Path),
			)
			return m, nil
		default:
			_, _ = m.completeSaveRequest(msg.RequestID)
			if req.QuitAfter {
				m.cancelQuitAfterSaves()
			}
			m.status = fmt.Sprintf(
				"Save As blocked because %s could not be verified safely: %v",
				filepath.Base(req.Path),
				msg.Err,
			)
			return m, nil
		}
	}
	req, hadPendingSave = m.completeSaveRequest(msg.RequestID)
	if !hadPendingSave {
		return m, nil
	}
	if req.QuitAfter {
		m.cancelQuitAfterSaves()
	}
	path := cleanExternalConflictPath(msg.Path)
	m.recordExternalConflict(path, msg.Snapshot, 0, msg.Missing, false)
	m = m.showExternalConflictConfirmation(path)
	m.status = fmt.Sprintf(
		"Save blocked because %s changed after it was loaded: %v",
		filepath.Base(path),
		msg.Err,
	)
	return m, nil
}

func (m Model) handleFileLoadError(msg FileLoadErrorMsg) (tea.Model, tea.Cmd) {
	if msg.RequestID != 0 {
		request, ok := m.pendingFileLoads[msg.RequestID]
		if !ok || request.EditorID != msg.EditorID || request.Path != msg.Path {
			return m, nil
		}
		delete(m.pendingFileLoads, msg.RequestID)
		request.Cancel()
	}
	if !errors.Is(msg.Err, context.Canceled) {
		m.status = fmt.Sprintf("Error loading %s: %v", filepath.Base(msg.Path), msg.Err)
	}
	return m, nil
}

// --- overlays and pickers ---

func (m Model) handlePickerSelect(msg overlay.PickerSelectMsg) (tea.Model, tea.Cmd) {
	item := msg.Item
	if sel, ok := item.Value.(pluginUISelectItem); ok {
		m.clearOverlayStack()
		return m, func() tea.Msg {
			return pluginUISelectResultMsg{
				CallbackID: sel.CallbackID,
				Option:     sel.Option,
				Index:      sel.Index,
				Accepted:   true,
			}
		}
	}
	m.clearOverlayStack()
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
	// LSP code action picker. Revalidate the originating document here,
	// not only when the server response first arrived: the user may edit or
	// switch files while the picker is open.
	if sel, ok := item.Value.(lspCodeActionPickerMsg); ok {
		if !m.acceptsDocumentResult(documentRequestCodeAction, sel.Metadata, true) {
			m.status = "Code action expired; request it again"
			return m, nil
		}

		if sel.Action.Edit != nil {
			action := sel.Action
			return m.startWorkspaceEditAsync(*action.Edit, workspaceEditContinuation{
				CodeAction:    &action,
				CodeActionURI: sel.Metadata.FilePath,
			})
		}

		if sel.Action.Command == nil {
			if sel.Action.Edit == nil {
				m.status = fmt.Sprintf("Code action %q has no edit or command", sel.Action.Title)
			}
			return m, nil
		}

		return m.executeSelectedCodeAction(sel.Action, sel.Metadata)
	}
	// LSP location picker (go-to-definition / references)
	if sel, ok := item.Value.(lspLocationPickerMsg); ok {
		loc := sel.Location
		path := lsp.URIToPath(loc.URI)
		pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
		m.setPendingLSPCursor(path, pos, loc.ProtocolEncoding)
		return m.openFilePinned(path)
	}
	// LSP symbol picker
	if sel, ok := item.Value.(lspSymbolPickerMsg); ok {
		ed := m.activeEditor()
		if ed != nil {
			ed.Buffer.SetCursor(text.Position{
				Line: sel.Symbol.SelectionRange.Start.Line,
				Col:  sel.Symbol.SelectionRange.Start.Character,
			})
			ed.EnsureCursorVisible()
			m.setEditor(m.activeTab, *ed)
		}
		return m, nil
	}
	// Code map callers/callees/impact picker.
	if sel, ok := item.Value.(codemapSymbolPickerMsg); ok {
		path := sel.Symbol.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.rootDir, path)
		}
		m.setPendingCursor(path, text.Position{Line: sel.Symbol.Line0(), Col: 0})
		return m.openFilePinned(path)
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
}

func (m Model) handlePickerClose(_ overlay.PickerCloseMsg) (tea.Model, tea.Cmd) {
	m.clearOverlayStack()
	m.cancelFileListScan()
	return m, nil
}

func (m Model) handlePickerFilterReady(msg overlay.PickerFilterReadyMsg) (tea.Model, tea.Cmd) {
	if m.overlayStack.IsEmpty() {
		return m, nil
	}
	return m, m.overlayStack.Update(msg)
}

func (m Model) handlePickerItemsReady(msg overlay.PickerItemsReadyMsg) (tea.Model, tea.Cmd) {
	if m.overlayStack.IsEmpty() {
		return m, nil
	}
	return m, m.overlayStack.Update(msg)
}

func (m Model) handleFileList(msg FileListMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.fileListGeneration {
		return m, nil
	}
	m.fileListCancel = nil
	if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
		if errors.Is(msg.Err, errQuickOpenLimit) {
			m.status = fmt.Sprintf("Quick Open limited to %d files", maxQuickOpenFiles)
		} else {
			m.status = fmt.Sprintf("Quick Open scan: %v", msg.Err)
		}
	}
	m.cachedFiles = msg.Files
	m.cachedFilesReady = true
	var filterCmd tea.Cmd
	// If quick open picker is showing, update its items
	if !m.overlayStack.IsEmpty() {
		if picker, ok := m.overlayStack.Top().(*overlay.Picker); ok {
			switch picker.ZoneID() {
			case "quickopen":
				filterCmd = preparePickerItemsCmd(picker.InstanceID(), "quickopen", m.cachedFiles, false)
			case "agent-file-picker":
				filterCmd = preparePickerItemsCmd(picker.InstanceID(), "agent-file-picker", m.cachedFiles, true)
			}
		}
	}
	return m, filterCmd
}

// --- git ---

func (m Model) handleGitRepositoryDetected(msg git.RepositoryDetectedMsg) (tea.Model, tea.Cmd) {
	m.gitPanel.SetIsGitRepo(msg.IsRepository)
	if !msg.IsRepository {
		m.gitBranch = ""
		return m, nil
	}
	return m, m.gitPanel.Refresh()
}

func (m Model) handleGitRefresh(msg git.RefreshMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != 0 && msg.Generation != m.gitRefreshGeneration {
		return m, nil
	}
	var cmd tea.Cmd
	m.gitPanel, cmd = m.gitPanel.Update(msg)
	if msg.Err != nil {
		m.status = fmt.Sprintf("Git error: %v", msg.Err)
		return m, cmd
	}
	return m, cmd
}

func (m Model) handlePreparedGitRefresh(msg git.PreparedRefreshMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != 0 && msg.Generation != m.gitRefreshGeneration {
		return m, nil
	}
	applied, cmd := m.gitPanel.ApplyPreparedRefresh(msg)
	if !applied {
		return m, cmd
	}
	if msg.Branch != "" {
		m.gitBranch = msg.Branch
	}
	if !m.gitPanel.IsGitRepo() {
		m.gitPanel.SetIsGitRepo(true)
	}
	m.tree.SetGitStatus(msg.GitStatus)
	return m, cmd
}

func (m Model) handleGitCommitResult(msg git.CommitResultMsg) (tea.Model, tea.Cmd) {
	m.gitPanel.StopSpinner()
	if msg.Err != nil {
		m.status = fmt.Sprintf("Commit failed: %v", msg.Err)
	} else {
		m.status = "Committed successfully"
	}
	return m, m.gitPanel.Refresh()
}

func (m Model) handleGitPushResult(msg git.PushResultMsg) (tea.Model, tea.Cmd) {
	m.gitPanel.StopSpinner()
	if msg.Err != nil {
		m.status = fmt.Sprintf("Push failed: %v", msg.Err)
	} else {
		m.status = "Pushed successfully"
	}
	return m, m.gitPanel.Refresh()
}

func (m Model) handleGitPullResult(msg git.PullResultMsg) (tea.Model, tea.Cmd) {
	m.gitPanel.StopSpinner()
	if msg.Err != nil {
		m.status = fmt.Sprintf("Pull failed: %v", msg.Err)
	} else {
		m.status = "Pulled successfully"
	}
	return m, m.gitPanel.Refresh()
}

func (m Model) handleGitOpenBranchPicker(_ git.OpenBranchPickerMsg) (tea.Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
	m.showBranchPicker = true
	m.branchPickerM.SetSize(m.width, m.height)
	return m, tea.Batch(
		git.ListBranchesCmd(m.gitPanel.RootDir()),
		m.branchPickerM.Focus(),
	)
}

func (m Model) handleGitBranchList(msg git.BranchListMsg) (tea.Model, tea.Cmd) {
	if msg.Err == nil {
		m.branchPickerM.SetBranches(msg.Branches, msg.Current)
	}
	return m, nil
}

func (m Model) handleGitSwitchBranchResult(msg git.SwitchBranchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = fmt.Sprintf("Switch failed: %v", msg.Err)
	} else {
		m.gitBranch = msg.Branch
		m.status = fmt.Sprintf("Switched to %s", msg.Branch)
	}
	return m, m.gitPanel.Refresh()
}

func (m Model) handleGitRefreshDebounce(msg gitRefreshDebounceMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.gitRefreshGeneration {
		return m, nil
	}
	if refreshCmd := m.gitPanel.Refresh(); refreshCmd != nil {
		generation := msg.generation
		return m, func() tea.Msg {
			result := refreshCmd()
			if refresh, ok := result.(git.RefreshMsg); ok {
				refresh.Generation = generation
				return refresh
			}
			return result
		}
	}
	return m, nil
}

// --- LSP results ---

func (m Model) handleCompletionResult(msg lsp.CompletionResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsOverlayResult(overlayRequestCompletion, msg.OverlayRequestMetadata) {
		return m, nil
	}
	items := make([]overlays.AutocompleteItem, len(msg.Items))
	for i, item := range msg.Items {
		items[i] = overlays.AutocompleteItem{
			Label:      item.Label,
			Detail:     item.Detail,
			InsertText: item.InsertText,
			HasEdit:    item.HasEdit,
			Edit: overlays.AutocompleteEdit{
				StartLine: item.Edit.StartLine,
				StartCol:  item.Edit.StartCol,
				EndLine:   item.Edit.EndLine,
				EndCol:    item.Edit.EndCol,
			},
		}
	}
	if m.activeEditor() != nil {
		m.activeEditor().ShowAutocomplete(items)
		m.setEditor(m.activeTab, *m.activeEditor())
	}
	return m, nil
}

func (m Model) handleHoverResult(msg lsp.HoverResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsOverlayResult(overlayRequestHover, msg.OverlayRequestMetadata) {
		return m, nil
	}
	if msg.Content != "" && m.activeEditor() != nil {
		m.activeEditor().ShowHover(msg.Content)
		m.setEditor(m.activeTab, *m.activeEditor())
	}
	return m, nil
}

func (m Model) handleSignatureHelpResult(msg lsp.SignatureHelpResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsOverlayResult(overlayRequestSignature, msg.OverlayRequestMetadata) {
		return m, nil
	}
	if msg.Help != nil && m.activeEditor() != nil {
		// Convert to overlays.SignatureData
		sigData := &overlays.SignatureData{
			ActiveSignature: msg.Help.ActiveSignature,
			ActiveParameter: msg.Help.ActiveParameter,
		}
		for _, sig := range msg.Help.Signatures {
			var params []overlays.ParameterInfo
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
				params = append(params, overlays.ParameterInfo{
					Label:         label,
					Documentation: p.Documentation,
				})
			}
			sigData.Signatures = append(sigData.Signatures, overlays.SignatureInfo{
				Label:         sig.Label,
				Documentation: sig.Documentation,
				Parameters:    params,
			})
		}
		m.activeEditor().ShowSignatureHelp(sigData)
		m.setEditor(m.activeTab, *m.activeEditor())
	}
	return m, nil
}

func (m Model) handleFormatResult(msg lsp.FormatResultMsg) (tea.Model, tea.Cmd) {
	idx := m.findEditorByPath(msg.FilePath)
	if msg.RequestID != 0 {
		req, ok := m.pendingSaves[msg.RequestID]
		if ok {
			idx = m.saveRequestEditorIndex(req)
		}
		if !ok || idx < 0 || (msg.HasBaseVersion && (req.SnapshotVersion != msg.BaseVersion || m.editors[idx].Buffer.Version() != msg.BaseVersion || m.editors[idx].Buffer.Rope() != req.Snapshot)) {
			delete(m.pendingSaves, msg.RequestID)
			if ok && req.QueuedSnapshot != nil {
				m.status = "Formatting result discarded; formatting newer edits"
				return m, m.startQueuedSave(msg.RequestID, req)
			}
			m.status = "Formatting result discarded; newer edits remain unsaved"
			if ok && req.QuitAfter {
				m.cancelQuitAfterSaves()
			}
			return m, nil
		}
	} else if msg.HasBaseVersion && (idx < 0 || m.editors[idx].Buffer.Version() != msg.BaseVersion) {
		m.status = "Formatting result discarded; newer edits remain unsaved"
		return m, nil
	}

	formatStatus := msg.Status
	formatErr := msg.Err
	var postMutationCmd tea.Cmd
	if formatStatus == lsp.FormatApplied && idx >= 0 {
		if err := validateFormattingTextEdits(m.editors[idx].Buffer, msg.Edits); err != nil {
			// Format responses are untrusted server input. Do not let the
			// buffer's interactive position clamping turn an invalid response
			// into a partial or Unicode-corrupting edit.
			formatStatus = lsp.FormatError
			formatErr = fmt.Errorf("invalid formatting edits: %w", err)
			m.status = fmt.Sprintf("Formatting result rejected: %v", err)
		} else {
			prevVersion := m.editors[idx].Buffer.Version()
			prevCursor := m.editors[idx].Buffer.Cursor
			applied := applyTextEditsToBuffer(m.editors[idx].Buffer, msg.Edits)
			if applied > 0 {
				if m.editors[idx].Highlighter != nil {
					m.editors[idx].Highlighter.Invalidate()
				}
				editorID := m.editors[idx].ID()
				version := m.editors[idx].Buffer.Version()
				retokenizeCmd := func() tea.Msg {
					return editor.RetokenizeMsg{EditorID: editorID, Version: version}
				}
				postMutationCmd = tea.Batch(
					retokenizeCmd,
					m.syncEditorStateAfterUpdate(idx, prevVersion, prevCursor),
				)
				m.status = "Document formatted"
			}
		}
	}
	if msg.RequestID == 0 {
		switch formatStatus {
		case lsp.FormatApplied:
			if idx >= 0 && idx == m.activeTab {
				m.setEditor(m.activeTab, m.editors[idx])
			}
		case lsp.FormatNoOp:
			m.status = "No formatting changes"
		case lsp.FormatUnsupported:
			m.status = "Formatting not supported"
		case lsp.FormatError:
			if formatErr != nil {
				m.status = fmt.Sprintf("Formatting failed: %v", formatErr)
			} else {
				m.status = "Formatting failed"
			}
		}
		return m, postMutationCmd
	}

	if formatStatus == lsp.FormatApplied && idx >= 0 {
		req := m.pendingSaves[msg.RequestID]
		req.Snapshot = m.editors[idx].Buffer.Rope()
		req.SnapshotVersion = m.editors[idx].Buffer.Version()
		req.LineEnding = m.editors[idx].Buffer.LineEnding()
		m.pendingSaves[msg.RequestID] = req
	}
	m.setPendingSaveNote(msg.RequestID, formatResultNote(formatStatus, formatErr))
	return m, tea.Batch(postMutationCmd, m.startSaveRequest(msg.RequestID))
}

func (m Model) handleCodeActionResult(msg lsp.CodeActionResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestCodeAction, msg.DocumentRequestMetadata, true) {
		return m, nil
	}
	if len(msg.Actions) == 0 {
		m.status = "No code actions available"
		return m, nil
	}
	items := lspCodeActionsToPickerItems(msg.Actions, msg.DocumentRequestMetadata)
	picker := overlay.NewPicker(fmt.Sprintf("Code Actions (%d)", len(items)), items, m.theme, "lsp-code-actions")
	m.overlayStack.Push(picker)
	return m, picker.Focus()
}

func (m Model) handleCodeActionCommandResult(msg lspCodeActionCommandResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.codeActionCommandGen ||
		!m.acceptsDocumentResult(documentRequestCodeAction, msg.Metadata, true) {
		return m, nil
	}
	m.codeActionCommandStop = nil
	if msg.Err != nil {
		m.status = fmt.Sprintf("Code action %q failed: %v", msg.Title, msg.Err)
		return m, nil
	}
	m.status = fmt.Sprintf("Code action completed: %s", msg.Title)
	return m, nil
}

func (m Model) handleDocumentSymbolResult(msg lsp.DocumentSymbolResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestSymbols, msg.DocumentRequestMetadata, true) {
		return m, nil
	}
	if len(msg.Symbols) > 0 {
		items := lspSymbolsToPickerItems(msg.Symbols)
		picker := overlay.NewPicker(fmt.Sprintf("Document Symbols (%d)", len(msg.Symbols)), items, m.theme, "lsp-sym")
		m.overlayStack.Push(picker)
		return m, picker.Focus()
	}
	m.status = "No symbols found"
	return m, nil
}

func (m Model) handleDefinitionResult(msg lsp.DefinitionResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestDefinition, msg.DocumentRequestMetadata, true) {
		return m, nil
	}
	if len(msg.Locations) == 1 {
		loc := msg.Locations[0]
		path := lsp.URIToPath(loc.URI)
		pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
		m.setPendingLSPCursor(path, pos, loc.ProtocolEncoding)
		return m.openFilePinned(path)
	} else if len(msg.Locations) > 1 {
		items := lspLocationsToPickerItems(msg.Locations, m.rootDir)
		picker := overlay.NewPicker("Go to Definition", items, m.theme, "lsp-def")
		m.overlayStack.Push(picker)
		return m, picker.Focus()
	}
	m.status = "No definition found"
	return m, nil
}

func (m Model) handleReferencesResult(msg lsp.ReferencesResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestReferences, msg.DocumentRequestMetadata, true) {
		return m, nil
	}
	if len(msg.Locations) == 1 {
		loc := msg.Locations[0]
		path := lsp.URIToPath(loc.URI)
		pos := text.Position{Line: loc.StartLine, Col: loc.StartCol}
		m.setPendingLSPCursor(path, pos, loc.ProtocolEncoding)
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
}

func (m Model) handleRenameResult(msg lsp.RenameResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestRename, msg.DocumentRequestMetadata, true) {
		return m, nil
	}
	return m.startWorkspaceEditAsync(msg.Edit, workspaceEditContinuation{})
}

func (m Model) handleLspReady(msg LspReadyMsg) (tea.Model, tea.Cmd) {
	// LSP finished initializing — set trigger characters on matching editors
	if client := m.lspMgr.ClientForFile(msg.FilePath); client != nil {
		if chars := client.GetCompletionTriggerCharacters(); len(chars) > 0 {
			for i := range m.editors {
				if m.editors[i].Buffer.FilePath == msg.FilePath {
					m.editors[i].TriggerCharacters = chars
				}
			}
		}
		// If the document changed while the server was still starting, send
		// one full-sync update to reconcile stale didOpen content.
		for i := range m.editors {
			if m.editors[i].Buffer.FilePath == msg.FilePath && m.editors[i].Buffer.Version() > msg.OpenVersion {
				version, snapshot := m.editors[i].Buffer.Version(), m.editors[i].Buffer.Rope()
				_, foldingCmd := m.requestFoldingRanges(msg.FilePath)
				return m, tea.Sequence(lspDidChangeCmd(m.lspMgr, msg.FilePath, version, snapshot, m.lspMaterializationBudget()), foldingCmd)
			}
		}
	}
	// Request folding ranges from LSP
	return m.requestFoldingRanges(msg.FilePath)
}

func (m Model) handleFoldingRangeResult(msg lsp.FoldingRangeResultMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsDocumentResult(documentRequestFolding, msg.DocumentRequestMetadata, false) {
		return m, nil
	}
	for i := range m.editors {
		if m.editors[i].Buffer.FilePath == msg.FilePath {
			regions := make([]editor.FoldRegion, len(msg.Ranges))
			for j, r := range msg.Ranges {
				regions[j] = editor.FoldRegion{
					StartLine: r.StartLine,
					EndLine:   r.EndLine,
				}
			}
			m.editors[i].Folds.SetRegions(regions)
		}
	}
	return m, nil
}

func (m Model) handleLspShowMessage(msg lsp.LspShowMessageMsg) (tea.Model, tea.Cmd) {
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
}

func (m Model) handleServerExited(msg lsp.ServerExitedMsg) (tea.Model, tea.Cmd) {
	m.status = fmt.Sprintf("Language server %q exited unexpectedly", msg.Command)
	log.Error("lsp server exited", "command", msg.Command, "err", msg.Err)
	if m.lspMgr == nil {
		return m, nil
	}
	if m.lspRestarts == nil {
		m.lspRestarts = make(map[string]int)
	}
	m.lspRestarts[msg.Command]++
	if m.lspRestarts[msg.Command] > maxLSPRestarts {
		// A server that keeps crashing must not be relaunched forever.
		m.status = fmt.Sprintf("Language server %q keeps crashing; not restarting it again", msg.Command)
		return m, nil
	}
	// Re-open every affected document: EnsureClient replaces the dead client
	// and didOpen restores the server's view of buffers that were already
	// open before the crash.
	m.status = fmt.Sprintf("Language server %q exited unexpectedly; restarting", msg.Command)
	var cmds []tea.Cmd
	for i := range m.editors {
		buf := m.editors[i].Buffer
		if buf == nil || buf.FilePath == "" {
			continue
		}
		cfg := m.lspMgr.ConfigForFile(buf.FilePath)
		if cfg == nil || cfg.Command != msg.Command {
			continue
		}
		cmds = append(cmds, m.lspDidOpen(buf))
	}
	return m, tea.Batch(cmds...)
}

// --- protocol message routing ---

func (m Model) routeLSPMsg(msg lspMsg) (tea.Model, tea.Cmd) {
	// Route through LSP coordinator
	if m.coordinator != nil {
		if cmds := m.coordinator.HandleMessage(msg); len(cmds) > 0 {
			return m, tea.Batch(append(cmds, m.listenLSP())...)
		}
	}
	if msg.msg == nil {
		return m, m.listenLSP()
	}
	result, cmd := m.Update(msg.msg)
	m = result.(Model)
	return m, tea.Batch(cmd, m.listenLSP())
}

func (m Model) routeACPMsg(msg acpMsg) (tea.Model, tea.Cmd) {
	// Route through ACP coordinator
	if m.coordinator != nil {
		if cmds := m.coordinator.HandleMessage(msg); len(cmds) > 0 {
			return m, tea.Batch(append(cmds, m.listenACP())...)
		}
	}
	return m.handleACPMsg(msg)
}

func (m Model) routeDAPMsg(msg dapMsg) (tea.Model, tea.Cmd) {
	// Route through DAP coordinator
	if m.coordinator != nil {
		if cmds := m.coordinator.HandleMessage(msg); len(cmds) > 0 {
			return m, tea.Batch(append(cmds, m.listenDAP())...)
		}
	}
	return m.handleDAPMsg(msg)
}

// --- debugger ---

func (m Model) handleBreakpointClick(msg editor.BreakpointClickMsg) (tea.Model, tea.Cmd) {
	if ed := m.activeEditor(); ed != nil && ed.Buffer.FilePath != "" {
		cmd := m.toggleBreakpoint(ed.Buffer.FilePath, msg.Line)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleDebugState(msg debugStateMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != 0 && msg.Generation != m.debugLifecycle.generation {
		return m, nil
	}
	m.debuggerPanel.SetStackFrames(msg.Frames)
	m.debuggerPanel.SetVariables(msg.Variables)
	if len(msg.Frames) > 0 {
		frame := msg.Frames[0]
		if frame.Source.Path != "" {
			m.setExecutionLocation(frame.Source.Path, frame.Line-1) // DAP is 1-based, we use 0-based
		}
	}
	return m, nil
}

func (m Model) handleJumpToFrame(msg debugger.JumpToFrameMsg) (tea.Model, tea.Cmd) {
	// Open the file and jump to the line
	if msg.FilePath != "" {
		m.setPendingCursor(msg.FilePath, text.Position{Line: msg.Line, Col: 0})
		return m.openFilePinned(msg.FilePath)
	}
	return m, nil
}

// --- tooling integrations ---

func (m Model) handleCodemapResult(msg codemapResultMsg) (tea.Model, tea.Cmd) {
	if msg.generation != 0 && msg.generation != m.codemapGeneration {
		return m, nil
	}
	if m.codemapCancel != nil {
		m.codemapCancel()
		m.codemapCancel = nil
	}
	if msg.err != nil {
		m.status = fmt.Sprintf("Code map: %v", msg.err)
		return m, nil
	}
	if len(msg.symbols) == 0 {
		m.status = fmt.Sprintf("Code map: no %s found", msg.kind)
		return m, nil
	}
	items := codemapSymbolsToPickerItems(msg.symbols, m.rootDir)
	picker := overlay.NewPicker(fmt.Sprintf("Code Map %s (%d)", msg.kind, len(msg.symbols)), items, m.theme, "codemap-"+msg.kind)
	m.overlayStack.Push(picker)
	return m, picker.Focus()
}

// --- agent panel ---

func (m Model) handleAgentPanelModeChanged(msg acp.AgentModeChangedMsg) (tea.Model, tea.Cmd) {
	m.agentPanel, _ = m.agentPanel.Update(msg)
	m.agentPanel.AddSystemMessage("Mode changed to " + string(msg.ModeId))
	return m, nil
}

func (m Model) handleAgentCancelRequested(_ agent.CancelRequestedMsg) (tea.Model, tea.Cmd) {
	if m.acpMgr != nil {
		m.acpMgr.Cancel()
		m.agentPanel.AddSystemMessage("Cancelled.")
	}
	return m, nil
}

func (m Model) handleFocusAgent(_ focusAgentMsg) (tea.Model, tea.Cmd) {
	if m.showAgent {
		if m.focus == FocusAgent {
			m.setFocus(FocusEditor)
			m.agentPanel.Blur()
		} else {
			m.setFocus(FocusAgent)
			return m, m.agentPanel.Focus()
		}
	}
	return m, nil
}

func (m Model) handleAgentCancel(_ agentCancelMsg) (tea.Model, tea.Cmd) {
	if m.acpMgr != nil {
		m.acpMgr.Cancel()
	}
	return m, nil
}

// --- end Update message handlers ---------------------------------------------

// lspChangeQueue returns the independent per-document snapshot preparer. It
// is deliberately not captured through Model by tea.Cmd closures: a command
// may run after a tab has moved, changed, or closed.
func (m *Model) lspChangeQueue() *lspChangePreparer {
	if m == nil || m.modelState == nil {
		return nil
	}
	if m.lspChanges == nil {
		m.lspChanges = newLSPChangePreparer()
	}
	return m.lspChanges
}

// lspMaterializationBudget returns the process-wide transport budget owned by
// the change preparer. Commands capture this coordinator, never the live
// Model, before they flatten an immutable rope.
func (m Model) lspMaterializationBudget() *lspMaterializationBudget {
	if m.modelState != nil && m.lspChanges != nil {
		return lspMaterializationsFor(m.lspChanges.budget)
	}
	return sharedLSPMaterializations
}

// cancelLSPChangePreparation drops a pending full-text conversion when the
// document lifecycle is retired. Manager.CloseDocument remains the protocol
// authority; this is the memory/CPU half of the same cancellation.
func (m *Model) cancelLSPChangePreparation(path string) {
	if queue := m.lspChangeQueue(); queue != nil {
		queue.cancel(path)
	}
}

// notifyLSPChange sends a didChange notification using incremental sync if
// the server supports it and the buffer has change info, otherwise full sync.
// It copies all protocol metadata and captures an immutable Rope before
// returning. In particular, the tea.Cmd below never closes over ed or m.
func (m *Model) notifyLSPChange(client *lsp.Client, ed *editor.Editor) tea.Cmd {
	if m == nil || m.modelState == nil || client == nil || ed == nil || ed.Buffer == nil {
		return nil
	}
	path := ed.Buffer.FilePath
	if path == "" {
		return nil
	}
	uri := lsp.FileURI(path)
	mgr := m.lspMgr
	generation := uint64(0)
	if mgr != nil {
		generation = mgr.DocumentGeneration(path)
	}
	version := ed.Buffer.Version()
	snapshot := ed.Buffer.Rope()
	queue := m.lspChangeQueue()
	if queue == nil || snapshot == nil {
		return nil
	}

	if _, open := client.DocumentVersion(uri); !open {
		// lspDidOpen may still be running in another tea.Cmd. LspReady sends a
		// full catch-up after the didOpen barrier has been queued. A document
		// that was explicitly retired for exceeding LSP transport capacity is
		// different: retry it as a fresh full didOpen, so reducing its size can
		// resume synchronization without reopening the editor tab.
		if !client.DocumentSyncSuppressed(uri) {
			return nil
		}
		languageID := ""
		if mgr != nil {
			if cfg := mgr.ConfigForFile(path); cfg != nil {
				languageID = cfg.LanguageID
			}
		}
		return queue.queue(lspChangePayload{
			path:     path,
			version:  version,
			snapshot: snapshot,
			dispatch: func(content string) tea.Msg {
				if mgr != nil {
					if err := mgr.OpenDocument(path, generation, languageID, version, content); err != nil {
						command := ""
						if cfg := mgr.ConfigForFile(path); cfg != nil {
							command = cfg.Command
						}
						return lsp.LspErrorMsg{
							Method:  "textDocument/didOpen",
							Message: lspStartupMessage(command, err),
						}
					}
					return nil
				}
				client.DidOpen(uri, languageID, version, content)
				return nil
			},
		})
	}

	if change := ed.Buffer.LastChange(); change != nil && client.GetSyncKind() == lsp.SyncIncremental {
		// Copy the last-change values before returning from Update. Buffer owns
		// and replaces this record on the next edit, so retaining the pointer
		// would make a delayed command describe the wrong range.
		startLine, startCol := change.StartLine, change.StartCol
		endLine, endCol, replacement := change.EndLine, change.EndCol, change.Text
		return queue.queue(lspChangePayload{
			path:     path,
			version:  version,
			snapshot: snapshot,
			dispatch: func(content string) tea.Msg {
				if mgr != nil {
					mgr.ChangeDocumentIncremental(path, generation, version, startLine, startCol, endLine, endCol, replacement, content)
					return nil
				}
				client.DidChangeIncrementalWithContent(uri, version, startLine, startCol, endLine, endCol, replacement, content)
				return nil
			},
		})
	}

	return queue.queue(lspChangePayload{
		path:     path,
		version:  version,
		snapshot: snapshot,
		dispatch: func(content string) tea.Msg {
			if mgr != nil {
				mgr.ChangeDocument(path, generation, version, content)
				return nil
			}
			client.DidChange(uri, version, content)
			return nil
		},
	})
}

func (m Model) isActiveDiffTab() bool {
	if m.activeTab < len(m.tabBar.Tabs) {
		return m.tabBar.Tabs[m.activeTab].Kind == editor.TabDiff
	}
	return false
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

// editorIndexForAsyncMessage resolves the stable owner of an editor command.
// ID zero is retained as an explicit compatibility path for legacy tests and
// callers; production scheduling always carries an editor ID.
func (m *Model) editorIndexForAsyncMessage(editorID uint64) int {
	if editorID == 0 {
		if len(m.editors) == 0 {
			return -1
		}
		if m.activeTab >= 0 && m.activeTab < len(m.editors) {
			return m.activeTab
		}
		return 0
	}
	for i := range m.editors {
		if m.editors[i].ID() == editorID {
			return i
		}
	}
	return -1
}

// setEditor updates the editor slice, the single source of truth for tab state.
func (m *Model) setEditor(idx int, ed editor.Editor) {
	if idx < 0 || idx >= len(m.editors) {
		return
	}
	m.editors[idx] = ed
	m.projectDebugGutterForEditor(idx)
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
	ed := m.activeEditor()
	if ed == nil {
		return m, nil
	}
	if ed.Buffer.FilePath != "" {
		ed.HasLSP = m.lspMgr.ClientForFile(ed.Buffer.FilePath) != nil
	}
	prevVersion := ed.Buffer.Version()
	prevCursor := ed.Buffer.Cursor
	var cmd tea.Cmd
	updated, cmd := ed.Update(msg)
	m.setEditor(m.activeTab, updated)
	if updated.IsContextMenuVisible() {
		m.clampActiveEditorContextMenu()
	}

	postUpdateCmd := m.syncEditorStateAfterUpdate(m.activeTab, prevVersion, prevCursor)
	var signatureHelpCmd tea.Cmd
	if updated.Buffer.Version() != prevVersion && signatureHelpTrigger(msg) {
		m, signatureHelpCmd = m.requestSignatureHelp()
	}
	return m, tea.Batch(
		cmd,
		signatureHelpCmd,
		postUpdateCmd,
	)
}

// handleGitPanelClick routes a click in the git panel area.
// adjustedY is relative to the panel (0-based), originalMsg has absolute coords for zone checks.
func (m Model) handleGitPanelClick(adjustedY int, originalMsg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Check zone-based buttons using original absolute-coordinate message
	if zone.Get("git-init-btn").InBounds(originalMsg) {
		// Initialize git repository
		cmd := git.InitCmd(m.rootDir)
		m.status = "Initializing Git repository..."
		return m, cmd
	}
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
		cmd := m.gitPanel.FocusBodyAt(adjustedY, mouse.X)
		return m, cmd
	}

	// Positional fallback for commit form clicks (zone may only track last-marked line)
	switch m.gitPanel.CommitFormHitTest(adjustedY) {
	case "title":
		mouse := originalMsg.Mouse()
		cmd := m.gitPanel.FocusTitleAt(mouse.X)
		return m, cmd
	case "body":
		mouse := originalMsg.Mouse()
		cmd := m.gitPanel.FocusBodyAt(adjustedY, mouse.X)
		return m, cmd
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
	// Default sidebar width; the user can override it from Settings or by
	// dragging the divider, which persists ui.tree_width.
	const defaultWidth = 25
	width := m.appCfg.UI.TreeWidth
	if width <= 0 {
		width = defaultWidth
	}
	// Reserve one column for the divider and one for the editor. Keeping the
	// sidebar within this budget is important while a terminal is resized: a
	// minimum sidebar width would otherwise make the complete TUI wrap.
	maxSidebarWidth := max(0, m.width-2)
	if maxSidebarWidth == 0 {
		return 0
	}

	// On small screens fall back to a proportional width unless the user has
	// configured one explicitly.
	if m.width < 80 && m.appCfg.UI.TreeWidth <= 0 {
		tw := m.width / 4
		if tw < 1 {
			tw = 1
		}
		if tw > defaultWidth {
			tw = defaultWidth
		}
		return min(tw, maxSidebarWidth)
	}

	return min(width, maxSidebarWidth)
}

// treeVisible is the effective sidebar state for the current terminal size.
// showTree remains the user's preference, so it comes back automatically once
// enough columns are available.
func (m Model) treeVisible() bool {
	return m.showTree && m.treeWidth() > 0
}

func (m *Model) relayout() {
	statusHeight := 2 // divider + status bar
	tabBarHeight := 1
	compact := m.height < compactTerminalHeight
	if compact {
		statusHeight = 1 // compact view has no divider row
	}

	// Agent panel width (0 if hidden)
	aw := m.agentPanelWidth()
	if compact {
		aw = 0
	}
	if m.width > 0 && m.height > 0 && m.showAgent && aw == 0 && m.focus == FocusAgent {
		m.setFocus(FocusEditor)
		m.agentPanel.Blur()
	}
	agentExtra := 0
	if aw > 0 {
		agentExtra = aw + 1 // +1 for border
	}

	m.tabBar.Width = m.width // will be constrained when tree is shown

	sidebarHeight := m.height - statusHeight

	if !compact && m.treeVisible() {
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
		m.problemsPanel.SetSize(tw, panelHeight)
		m.debuggerPanel.SetSize(tw, panelHeight)
		m.tabBar.Width = editorWidth
		m.sizeEditors(editorWidth, editorHeight)
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
		m.sizeEditors(editorWidth, editorHeight)
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

	if m.showHelp {
		m.helpM.SetSize(m.width, m.height-2)
	}
	if m.showSettings {
		m.setSettingsOverlaySize()
	}
	if m.unsavedConfirm != nil {
		m.unsavedConfirm.SetWidth(min(50, max(1, m.width)))
	}
}

func (m Model) openFile(path string) (tea.Model, tea.Cmd) {
	return m.openFileAs(path, true)
}

// openFilePinned opens a file and immediately pins it (not a preview).
func (m Model) openFilePinned(path string) (tea.Model, tea.Cmd) {
	return m.openFileAs(path, false)
}

func (m Model) openFileAs(path string, preview bool) (tea.Model, tea.Cmd) {
	m.cancelSessionRestore()
	normalizedPath, err := m.normalizeEditorFilePath(path)
	if err != nil {
		m.status = fmt.Sprintf("Cannot open file: %v", err)
		return m, nil
	}
	path = normalizedPath
	if len(m.sessionUnrestoredTabs) > 0 {
		remaining := m.sessionUnrestoredTabs[:0]
		for _, tab := range m.sessionUnrestoredTabs {
			if filepath.Clean(tab.FilePath) != filepath.Clean(path) {
				remaining = append(remaining, tab)
			}
		}
		m.sessionUnrestoredTabs = remaining
	}
	if m.saveAsDestinationReserved(path) {
		m.status = fmt.Sprintf(
			"Cannot open %s while Save As is using that destination",
			filepath.Base(path),
		)
		return m, nil
	}
	// Dismiss welcome screen if active
	if m.welcome != nil {
		m.welcome.Dismiss()
	}

	oldActivePath := ""
	oldActiveIdx := m.activeTab
	if active := m.activeEditor(); active != nil {
		oldActivePath = active.Buffer.FilePath
	}

	// Check if already open
	idx := m.tabBar.FindTab(path)
	if idx >= 0 {
		m.activateTab(idx)
		m.setFocus(FocusEditor)
		// Double-open pins the tab
		if !preview {
			m.tabBar.PinTab(idx)
		}
		// Apply a pending navigation. UTF-16/32 target positions are deferred
		// until the document bytes are available, rather than guessed by the
		// protocol reader.
		if navigation := m.takePendingNavigation(path); navigation != nil {
			ed := m.activeEditor()
			position, err := resolvePendingLSPPositionRope(ed.Buffer.Rope(), navigation.Position, navigation.ProtocolEncoding)
			if err != nil {
				m.status = fmt.Sprintf("LSP navigation position rejected: %v", err)
			} else {
				ed.Buffer.SetCursor(position)
				ed.EnsureCursorVisible()
				m.setEditor(m.activeTab, *ed)
			}
		}
		if idx == oldActiveIdx || oldActivePath == path {
			return m, nil
		}
		return m, tea.Batch(
			m.triggerPluginEvents(
				m.pluginEvent(plugin.EventBufLeave, oldActivePath),
				m.pluginEvent(plugin.EventBufEnter, path),
			),
		)
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
	replaceIdx := m.findReplaceableTab()
	if replaceIdx >= 0 {
		replacedPath := m.tabBar.Tabs[replaceIdx].FilePath
		m.removeLoadsForEditor(m.editors[replaceIdx].ID())
		if m.watcher != nil && replacedPath != "" && replacedPath != path {
			m.watcher.UnwatchFile(replacedPath)
		}
		m.setEditor(replaceIdx, ed)
		m.tabBar.Tabs[replaceIdx].Label = filepath.Base(path)
		m.tabBar.Tabs[replaceIdx].FilePath = path
		m.tabBar.Tabs[replaceIdx].Dirty = false
		m.tabBar.Tabs[replaceIdx].Preview = preview
		m.tabBar.Tabs[replaceIdx].DiagSeverity = 0
		m.activateTab(replaceIdx)
	} else {
		m.editors = append(m.editors, ed)
		tabIdx := m.tabBar.AddTab(filepath.Base(path), path)
		m.tabBar.Tabs[tabIdx].Preview = preview
		m.activateTab(tabIdx)
	}

	m.setFocus(FocusEditor)
	m.relayout()

	// Read file content asynchronously
	return m, tea.Batch(
		m.triggerPluginEvents(m.pluginEvent(plugin.EventBufLeave, oldActivePath)),
		m.startFileLoad(path, ed, false, nil),
	)
}

func (m Model) handleFileLoaded(msg FileLoadedMsg) (tea.Model, tea.Cmd) {
	var request pendingFileLoad
	if msg.RequestID != 0 {
		var ok bool
		request, ok = m.pendingFileLoads[msg.RequestID]
		if !ok || request.EditorID != msg.EditorID || request.Path != msg.Path {
			return m, nil
		}
	}
	tabIdx := m.editorIndexForLoad(msg.EditorID, msg.Path)
	if msg.RequestID == 0 && msg.EditorID == 0 { // retained only for older direct unit messages
		tabIdx = msg.TabIndex
	}
	if tabIdx < 0 || tabIdx >= len(m.editors) || tabIdx >= len(m.tabBar.Tabs) || m.tabBar.Tabs[tabIdx].FilePath != msg.Path {
		return m, nil
	}
	if msg.RequestID != 0 {
		buffer := m.editors[tabIdx].Buffer
		if buffer.Version() != request.BaseVersion || buffer.Rope() != request.BaseRope {
			delete(m.pendingFileLoads, msg.RequestID)
			request.Cancel()
			m.status = fmt.Sprintf("Kept edits made while %s was loading", filepath.Base(msg.Path))
			return m, nil
		}
	}

	if msg.Snapshot == nil {
		// Legacy/direct callers may still provide Data. Convert it in a command,
		// preserving the same request identity so Update never constructs a
		// document-sized rope.
		data := msg.Data
		msg.Data = nil
		return m, func() tea.Msg {
			normalized, ending := text.NormalizeLineEndings(data)
			msg.Snapshot = text.New(normalized)
			msg.LineEnding = ending
			return msg
		}
	}
	if msg.RequestID != 0 {
		delete(m.pendingFileLoads, msg.RequestID)
		request.Cancel()
	}

	// Load source bytes unchanged; tab expansion is handled only by the editor view.
	ed := &m.editors[tabIdx]
	snapshot := msg.Snapshot
	ed.Buffer.LoadRopeSnapshot(snapshot, msg.LineEnding)
	if msg.RecoveredDirty {
		// Crash-recovery content is newer than whatever is on disk; keep it
		// visibly unsaved so the user decides what happens to it.
		ed.Buffer.MarkDirty()
		if tabIdx < len(m.tabBar.Tabs) {
			m.tabBar.Tabs[tabIdx].Dirty = true
		}
	}

	// Set up syntax highlighting
	cfg := ed.Config
	cfg.CommentPrefix = editor.CommentPrefixForFile(msg.Path)
	ed.Config = cfg
	newEd := editor.New(ed.Buffer, m.theme, ed.Config)
	m.setEditor(tabIdx, newEd)
	m.relayout()
	if msg.RecoveredDirty {
		m.status = "Recovered unsaved buffer from the previous session"
	} else {
		m.status = ""
	}

	// CLI/startup line:col is stashed when Init issues requestID-0 loads (value
	// receiver cannot register pendingFileLoads). Apply it here before session
	// restore so an explicit launch position wins over a restored cursor.
	if request.Cursor == nil {
		if pos, ok := m.startupCursors[filepath.Clean(msg.Path)]; ok {
			delete(m.startupCursors, msg.Path)
			delete(m.startupCursors, filepath.Clean(msg.Path))
			request.Cursor = &pos
		}
	}

	// A navigation belongs to this request, not whichever tab happens to be
	// active when disk I/O completes. Session state gets the same treatment.
	if request.Cursor != nil {
		if request.CursorEncoding != "" {
			position, err := resolvePendingLSPPositionRope(m.editors[tabIdx].Buffer.Rope(), *request.Cursor, request.CursorEncoding)
			if err != nil {
				// Protocol positions that fail conversion must not move the cursor.
				m.status = fmt.Sprintf("LSP navigation position rejected: %v", err)
			} else {
				m.editors[tabIdx].Buffer.SetCursor(position)
				m.editors[tabIdx].EnsureCursorVisible()
			}
		} else {
			// CLI / plain positions: clamp to document bounds after load.
			line := min(max(0, request.Cursor.Line), max(0, m.editors[tabIdx].Buffer.LineCount()-1))
			col := min(max(0, request.Cursor.Col), m.editors[tabIdx].Buffer.Rope().LineLen(line))
			m.editors[tabIdx].Buffer.SetCursor(text.Position{Line: line, Col: col})
			m.editors[tabIdx].EnsureCursorVisible()
		}
	}
	if request.Session == nil {
		if restored, ok := m.restoredTabs[msg.EditorID]; ok {
			copy := restored
			request.Session = &copy
		}
	}
	if request.Session != nil {
		if request.Cursor == nil {
			line := min(max(0, request.Session.CursorLine), max(0, m.editors[tabIdx].Buffer.LineCount()-1))
			col := min(max(0, request.Session.CursorCol), m.editors[tabIdx].Buffer.Rope().LineLen(line))
			m.editors[tabIdx].Buffer.SetCursor(text.Position{Line: line, Col: col})
			m.editors[tabIdx].EnsureCursorVisible()
		}
		m.editors[tabIdx].Viewport.ScrollY = max(0, request.Session.ScrollY)
		m.editors[tabIdx].Viewport.WrapScrollY = max(0, request.Session.WrapScrollY)
		delete(m.restoredTabs, msg.EditorID)
	}

	// Watch this file for external changes
	if m.watcher != nil && msg.Path != "" {
		m.watcher.WatchFile(msg.Path)
	}

	// Async tokenize + LSP open
	var events []plugin.EventContext
	events = append(events,
		m.pluginEvent(plugin.EventBufRead, msg.Path),
		m.pluginEvent(plugin.EventFileType, msg.Path),
	)
	if tabIdx == m.activeTab {
		events = append(events, m.pluginEvent(plugin.EventBufEnter, msg.Path))
	}
	editorID := m.editors[tabIdx].ID()
	version := m.editors[tabIdx].Buffer.Version()
	return m, tea.Batch(
		prepareExternalFoldRegionsCmd(editorID, version, snapshot),
		m.editors[tabIdx].ScheduleInitialTokenize(),
		m.lspDidOpen(m.editors[tabIdx].Buffer),
		m.triggerPluginEvents(events...),
	)
}

// findReplaceableTab returns the index of a preview tab or empty untitled tab, or -1.
func (m Model) findReplaceableTab() int {
	// Prefer replacing an existing preview tab
	idx := m.tabBar.FindPreviewTab()
	if idx >= 0 &&
		idx < len(m.editors) &&
		!m.tabBar.Tabs[idx].Dirty &&
		!m.editors[idx].Buffer.Dirty() {
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

func (m Model) closeCurrentTabSafe() (tea.Model, tea.Cmd) {
	return m.closeTabSafe(m.activeTab)
}

func (m Model) closeTabSafe(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.editors) {
		return m, nil
	}
	buf := m.editors[idx].Buffer
	if buf.Dirty() {
		m.cancelActiveEditorDrag()
		name := filepath.Base(buf.FilePath)
		if name == "." || buf.FilePath == "" {
			name = m.tabBar.Tabs[idx].Label
		}
		m.pendingCloseTab = idx
		buttons := []overlay.Button{
			{Label: "Close Without Saving", Style: lipgloss.NewStyle().Background(ui.Nord11).Foreground(ui.Nord6).Padding(0, 2), Action: ForceCloseTabMsg{Index: idx}},
			{Label: "Cancel", Action: overlay.ButtonAction{Label: "Cancel"}},
		}
		if buf.FilePath != "" {
			buttons = append([]overlay.Button{
				{Label: "Save & Close", Style: lipgloss.NewStyle().Background(ui.Nord14).Foreground(ui.Nord0).Padding(0, 2), Action: SaveAndCloseTabMsg{Index: idx}},
			}, buttons...)
		}
		confirm := overlay.NewConfirm(
			"Unsaved Changes",
			fmt.Sprintf("%q has unsaved changes.", name),
			nil,
			buttons,
			m.theme,
		)
		m.unsavedConfirm = confirm
		return m, nil
	}
	return m.closeTab(idx)
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
	m.removeLoadsForEditor(m.editors[idx].ID())
	closingPath := buf.FilePath
	wasActive := idx == m.activeTab
	if wasActive {
		m.overlayRequests.invalidateAll()
		m.documentRequests.invalidateActiveRequests()
	}
	m.documentRequests.invalidateDocument(closingPath)
	lastPathOwner := closingPath != "" && !m.hasOtherEditorForPath(closingPath, idx)
	if m.watcher != nil && lastPathOwner {
		m.watcher.UnwatchFile(closingPath)
	}
	if lastPathOwner {
		m.cancelLSPChangePreparation(closingPath)
	}
	if lastPathOwner && m.lspMgr != nil {
		m.lspMgr.CloseDocument(closingPath)
	}
	if lastPathOwner {
		m.clearClosedExternalPathState(closingPath)
	}

	// If closing the last tab, show the welcome screen with no tabs
	if len(m.editors) <= 1 {
		cmd := m.triggerPluginEvents(
			m.pluginEvent(plugin.EventBufLeave, closingPath),
			m.pluginEvent(plugin.EventBufDelete, closingPath),
		)
		m.editors = nil
		m.tabBar.Tabs = nil
		m.activeTab = 0
		m.tabBar.ActiveIdx = 0
		w := editor.NewWelcome(m.theme)
		m.welcome = &w
		m.relayout()
		return m, tea.Batch(cmd, m.welcome.Init())
	}

	m.editors = append(m.editors[:idx], m.editors[idx+1:]...)
	m.tabBar.RemoveTab(idx)
	m.activateTab(m.tabBar.ActiveIdx)

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
	var events []plugin.EventContext
	if wasActive {
		events = append(events, m.pluginEvent(plugin.EventBufLeave, closingPath))
	}
	events = append(events, m.pluginEvent(plugin.EventBufDelete, closingPath))
	if wasActive && m.activeEditor() != nil {
		events = append(events, m.pluginEvent(plugin.EventBufEnter, m.activeEditor().Buffer.FilePath))
	}
	return m, m.triggerPluginEvents(events...)
}

func (m Model) hasOtherEditorForPath(path string, excluded int) bool {
	clean := cleanExternalConflictPath(path)
	if clean == "" {
		return false
	}
	for i := range m.editors {
		if i == excluded {
			continue
		}
		if cleanExternalConflictPath(m.editors[i].Buffer.FilePath) == clean {
			return true
		}
	}
	return false
}

func (m *Model) clearClosedExternalPathState(path string) {
	clean := cleanExternalConflictPath(path)
	if clean == "" {
		return
	}
	m.clearExternalConflict(clean)
	delete(m.externalChangeObserved, clean)
	delete(m.lastSaveWatcherWatermarks, clean)
	if m.externalReads.pending != nil {
		delete(m.externalReads.pending, clean)
	}
}

func (m Model) findNext() (tea.Model, tea.Cmd) {
	if len(m.lastSearchResults) == 0 {
		m.status = "No search results"
		return m, nil
	}
	// Find next result after current cursor
	ed := m.activeEditor()
	if ed != nil {
		curFile := ed.Buffer.FilePath
		curLine := ed.Buffer.Cursor.Line
		curCol := ed.Buffer.Cursor.Col
		for i := 0; i < len(m.lastSearchResults); i++ {
			idx := (m.lastSearchIndex + 1 + i) % len(m.lastSearchResults)
			r := m.lastSearchResults[idx]
			rPath := r.FilePath
			if !filepath.IsAbs(rPath) {
				rPath = filepath.Join(m.rootDir, rPath)
			}
			if rPath == curFile && (r.Line > curLine || (r.Line == curLine && r.Col > curCol)) {
				m.lastSearchIndex = idx
				pos := text.Position{Line: r.Line, Col: r.Col}
				m.setPendingCursor(rPath, pos)
				m.status = fmt.Sprintf("Match %d/%d", idx+1, len(m.lastSearchResults))
				return m.openFilePinned(rPath)
			}
		}
	}
	// Wrap around: use next index
	m.lastSearchIndex = (m.lastSearchIndex + 1) % len(m.lastSearchResults)
	r := m.lastSearchResults[m.lastSearchIndex]
	rPath := r.FilePath
	if !filepath.IsAbs(rPath) {
		rPath = filepath.Join(m.rootDir, rPath)
	}
	pos := text.Position{Line: r.Line, Col: r.Col}
	m.setPendingCursor(rPath, pos)
	m.status = fmt.Sprintf("Match %d/%d (wrapped)", m.lastSearchIndex+1, len(m.lastSearchResults))
	return m.openFilePinned(rPath)
}

func (m Model) findPrev() (tea.Model, tea.Cmd) {
	if len(m.lastSearchResults) == 0 {
		m.status = "No search results"
		return m, nil
	}
	ed := m.activeEditor()
	if ed != nil {
		curFile := ed.Buffer.FilePath
		curLine := ed.Buffer.Cursor.Line
		curCol := ed.Buffer.Cursor.Col
		for i := 0; i < len(m.lastSearchResults); i++ {
			idx := (m.lastSearchIndex - 1 - i + len(m.lastSearchResults)) % len(m.lastSearchResults)
			r := m.lastSearchResults[idx]
			rPath := r.FilePath
			if !filepath.IsAbs(rPath) {
				rPath = filepath.Join(m.rootDir, rPath)
			}
			if rPath == curFile && (r.Line < curLine || (r.Line == curLine && r.Col < curCol)) {
				m.lastSearchIndex = idx
				pos := text.Position{Line: r.Line, Col: r.Col}
				m.setPendingCursor(rPath, pos)
				m.status = fmt.Sprintf("Match %d/%d", idx+1, len(m.lastSearchResults))
				return m.openFilePinned(rPath)
			}
		}
	}
	m.lastSearchIndex = (m.lastSearchIndex - 1 + len(m.lastSearchResults)) % len(m.lastSearchResults)
	r := m.lastSearchResults[m.lastSearchIndex]
	rPath := r.FilePath
	if !filepath.IsAbs(rPath) {
		rPath = filepath.Join(m.rootDir, rPath)
	}
	pos := text.Position{Line: r.Line, Col: r.Col}
	m.setPendingCursor(rPath, pos)
	m.status = fmt.Sprintf("Match %d/%d (wrapped)", m.lastSearchIndex+1, len(m.lastSearchResults))
	return m.openFilePinned(rPath)
}

func (m Model) newUntitledTab() (tea.Model, tea.Cmd) {
	m.cancelSessionRestore()
	if m.welcome != nil {
		m.welcome.Dismiss()
	}
	m.untitledCounter++
	untitledNum := m.untitledCounter
	label := fmt.Sprintf("Untitled-%d", untitledNum)
	buf := text.NewBuffer()
	cfg := editor.DefaultConfig()
	if len(m.editors) > 0 {
		cfg = m.editors[0].Config
	}
	ed := editor.New(buf, m.theme, cfg)
	m.editors = append(m.editors, ed)
	idx := len(m.editors) - 1
	m.tabBar.AddTab(label, "")
	m.tabBar.PinTab(idx)
	m.activateTab(idx)
	m.setFocus(FocusEditor)
	m.relayout()
	return m, nil
}

func (m Model) handleTabBarClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Check close buttons first
	for i, tab := range m.tabBar.Tabs {
		if zone.Get(editor.TabCloseZoneID(tab)).InBounds(msg) {
			return m.closeTabSafe(i)
		}
	}
	// Then check label zones for switching
	for i, tab := range m.tabBar.Tabs {
		if zone.Get(editor.TabZoneID(tab)).InBounds(msg) {
			m.activateTab(i)
			return m, nil
		}
	}
	return m, nil
}

func (m Model) openSearch(mode search.Mode) (tea.Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
	m.showSearch = true
	m.searchMode = mode
	m.searchM = search.New(m.theme, m.rootDir, mode)
	m.searchM.SetSize(m.width, m.height-2)
	cmd := m.searchM.Focus()
	return m, cmd
}

func (m Model) openSearchReplace() (tea.Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
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
	msg = m.searchOverlayLocalMouseMsg(msg)
	m.searchM, cmd = m.searchM.Update(msg)
	return m, cmd
}

func (m Model) searchOverlayLocalMouseMsg(msg tea.Msg) tea.Msg {
	x, y := m.searchM.OverlayOrigin(m.width, m.height)
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		mouse.X -= x
		mouse.Y -= y
		return tea.MouseClickMsg(mouse)
	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		mouse.X -= x
		mouse.Y -= y
		return tea.MouseMotionMsg(mouse)
	case tea.MouseReleaseMsg:
		mouse := msg.Mouse()
		mouse.X -= x
		mouse.Y -= y
		return tea.MouseReleaseMsg(mouse)
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		mouse.X -= x
		mouse.Y -= y
		return tea.MouseWheelMsg(mouse)
	default:
		return msg
	}
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
				m.setPendingCursor(prob.FilePath, pos)
				return m.openFilePinned(prob.FilePath)
			}
			return m, nil
		case "esc", "escape":
			// Switch back to editor focus
			m.setFocus(FocusEditor)
			return m, nil
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.problemsPanel.ScrollUp(3)
		case tea.MouseWheelDown:
			m.problemsPanel.ScrollDown(3)
		}
		return m, nil
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			clickIdx := m.problemsPanel.ScrollY() + mouse.Y
			if m.problemsPanel.SelectAt(clickIdx) {
				if prob := m.problemsPanel.SelectedProblem(); prob != nil {
					pos := text.Position{Line: prob.Line, Col: prob.Col}
					m.setPendingCursor(prob.FilePath, pos)
					return m.openFilePinned(prob.FilePath)
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
			m.setFocus(FocusEditor)
			return m, nil
		case "c":
			return m.handleDebugAction(debugActionContinue)
		case "n":
			return m.handleDebugAction(debugActionNext)
		case "i":
			return m.handleDebugAction(debugActionStepIn)
		case "o":
			return m.handleDebugAction(debugActionStepOut)
		case "q":
			return m.handleDebugStop("Debugging stopped")
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
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			if idx := m.debuggerPanel.StackFrameAtY(mouse.Y); idx >= 0 {
				cmd := m.debuggerPanel.SelectFrame(idx)
				return m, cmd
			}
		}
		return m, nil
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.debuggerPanel.ScrollUp(3)
		case tea.MouseWheelDown:
			m.debuggerPanel.ScrollDown(3)
		}
		return m, nil
	}
	return m, nil
}

// handleDebuggerControl dispatches a click on one of the debugger panel's
// control buttons through the same paths as the keyboard bindings.
func (m Model) handleDebuggerControl(control debugger.DebugControl) (tea.Model, tea.Cmd) {
	switch control {
	case debugger.DebugStart:
		return m.handleDebugStart()
	case debugger.DebugContinue:
		return m.handleDebugAction(debugActionContinue)
	case debugger.DebugNext:
		return m.handleDebugAction(debugActionNext)
	case debugger.DebugStepIn:
		return m.handleDebugAction(debugActionStepIn)
	case debugger.DebugStepOut:
		return m.handleDebugAction(debugActionStepOut)
	case debugger.DebugStop:
		return m.handleDebugStop("Debugging stopped")
	}
	return m, nil
}

// jumpToBreakpoint opens the clicked breakpoint's file at its line.
func (m Model) jumpToBreakpoint(idx int) (tea.Model, tea.Cmd) {
	bps := m.debuggerPanel.Breakpoints()
	if idx < 0 || idx >= len(bps) {
		return m, nil
	}
	bp := bps[idx]
	m.setPendingCursor(bp.FilePath, text.Position{Line: bp.Line, Col: 0})
	return m.openFilePinned(bp.FilePath)
}

// updateSettings handles input for the Settings overlay.
func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settingsSaving {
		// The command owns a snapshot of every visible value. Do not accept a
		// second edit until it completes, otherwise a successful older snapshot
		// could incorrectly clear the dirty state of a newer edit.
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+s":
			return m.saveSettings()
		case "esc", "escape", "ctrl+,":
			m = m.requestCloseSettings()
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
				case settings.TypeString:
					m.settingsM.CycleStringValue()
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
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.settingsM.SelectPrevSetting()
		case tea.MouseWheelDown:
			m.settingsM.SelectNextSetting()
		}
		return m, nil
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		x, y, width, height := m.settingsModalGeometry()
		if mouse.Button != tea.MouseLeft || mouse.X < x || mouse.X >= x+width || mouse.Y < y || mouse.Y >= y+height {
			return m, nil
		}
		// The model is rendered within a rounded border and one-cell/two-cell
		// padding, so route clicks in content rather than screen coordinates.
		m.settingsM.HandleMouseClick(mouse.X-x-3, mouse.Y-y-2)
		return m, nil
	}
	return m, nil
}

func (m Model) saveSettings() (tea.Model, tea.Cmd) {
	if m.settingsSaving {
		return m, nil
	}
	cfg, err := m.settingsM.Config()
	if err != nil {
		m.settingsM.SetStatus(fmt.Sprintf("Cannot save settings: %v", err))
		return m, nil
	}
	path := m.settingsM.ConfigPath()
	if path == "" {
		path = config.ConfigPath()
	}
	m.settingsSaving = true
	m.settingsM.SetStatus("Saving settings…")
	return m, func() tea.Msg {
		outcome, err := config.SaveToWithOutcome(path, cfg)
		return settingsSaveResultMsg{Config: cfg, Outcome: outcome, Err: err}
	}
}

// applySavedSettings updates only values that are safe to change in a running
// TUI. Themes are deliberately deferred: several sub-models cache styles, and
// Settings explicitly tells the user a restart is required for a theme change.
func (m *Model) applySavedSettings(cfg config.Config) {
	m.appCfg = cfg
	m.showTree = cfg.UI.ShowTree
	for i := range m.editors {
		ed := &m.editors[i]
		ed.Config.TabSize = cfg.Editor.TabSize
		ed.Config.InsertTabs = cfg.Editor.InsertTabs
		ed.Config.AutoIndent = cfg.Editor.AutoIndent
		ed.Config.WordWrap = cfg.Editor.WordWrap
		ed.Config.ScrollMargin = cfg.Editor.ScrollMargin
		if !cfg.Editor.WordWrap {
			ed.Wrap = nil
		}
		ed.SetSize(ed.Viewport.Width, ed.Viewport.Height)
	}
	m.relayout()
}

func (m Model) handleDiagnostics(msg lsp.DiagnosticsMsg) (tea.Model, tea.Cmd) {
	m.ensureDiagnosticIndexes()
	path := lsp.URIToPath(msg.URI)
	if msg.HasVersion {
		for i := range m.editors {
			if m.editors[i].Buffer.FilePath == path && m.editors[i].Buffer.Version() != msg.Version {
				// Diagnostics are tied to the server's document version. Do not
				// underline newer text with ranges computed for an older snapshot.
				return m, nil
			}
		}
	}

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

	// Store full diagnostics in LSP coordinator (single source of truth)
	if m.coordinator != nil {
		_ = m.coordinator.HandleMessage(msg)
	}

	// Update the changed file and only its ancestor severity counters. The
	// shared tree map stays attached to the tree model, so a notification never
	// copies every file and directory diagnostic.
	oldSeverity, hadOldSeverity := m.fileDiagnostics[path]
	newSeverity, hasNewSeverity := worstDiagnosticSeverity(msg.Diagnostics)
	if hadOldSeverity {
		m.adjustDirectoryDiagnostics(path, oldSeverity, -1)
	}
	if !hasNewSeverity {
		delete(m.fileDiagnostics, path)
		delete(m.treeDiagnostics, path)
	} else {
		m.fileDiagnostics[path] = newSeverity
		m.treeDiagnostics[path] = newSeverity
		m.adjustDirectoryDiagnostics(path, newSeverity, 1)
	}

	// Sync to matching tab
	for i, tab := range m.tabBar.Tabs {
		if tab.FilePath == path {
			sev := m.fileDiagnostics[path] // 0 if deleted
			m.tabBar.Tabs[i].DiagSeverity = sev
		}
	}

	// Update only the received file in the problems panel. Its model keeps the
	// global presentation order through a linear merge, avoiding an aggregate
	// coordinator walk and quadratic sort on each LSP notification.
	m.updateProblemsPanelForFile(path, msg.Diagnostics)

	return m, nil
}

func (m *Model) ensureDiagnosticIndexes() {
	if m.fileDiagnostics == nil {
		m.fileDiagnostics = make(map[string]int)
	}
	if m.dirDiagnostics == nil {
		m.dirDiagnostics = make(map[string]int)
	}
	if m.dirDiagnosticCounts == nil {
		m.dirDiagnosticCounts = make(map[string]map[int]int)
	}
	if m.treeDiagnostics == nil {
		m.treeDiagnostics = make(map[string]int)
	}
}

func worstDiagnosticSeverity(diagnostics []lsp.Diagnostic) (int, bool) {
	if len(diagnostics) == 0 {
		return 0, false
	}
	worst := int(diagnostics[0].Severity)
	for _, diagnostic := range diagnostics[1:] {
		if severity := int(diagnostic.Severity); severity < worst {
			worst = severity
		}
	}
	return worst, true
}

func (m *Model) adjustDirectoryDiagnostics(path string, severity, delta int) {
	for dir := filepath.Dir(path); dir != m.rootDir && dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		counts := m.dirDiagnosticCounts[dir]
		if counts == nil {
			counts = make(map[int]int)
			m.dirDiagnosticCounts[dir] = counts
		}
		counts[severity] += delta
		if counts[severity] <= 0 {
			delete(counts, severity)
		}
		if len(counts) == 0 {
			delete(m.dirDiagnosticCounts, dir)
			delete(m.dirDiagnostics, dir)
			delete(m.treeDiagnostics, dir)
			continue
		}
		worst := 0
		for candidate := range counts {
			if worst == 0 || candidate < worst {
				worst = candidate
			}
		}
		m.dirDiagnostics[dir] = worst
		m.treeDiagnostics[dir] = worst
	}
}

func (m *Model) updateProblemsPanelForFile(path string, diagnostics []lsp.Diagnostic) {
	fileProblems := make([]problems.Problem, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		fileProblems = append(fileProblems, problems.Problem{
			FilePath: path,
			Line:     diagnostic.Range.Start.Line,
			Col:      diagnostic.Range.Start.Character,
			EndLine:  diagnostic.Range.End.Line,
			EndCol:   diagnostic.Range.End.Character,
			Severity: int(diagnostic.Severity),
			Message:  diagnostic.Message,
			Source:   diagnostic.Source,
		})
	}
	m.problemsPanel.ReplaceFileProblems(path, fileProblems)
}

// sortProblems sorts problems by severity, path, and line.
func sortProblems(probs []problems.Problem) {
	slices.SortStableFunc(probs, problems.Compare)
}

// LSP helpers

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

type breakpointEntry struct {
	Line    int
	Enabled bool
}

// toggleBreakpoint cycles breakpoint state: none → active → disabled → removed.
func (m *Model) toggleBreakpoint(filePath string, line int) tea.Cmd {
	entries := m.breakpoints[filePath]

	// Check if breakpoint already exists at this line
	idx := -1
	for i, bp := range entries {
		if bp.Line == line {
			idx = i
			break
		}
	}

	if idx >= 0 {
		if entries[idx].Enabled {
			// Active → disabled
			entries[idx].Enabled = false
		} else {
			// Disabled → remove
			entries = append(entries[:idx], entries[idx+1:]...)
		}
	} else {
		// Add breakpoint (active) in sorted position
		bp := breakpointEntry{Line: line, Enabled: true}
		inserted := false
		for i, e := range entries {
			if line < e.Line {
				entries = append(entries[:i+1], entries[i:]...)
				entries[i] = bp
				inserted = true
				break
			}
		}
		if !inserted {
			entries = append(entries, bp)
		}
	}

	if len(entries) == 0 {
		delete(m.breakpoints, filePath)
	} else {
		m.breakpoints[filePath] = entries
	}
	m.rebuildBreakpointGutter(filePath)
	m.projectDebugGutterForPath(filePath)

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
	for fp, entries := range m.breakpoints {
		for _, bp := range entries {
			bps = append(bps, debugger.Breakpoint{
				FilePath: fp,
				Line:     bp.Line,
				Enabled:  bp.Enabled,
			})
		}
	}
	m.debuggerPanel.SetBreakpoints(bps)
}

// sendBreakpointsToDAP sends breakpoints for a file to the DAP adapter.
func (m Model) sendBreakpointsToDAP(filePath string) tea.Cmd {
	mgr := m.debugMgr
	entries := m.breakpoints[filePath]
	// DAP uses 1-based lines; only send enabled breakpoints
	var dapLines []int
	for _, bp := range entries {
		if bp.Enabled {
			dapLines = append(dapLines, bp.Line+1)
		}
	}
	return func() tea.Msg {
		if _, err := mgr.SetBreakpoints(filePath, dapLines); err != nil {
			log.Error("dap: failed to set breakpoints", "file", filePath, "err", err)
		}
		return nil
	}
}

func (m Model) syncAllBreakpointsToDAP() tea.Cmd {
	if len(m.breakpoints) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.breakpoints))
	for filePath := range m.breakpoints {
		if cmd := m.sendBreakpointsToDAP(filePath); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
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
		cmd := m.fetchDebugStateForGeneration(m.debugLifecycle.generation)
		return m, tea.Batch(cmd, m.listenDAP())

	case dap.ContinuedEventMsg:
		m.debuggerPanel.SetState(dap.StateRunning)
		m.setExecutionLocation("", -1)
		m.status = "Debugging"
		return m, m.listenDAP()

	case dap.TerminatedEventMsg:
		return m.handleDebugStop("Debug session terminated")

	case dap.ExitedEventMsg:
		return m.handleDebugStop(fmt.Sprintf("Process exited with code %d", inner.ExitCode))

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
	return m.fetchDebugStateForGeneration(m.debugLifecycle.generation)
}

func (m Model) fetchDebugStateForGeneration(generation uint64) tea.Cmd {
	mgr := m.debugMgr
	return func() tea.Msg {
		frames, err := mgr.GetStackTrace()
		if err != nil || len(frames) == 0 {
			return debugStateMsg{Generation: generation}
		}

		// Get scopes for top frame
		scopes, err := mgr.GetScopes(frames[0].Id)
		if err != nil {
			return debugStateMsg{Generation: generation, Frames: frames}
		}

		// Get variables from the first non-expensive scope (usually "Locals")
		var vars []dap.Variable
		for _, scope := range scopes {
			if !scope.Expensive && scope.VariablesReference > 0 {
				vars, _ = mgr.GetVariables(scope.VariablesReference)
				break
			}
		}

		return debugStateMsg{Generation: generation, Frames: frames, Variables: vars}
	}
}

// lspStartupMessage turns a server startup failure into something a user can
// act on. A missing binary gets its install command; hitting the concurrent
// client cap is called out explicitly so it is not mistaken for a missing tool.
func lspStartupMessage(command string, err error) string {
	switch {
	case errors.Is(err, lsp.ErrClientCapacity):
		return fmt.Sprintf("%s not started: too many language servers running", command)
	case toolpath.IsMissing(err):
		if hint := toolpath.Hint(command); hint != "" {
			return fmt.Sprintf("%s not found. Install with: %s", command, hint)
		}
		return fmt.Sprintf("%s not found in PATH", command)
	default:
		return err.Error()
	}
}

func (m Model) lspDidOpen(buf *text.Buffer) tea.Cmd {
	if buf.FilePath == "" || m.lspMgr == nil {
		return nil
	}
	mgr := m.lspMgr
	filePath := buf.FilePath
	snapshot := buf.Rope()
	version := buf.Version()
	generation := mgr.BeginDocument(filePath)
	budget := m.lspMaterializationBudget()
	return func() tea.Msg {
		// A close can happen while this command waits behind another large LSP
		// snapshot. Avoid creating a needless full string copy for that retired
		// lifecycle; Manager repeats the generation check at its protocol edge.
		if mgr.DocumentGeneration(filePath) != generation {
			return nil
		}
		content, ok := budget.materializeSnapshotIf(snapshot, nil, func() bool {
			return mgr.DocumentGeneration(filePath) == generation
		})
		if !ok {
			return nil
		}
		cfg := mgr.ConfigForFile(filePath)
		langID := ""
		if cfg != nil {
			langID = cfg.LanguageID
		}
		if err := mgr.OpenDocument(filePath, generation, langID, version, content); err != nil {
			// Reporting readiness here would claim language features are
			// available when no server ever started, leaving a missing or
			// unresolvable server indistinguishable from a silently broken one.
			command := ""
			if cfg != nil {
				command = cfg.Command
			}
			return lsp.LspErrorMsg{
				Method:  "textDocument/didOpen",
				Message: lspStartupMessage(command, err),
			}
		}
		return LspReadyMsg{FilePath: filePath, OpenVersion: version}
	}
}

func (m Model) requestDefinition() (Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || ed.Buffer.FilePath == "" {
		return m, nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestDefinition, filePath)
	if !ok {
		return m, nil
	}
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		locs, err := client.DefinitionContext(requestContext, lsp.FileURI(filePath), line, col)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/definition", Message: err.Error()}
		}
		return lsp.DefinitionResultMsg{DocumentRequestMetadata: metadata, Locations: locs}
	}
}

func (m Model) requestFoldingRanges(filePath string) (Model, tea.Cmd) {
	if filePath == "" {
		return m, nil
	}
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestFolding, filePath)
	if !ok {
		return m, nil
	}
	mgr := m.lspMgr
	return m, func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		ranges, err := client.FoldingRangeContext(requestContext, lsp.FileURI(filePath))
		if err != nil || len(ranges) == 0 {
			return nil
		}
		return lsp.FoldingRangeResultMsg{DocumentRequestMetadata: metadata, FilePath: filePath, Ranges: ranges}
	}
}

func (m Model) requestCodeActions() (Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || ed.Buffer.FilePath == "" {
		return m, nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestCodeAction, filePath)
	if !ok {
		return m, nil
	}
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	diagnostics := snapshotCodeActionDiagnostics(ed.Diagnostics, line)
	requester := m.codeActionRequester
	return m, func() tea.Msg {
		var (
			actions []lsp.CodeAction
			err     error
		)
		if requester != nil {
			actions, err = requester(requestContext, filePath, line, col, diagnostics)
		} else {
			client := mgr.ClientForFile(filePath)
			if client == nil {
				return nil
			}
			actions, err = client.CodeActionContext(requestContext, lsp.FileURI(filePath), line, col, line, col, diagnostics)
		}
		if err != nil || len(actions) == 0 {
			return nil
		}
		return lsp.CodeActionResultMsg{DocumentRequestMetadata: metadata, Actions: actions}
	}
}

const codeActionCommandTimeout = 5 * time.Second

// executeSelectedCodeAction runs only a command that the user explicitly
// chose from the code-action picker. The request itself happens in a tea.Cmd;
// this method only captures immutable identity and cancellation state.
func (m Model) executeSelectedCodeAction(action lsp.CodeAction, _ lsp.DocumentRequestMetadata) (Model, tea.Cmd) {
	if action.Command == nil || strings.TrimSpace(action.Command.Command) == "" {
		m.status = fmt.Sprintf("Code action %q has an empty server command", action.Title)
		return m, nil
	}
	ed := m.activeEditor()
	if ed == nil || ed.Buffer.FilePath == "" {
		m.status = "Code action expired; request it again"
		return m, nil
	}
	return m.executeSelectedCodeActionForFile(action, ed.Buffer.FilePath)
}

// executeSelectedCodeActionForFile lets an asynchronously prepared workspace
// edit start its explicit server command against the document that produced
// the action, even if the user focused another tab while disk work ran.
func (m Model) executeSelectedCodeActionForFile(action lsp.CodeAction, filePath string) (Model, tea.Cmd) {
	if action.Command == nil || strings.TrimSpace(action.Command.Command) == "" {
		m.status = fmt.Sprintf("Code action %q has an empty server command", action.Title)
		return m, nil
	}
	metadata, ok := m.beginDocumentRequest(documentRequestCodeAction, filePath)
	if !ok {
		m.status = "Code action expired; request it again"
		return m, nil
	}
	if m.codeActionCommandStop != nil {
		m.codeActionCommandStop()
	}
	m.codeActionCommandGen++
	generation := m.codeActionCommandGen
	ctx, cancel := context.WithCancel(context.Background())
	m.codeActionCommandStop = cancel
	manager := m.lspMgr
	filePath = metadata.FilePath
	command := action.Command.Command
	arguments := append([]any(nil), action.Command.Arguments...)
	title := action.Title
	m.status = fmt.Sprintf("Running code action: %s", title)

	return m, func() tea.Msg {
		defer cancel()
		requestCtx, requestCancel := context.WithTimeout(ctx, codeActionCommandTimeout)
		defer requestCancel()
		if manager == nil {
			return lspCodeActionCommandResultMsg{
				Generation: generation,
				Metadata:   metadata,
				Title:      title,
				Err:        errors.New("language server is unavailable"),
			}
		}
		client := manager.ClientForFile(filePath)
		if client == nil {
			return lspCodeActionCommandResultMsg{
				Generation: generation,
				Metadata:   metadata,
				Title:      title,
				Err:        errors.New("language server is unavailable"),
			}
		}
		_, err := client.ExecuteCommand(requestCtx, command, arguments)
		return lspCodeActionCommandResultMsg{
			Generation: generation,
			Metadata:   metadata,
			Title:      title,
			Err:        err,
		}
	}
}

func (m Model) requestDocumentSymbols() (Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || ed.Buffer.FilePath == "" {
		return m, nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestSymbols, filePath)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		symbols, err := client.DocumentSymbolContext(requestContext, lsp.FileURI(filePath))
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/documentSymbol", Message: err.Error()}
		}
		return lsp.DocumentSymbolResultMsg{DocumentRequestMetadata: metadata, Symbols: symbols}
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
		if ed == nil {
			return m, nil
		}
		maxLine := ed.Buffer.LineCount() - 1
		if lineNum < 0 {
			lineNum = 0
		}
		if lineNum > maxLine {
			lineNum = maxLine
		}
		ed.Buffer.ClearSelection()
		ed.Buffer.SetCursor(text.Position{Line: lineNum, Col: 0})
		ed.EnsureCursorVisible()
		m.setEditor(m.activeTab, *ed)
		return m, nil
	case "backspace":
		m.goToLineInput = deleteLastRune(m.goToLineInput)
		return m, nil
	default:
		if msg.Text != "" && msg.Text >= "0" && msg.Text <= "9" {
			m.goToLineInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) handleSaveAsInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.saveAsMode = false
		m.saveAsInput = ""
		return m, nil
	case "enter":
		m.saveAsMode = false
		newPath := m.saveAsInput
		m.saveAsInput = ""
		if newPath == "" {
			return m, nil
		}
		ed := m.activeEditor()
		if ed == nil {
			return m, nil
		}
		return m, m.beginSaveAsForTab(m.activeTab, newPath)
	case "backspace":
		m.saveAsInput = deleteLastRune(m.saveAsInput)
		return m, nil
	default:
		if msg.Text != "" {
			m.saveAsInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) handleContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "goto_definition":
		return m.requestDefinition()
	case "find_references":
		return m.requestReferences()
	case "rename_symbol":
		m.renameMode = true
		m.renameInput = ""
		return m, nil
	}
	return m, nil
}

func (m Model) requestReferences() (Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return m, nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestReferences, filePath)
	if !ok {
		return m, nil
	}
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		locs, err := client.ReferencesContext(requestContext, lsp.FileURI(filePath), line, col)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			return lsp.LspErrorMsg{Method: "textDocument/references", Message: err.Error()}
		}
		return lsp.ReferencesResultMsg{DocumentRequestMetadata: metadata, Locations: locs}
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

func (m Model) requestRename(newName string) (Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed.Buffer.FilePath == "" {
		return m, nil
	}
	mgr := m.lspMgr
	filePath := ed.Buffer.FilePath
	metadata, requestContext, ok := m.beginDocumentRequestContext(documentRequestRename, filePath)
	if !ok {
		return m, nil
	}
	line := ed.Buffer.Cursor.Line
	col := ed.Buffer.Cursor.Col
	return m, func() tea.Msg {
		client := mgr.ClientForFile(filePath)
		if client == nil {
			return nil
		}
		edits, err := client.RenameContext(requestContext, lsp.FileURI(filePath), line, col, newName)
		if err != nil || (len(edits.Changes) == 0 && len(edits.DocumentChanges) == 0) {
			return nil
		}
		return lsp.RenameResultMsg{DocumentRequestMetadata: metadata, Edit: edits}
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
		return m.requestRename(newName)
	case "backspace":
		m.renameInput = deleteLastRune(m.renameInput)
		return m, nil
	default:
		if msg.Text != "" {
			m.renameInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) showTreeContextMenu(x, screenY, treeY int) (tea.Model, tea.Cmd) {
	// Get the entry at the clicked position from the tree
	entry := m.tree.EntryAtY(treeY)

	var items []editor.ContextMenuItem
	if entry == nil {
		// Clicked empty area — offer root-level actions
		m.treeContextPath = m.rootDir
		items = []editor.ContextMenuItem{
			{Label: "New File...", Action: "tree_new_file"},
			{Label: "New Folder...", Action: "tree_new_folder"},
		}
		m.showContextMenu(&m.treeContextMenu, items, x, screenY)
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
			{Label: "Rename...", Action: "tree_rename"},
			{Label: "Duplicate...", Action: "tree_copy"},
			{Label: "Move to...", Action: "tree_move"},
			{Label: ""}, // separator
			{Label: "Copy Path", Action: "tree_copy_path"},
			{Label: "Stash to Vault", Action: "tree_stash"},
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
			{Label: "Rename...", Action: "tree_rename"},
			{Label: "Duplicate...", Action: "tree_copy"},
			{Label: "Move to...", Action: "tree_move"},
			{Label: ""}, // separator
			{Label: "Copy Path", Action: "tree_copy_path"},
			{Label: "Stash to Vault", Action: "tree_stash"},
			{Label: "Delete", Action: "tree_delete"},
		}
	}

	m.showContextMenu(&m.treeContextMenu, items, x, screenY)
	return m, nil
}

func (m Model) handleTreeContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "tree_open":
		return m.openFile(m.treeContextPath)
	case "tree_open_new_tab":
		return m.openFileForceNewTab(m.treeContextPath)
	case "tree_copy_path":
		relPath, err := filepath.Rel(m.rootDir, m.treeContextPath)
		if err != nil {
			relPath = m.treeContextPath
		}
		return m, treeCopyPathCmd(relPath)
	case "tree_toggle":
		var cmd tea.Cmd
		m.tree, cmd = m.tree.ToggleEntry(m.treeContextPath)
		return m, cmd
	case "tree_rename":
		m.treeRenameMode = true
		m.treeCopyMode = false
		m.treeMoveMode = false
		m.treeEditTarget = m.treeContextPath
		m.treeEditInput = filepath.Base(m.treeContextPath)
		return m, nil
	case "tree_copy":
		m.treeRenameMode = false
		m.treeCopyMode = true
		m.treeMoveMode = false
		m.treeEditTarget = m.treeContextPath
		m.treeEditInput = filepath.Base(m.treeContextPath) + "-copy"
		return m, nil
	case "tree_move":
		m.treeRenameMode = false
		m.treeCopyMode = false
		m.treeMoveMode = true
		m.treeEditTarget = m.treeContextPath
		m.treeEditInput = "."
		return m, nil
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
	case "tree_stash":
		path := m.treeContextPath
		return m, func() tea.Msg {
			result, err := vault.StashFile(context.Background(), path)
			if err != nil {
				return stashErrMsg{err: err}
			}
			return stashSavedMsg{result: result}
		}
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

	m.showContextMenu(&m.gitContextMenu, items, x, y)
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
	m.cancelSessionRestore()
	normalizedPath, err := m.normalizeEditorFilePath(path)
	if err != nil {
		m.status = fmt.Sprintf("Cannot open file: %v", err)
		return m, nil
	}
	path = normalizedPath
	if m.saveAsDestinationReserved(path) {
		m.status = fmt.Sprintf(
			"Cannot open %s while Save As is using that destination",
			filepath.Base(path),
		)
		return m, nil
	}
	// LSP and watcher lifecycle is keyed by file path/URI. Two independent
	// buffers for one path would diverge and closing either could unsubscribe
	// the other. Until Teak has shared-buffer views, "new tab" pins and focuses
	// the existing owner instead of creating an unsafe duplicate.
	if idx := m.tabBar.FindTab(path); idx >= 0 {
		m.activateTab(idx)
		m.tabBar.PinTab(idx)
		m.setFocus(FocusEditor)
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

	m.editors = append(m.editors, ed)
	idx := m.tabBar.AddTab(filepath.Base(path), path)
	m.activateTab(idx)
	m.setFocus(FocusEditor)
	m.relayout()

	return m, m.startFileLoad(path, ed, true, nil)
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
		m.activateTab(idx)
		m.tabBar.PinTab(idx)
		m.setFocus(FocusEditor)
		return m, nil
	}

	// Create a placeholder editor (unused, but keeps editors slice in sync with tabs)
	buf := text.NewBuffer()
	ed := editor.New(buf, m.theme, editor.DefaultConfig())

	// Try to reuse a preview tab (same as file opening behavior)
	label := "\u0394 " + filepath.Base(relPath)
	replaceIdx := m.findReplaceableTab()
	if replaceIdx >= 0 {
		// Clean up any old diff view for this slot
		m.removeLoadsForEditor(m.editors[replaceIdx].ID())
		delete(m.diffViews, replaceIdx)
		m.setEditor(replaceIdx, ed)
		m.tabBar.Tabs[replaceIdx].Label = label
		m.tabBar.Tabs[replaceIdx].FilePath = diffKey
		m.tabBar.Tabs[replaceIdx].Dirty = false
		m.tabBar.Tabs[replaceIdx].Preview = true
		m.tabBar.Tabs[replaceIdx].Kind = editor.TabDiff
		m.tabBar.Tabs[replaceIdx].DiagSeverity = 0
		m.activateTab(replaceIdx)
	} else {
		m.editors = append(m.editors, ed)
		tabIdx := m.tabBar.AddTab(label, diffKey)
		m.tabBar.Tabs[tabIdx].Kind = editor.TabDiff
		m.tabBar.Tabs[tabIdx].Preview = true
		m.activateTab(tabIdx)
	}

	m.setFocus(FocusEditor)
	m.relayout()

	return m, m.startDiffLoad(relPath, status, ed)
}

func (m Model) handleDiffLoaded(msg DiffLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.RequestID != 0 {
		request, ok := m.pendingDiffLoads[msg.RequestID]
		if !ok || request.EditorID != msg.EditorID || request.Path != msg.Path {
			return m, nil
		}
		delete(m.pendingDiffLoads, msg.RequestID)
		request.Cancel()
	}
	tabIdx := m.editorIndexForDiffLoad(msg.EditorID, msg.Path)
	if msg.RequestID == 0 && msg.EditorID == 0 { // retained only for older direct unit messages
		tabIdx = msg.TabIndex
	}
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
	return git.CurrentBranch(dir)
}

// boundedReplaceAll scans at most maxReplaceAllMatches+1 non-overlapping
// occurrences before allocating the replacement string. Preparation runs in a
// background command, but the cap still bounds transient memory and latency.
func boundedReplaceAll(content, query, replacement string) (result string, matches int, ok bool) {
	result, _, matches, ok = boundedReplaceAllAtOffset(content, query, replacement, 0)
	return result, matches, ok
}

// boundedReplaceAllAtOffset also maps an original byte offset through the
// replacements. Mapping the offset before converting it back to a Position
// keeps the cursor valid when replacements add/remove lines or UTF-8 bytes.
func boundedReplaceAllAtOffset(content, query, replacement string, originalOffset int) (result string, mappedOffset, matches int, ok bool) {
	if query == "" {
		return content, min(max(0, originalOffset), len(content)), 0, true
	}
	originalOffset = min(max(0, originalOffset), len(content))
	mappedOffset = originalOffset
	resultBytes := len(content)
	delta := 0
	mapped := false
	for from := 0; from < len(content); {
		idx := strings.Index(content[from:], query)
		if idx < 0 {
			break
		}
		start := from + idx
		end := start + len(query)
		matches++
		if matches > maxReplaceAllMatches {
			return "", 0, matches, false
		}

		if growth := len(replacement) - len(query); growth > 0 {
			if resultBytes > maxReplaceResultBytes-growth {
				return "", 0, matches, false
			}
			resultBytes += growth
		} else {
			resultBytes += growth
		}

		if !mapped {
			switch {
			case originalOffset <= start:
				mappedOffset = originalOffset + delta
				mapped = true
			case originalOffset < end:
				withinMatch := originalOffset - start
				mappedOffset = start + delta + min(withinMatch, len(replacement))
				mapped = true
			default:
				delta += len(replacement) - len(query)
			}
		}
		from = end
	}
	if matches == 0 {
		return content, originalOffset, 0, true
	}
	if !mapped {
		mappedOffset = originalOffset + delta
	}
	return strings.ReplaceAll(content, query, replacement), mappedOffset, matches, true
}

// handleExternalFileChange reloads a file that was modified externally.
func (m Model) handleExternalFileChange(msg FileChangedMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	path := cleanExternalConflictPath(msg.Path)
	if msg.Observation != 0 {
		if msg.Observation <= m.externalChangeObserved[path] {
			if m.watcher != nil {
				cmds = append(cmds, m.watcher.listenCmd())
			}
			return m, tea.Batch(cmds...)
		}
		if !msg.OwnWriteVerified && msg.Observation <= m.lastSaveWatcherWatermarks[path] {
			msg.RequiresConflict = true
		}
	}
	if msg.OwnWriteCandidate && msg.Missing && msg.Snapshot == nil {
		// Atomic replacement on some platforms reports Remove/Rename before the
		// replacement Create. Re-read after the watcher debounce window so the
		// normal own-write byte comparison can distinguish that sequence from a
		// real external deletion.
		recheck := msg
		recheck.Missing = false
		recheck.NeedsRead = true
		cmds = append(cmds, tea.Tick(debounceWindow, func(time.Time) tea.Msg {
			return recheck
		}))
		if m.watcher != nil {
			cmds = append(cmds, m.watcher.listenCmd())
		}
		return m, tea.Batch(cmds...)
	}
	snapshot := msg.Snapshot
	if snapshot == nil {
		if msg.NeedsRead || msg.Missing {
			if cmd := m.enqueueExternalFileRead(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else {
			// Legacy/direct callers may still supply raw bytes. Convert them in
			// a command so Update never constructs a document-sized rope.
			cmds = append(cmds, prepareExternalFileChangedCmd(msg, m.externalBackgroundContext()))
		}
		if m.watcher != nil {
			cmds = append(cmds, m.watcher.listenCmd())
		}
		return m, tea.Batch(cmds...)
	}
	updated, applyCmd := m.applyPreparedExternalFileChange(msg)
	m = updated.(Model)
	if applyCmd != nil {
		cmds = append(cmds, applyCmd)
	}
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

// applyPreparedExternalFileChange mutates only from an immutable snapshot or a
// confirmed missing-path signal. Disk I/O and Rope construction happen before
// this point on the serialized external-read worker.
func (m Model) applyPreparedExternalFileChange(msg FileChangedMsg) (tea.Model, tea.Cmd) {
	path := cleanExternalConflictPath(msg.Path)
	if msg.Observation != 0 {
		// A serialized fallback read can finish after either a newer direct
		// watcher snapshot or a save whose watermark covers this observation.
		// Re-evaluate ordering at the final mutation boundary: enqueue-time
		// knowledge is insufficient while disk I/O is in flight.
		if msg.Observation <= m.externalChangeObserved[path] {
			return m, nil
		}
		if !msg.OwnWriteVerified && msg.Observation <= m.lastSaveWatcherWatermarks[path] {
			msg.RequiresConflict = true
		}
		m.externalChangeObserved[path] = msg.Observation
	}
	search.InvalidateSemanticIndex(m.rootDir)
	matched := false
	for _, ed := range m.editors {
		if cleanExternalConflictPath(ed.Buffer.FilePath) != path {
			continue
		}
		matched = true
		if msg.RequiresConflict {
			m.recordExternalConflict(path, msg.Snapshot, msg.Observation, msg.Missing, true)
			m = m.showExternalConflictConfirmation(path)
			m.status = fmt.Sprintf("External change overlapped the completed save: %s", filepath.Base(path))
			break
		}
		if msg.Missing {
			m.recordExternalConflict(path, nil, msg.Observation, true, msg.RequiresConflict)
			m = m.showExternalConflictConfirmation(path)
			m.status = fmt.Sprintf("File removed or renamed outside Teak: %s", filepath.Base(path))
			break
		}
		if existing, ok := m.externalConflicts[path]; ok {
			if existing.Missing && !existing.PostSave {
				// An atomic replace commonly produces Remove before the new
				// bytes become readable. A clean buffer can resolve only that
				// transient missing-path conflict automatically.
				m.clearExternalConflict(path)
			} else {
				m.recordExternalConflict(path, msg.Snapshot, msg.Observation, false, msg.RequiresConflict)
				m = m.showExternalConflictConfirmation(path)
				m.status = fmt.Sprintf("New external change remains unresolved: %s", filepath.Base(path))
				break
			}
		}
		if ed.Buffer.Dirty() {
			m.recordExternalConflict(path, msg.Snapshot, msg.Observation, false, msg.RequiresConflict)
			m = m.showExternalConflictConfirmation(path)
			m.status = fmt.Sprintf("External change conflicts with local edits: %s", filepath.Base(path))
			break
		}
	}
	if matched && !m.hasExternalConflict(path) {
		updated, reloadCmd := m.reloadExternalFile(path, msg.Snapshot)
		m = updated.(Model)
		if m.watcher != nil {
			// A direct watch is invalidated by atomic rename. Re-register after
			// the replacement is known to exist.
			m.watcher.WatchFile(path)
		}
		return m, reloadCmd
	}
	return m, nil
}

const treeRefreshDebounceWindow = 120 * time.Millisecond

// queueTreeRefresh coalesces filesystem changes before starting an expensive
// directory walk. It also cancels an in-flight walk immediately; the result
// carries a generation so an uncancellable OS directory read can never apply
// after a newer event.
func (m *Model) queueTreeRefresh() tea.Cmd {
	m.cancelTreeRefresh()
	m.treeRefreshGeneration++
	generation := m.treeRefreshGeneration
	return tea.Tick(treeRefreshDebounceWindow, func(time.Time) tea.Msg {
		return treeRefreshDebounceMsg{Generation: generation}
	})
}

func (m *Model) cancelTreeRefresh() {
	if m.treeRefreshCancel != nil {
		m.treeRefreshCancel()
		m.treeRefreshCancel = nil
	}
}

func (m *Model) startTreeRefresh(generation uint64) tea.Cmd {
	if m.rootDir == "" || m.tree.Root == "" {
		return nil
	}
	m.cancelTreeRefresh()
	ctx, cancel := context.WithCancel(context.Background())
	m.treeRefreshCancel = cancel
	// Take ownership of entry slices before the command starts. The snapshot
	// clone is memory-only; filesystem traversal and recursive reads happen in
	// the command below, never in Update.
	tree := m.tree.SnapshotForRefresh()
	return func() tea.Msg {
		refresh, err := tree.Refresh(ctx, tree.Root)
		return treeRefreshResultMsg{Generation: generation, Refresh: refresh, Err: err}
	}
}

// handleTreeChange refreshes the file tree when the directory structure changes.
func (m Model) handleTreeChange(msg TreeChangedMsg) (tea.Model, tea.Cmd) {
	// Invalidate cached file list for quick open
	m.cancelFileListScan()
	m.cachedFilesReady = false
	m.cachedFiles = nil
	m.fileListGeneration++
	search.InvalidateSemanticIndex(m.rootDir)
	// Reading a directory (and all expanded descendants) is deliberately
	// deferred. Watcher events can arrive in bursts while build tools generate
	// thousands of files, and Update must remain responsive to input.
	treeRefreshCmd := m.queueTreeRefresh()
	m.gitRefreshGeneration++
	var cmds []tea.Cmd
	cmds = append(cmds, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		generation := m.gitRefreshGeneration
		return gitRefreshDebounceMsg{generation: generation}
	}))
	cmds = append(cmds, treeRefreshCmd)
	// Continue listening
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.listenCmd())
	}
	return m, tea.Batch(cmds...)
}

// handleWatcherLimit surfaces watch-budget exhaustion. Past this point
// external-change detection silently degrades for unwatched paths, so the
// user needs a visible warning instead of a log line.
func (m Model) handleWatcherLimit() (tea.Model, tea.Cmd) {
	m.status = "File watcher limit reached: external changes in some folders may not refresh automatically"
	if m.watcher != nil {
		return m, m.watcher.listenCmd()
	}
	return m, nil
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
		return m, m.startTreeCreate(dir, name, isFolder)
	case "backspace":
		m.newItemInput = deleteLastRune(m.newItemInput)
		return m, nil
	default:
		if msg.Text != "" {
			m.newItemInput += msg.Text
		}
		return m, nil
	}
}

func (m Model) handleTreeFileOperationInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	reset := func(model *Model) {
		model.treeRenameMode = false
		model.treeCopyMode = false
		model.treeMoveMode = false
		model.treeEditInput = ""
		model.treeEditTarget = ""
	}
	switch msg.String() {
	case "esc", "escape":
		reset(&m)
		return m, nil
	case "enter":
		input := m.treeEditInput
		target := m.treeEditTarget
		rename, copyItem, move := m.treeRenameMode, m.treeCopyMode, m.treeMoveMode
		reset(&m)
		if input == "" || target == "" {
			return m, nil
		}
		if rename {
			return m, m.startTreeRename(target, input)
		}
		if copyItem {
			return m, m.startTreeCopy(target, input)
		}
		if move {
			return m, m.startTreeMove(target, input)
		}
		return m, nil
	case "backspace":
		m.treeEditInput = deleteLastRune(m.treeEditInput)
		return m, nil
	case "ctrl+u":
		m.treeEditInput = ""
		return m, nil
	default:
		if msg.Text != "" {
			m.treeEditInput += msg.Text
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
		return m, m.startTreeDelete(target)
	default:
		m.deleteConfirm = false
		m.deleteTarget = ""
		return m, nil
	}
}

func lipglossWidth(s string) int {
	return ansi.StringWidth(s)
}

// openQuickOpen pushes a Picker overlay for quick file opening.
func (m Model) openQuickOpen() (tea.Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
	picker := overlay.NewPicker("Open File", nil, m.theme, "quickopen")
	picker.SetSize(min(m.width-4, 60), m.height-4)

	var cmds []tea.Cmd
	cmds = append(cmds, picker.Focus())

	if m.cachedFilesReady {
		cmds = append(cmds, preparePickerItemsCmd(picker.InstanceID(), "quickopen", m.cachedFiles, false))
	} else {
		cmds = append(cmds, m.startFileListScan())
	}

	m.overlayStack.Push(picker)
	return m, tea.Batch(cmds...)
}

func (m *Model) startFileListScan() tea.Cmd {
	m.cancelFileListScan()
	m.fileListGeneration++
	ctx, cancel := context.WithCancel(context.Background())
	m.fileListCancel = cancel
	return quickOpenCmd(ctx, m.rootDir, m.fileListGeneration)
}

func (m *Model) cancelFileListScan() {
	if m.fileListCancel != nil {
		m.fileListCancel()
		m.fileListCancel = nil
	}
	// Invalidate a result even if its goroutine returns after cancellation.
	m.fileListGeneration++
}

func (m Model) handlePluginKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.pluginMgr == nil {
		m.pluginKeySequence = ""
		m.pluginKeyBuffer = nil
		return m, nil, false
	}

	mode, ok := m.pluginKeyMode()
	if !ok {
		m.pluginKeySequence = ""
		m.pluginKeyBuffer = nil
		return m, nil, false
	}

	key := normalizePluginKey(msg.String())
	if key == "" {
		m.pluginKeySequence = ""
		m.pluginKeyBuffer = nil
		return m, nil, false
	}

	sequence := appendPluginKeySequence(m.pluginKeySequence, key)
	exact, prefix := m.pluginMgr.MatchKey(mode, sequence)
	if exact {
		m.pluginKeySequence = ""
		m.pluginKeyBuffer = nil
		return m, pluginKeyDispatchCmd(m.pluginMgr, newPluginAsyncRuntime(m), mode, sequence), true
	}
	if prefix {
		m.pluginKeySequence = sequence
		m.pluginKeyBuffer = append(m.pluginKeyBuffer, msg)
		return m, nil, true
	}

	if m.pluginKeySequence != "" {
		// The pending sequence was abandoned. Reinject the buffered keys plus
		// the current one as ordinary input: a leader prefix consumed them
		// eagerly, and dropping them here would silently swallow typed
		// characters (the space that starts <leader>, most commonly).
		replay := append(m.pluginKeyBuffer, msg)
		m.pluginKeySequence = ""
		m.pluginKeyBuffer = nil
		var cmds []tea.Cmd
		for _, buffered := range replay {
			// Depth > 0 keeps the replayed keys out of plugin matching so a
			// replayed leader key cannot start the same dead sequence again.
			m.pluginFeedDepth++
			result, cmd := m.Update(buffered)
			m.pluginFeedDepth--
			m = result.(Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...), true
	}

	return m, nil, false
}

func (m Model) pluginKeyMode() (string, bool) {
	switch m.focus {
	case FocusEditor:
		return "n", true
	case FocusTree:
		return "tree", true
	case FocusGitPanel:
		return "git", true
	case FocusProblems:
		return "problems", true
	case FocusDebugger:
		return "debugger", true
	case FocusAgent:
		return "agent", true
	default:
		return "", false
	}
}

func normalizePluginKey(key string) string {
	switch key {
	case "":
		return ""
	case " ", "space":
		return "<leader>"
	default:
		return key
	}
}

func appendPluginKeySequence(current, key string) string {
	if current == "" {
		return key
	}
	if strings.HasPrefix(current, "<leader>") && len([]rune(key)) == 1 {
		return current + key
	}
	return key
}

func (m Model) findEditorByPath(path string) int {
	for i := range m.editors {
		if m.editors[i].Buffer.FilePath == path {
			return i
		}
	}
	return -1
}

func applyTextEditsToBuffer(buf *text.Buffer, edits []lsp.TextEdit) int {
	sortedEdits := make([]lsp.TextEdit, len(edits))
	copy(sortedEdits, edits)
	slices.SortFunc(sortedEdits, func(a, b lsp.TextEdit) int {
		if a.StartLine != b.StartLine {
			return b.StartLine - a.StartLine
		}
		return b.StartCol - a.StartCol
	})

	applied := 0
	for _, te := range sortedEdits {
		start := text.Position{Line: te.StartLine, Col: te.StartCol}
		end := text.Position{Line: te.EndLine, Col: te.EndCol}
		buf.ReplaceRange(start, end, []byte(te.NewText))
		applied++
	}
	if applied > 0 {
		// These edits can shorten the document arbitrarily (a formatter
		// collapsing blank lines, a code action deleting a block), leaving the
		// cursor past the new end of the buffer.
		buf.ClampCursor()
	}
	return applied
}

// openCommandPalette pushes a Picker overlay with available commands.
func (m Model) openCommandPalette() (tea.Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
	items := m.buildCommandList()
	picker := overlay.NewPicker("Command Palette", items, m.theme, "cmdpalette")
	picker.SetSize(min(m.width-4, 60), m.height-4)
	cmd := picker.Focus()
	m.overlayStack.Push(picker)
	return m, cmd
}

// jumpToProblem moves the problems-panel selection and opens the selected
// diagnostic's location. Shared by the F8/Shift+F8 bindings and the command
// palette entries.
func (m Model) jumpToProblem(prev bool) (tea.Model, tea.Cmd) {
	if m.problemsPanel.ProblemCount() == 0 {
		return m, nil
	}
	if prev {
		m.problemsPanel.SelectPrev()
	} else {
		m.problemsPanel.SelectNext()
	}
	prob := m.problemsPanel.SelectedProblem()
	if prob == nil {
		return m, nil
	}
	pos := text.Position{Line: prob.Line, Col: prob.Col}
	m.setPendingCursor(prob.FilePath, pos)
	model, cmd := m.openFile(prob.FilePath)
	updated := model.(Model)
	updated.status = fmt.Sprintf("Problem %d/%d", updated.problemsPanel.SelectedIndex()+1, updated.problemsPanel.ProblemCount())
	return updated, cmd
}

// handleCommandPaletteAction dispatches an action from the command palette.
func (m Model) handleCommandPaletteAction(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch innerMsg := msg.(type) {
	case saveRequestMsg:
		if m.activeEditor() == nil {
			return m, nil
		}
		buf := m.activeEditor().Buffer
		if buf.FilePath == "" {
			m.saveAsMode = true
			m.saveAsInput = filepath.Join(m.rootDir, "") + "/"
			return m, nil
		}
		return m, m.beginSaveForTab(m.activeTab, false, false)
	case toggleTreeMsg:
		m.showTree = !m.showTree
		if m.showTree {
			m.setFocus(FocusTree)
		} else {
			m.setFocus(FocusEditor)
		}
		m.relayout()
		return m, nil
	case toggleGitMsg:
		if m.gitPanel.IsGitRepo() {
			m.showTree = true
			m.sidebarTab = SidebarGit
			m.setFocus(FocusGitPanel)
			m.relayout()
		}
		return m, nil
	case toggleProblemsMsg:
		m.showTree = true
		m.sidebarTab = SidebarProblems
		m.setFocus(FocusProblems)
		m.relayout()
		return m, nil
	case openHealthDashboardMsg:
		return m.openHealthDashboard()
	case openSearchMsg:
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
		m.openSettingsOverlay()
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
		return m.handleDebugStart()
	case debugStopMsg:
		return m.handleDebugStop("Debugging stopped")
	case newFileMsg:
		return m.newUntitledTab()
	case saveAsMsg:
		if m.activeEditor() == nil {
			return m, nil
		}
		m.saveAsMode = true
		if m.activeEditor().Buffer.FilePath != "" {
			m.saveAsInput = m.activeEditor().Buffer.FilePath
		} else {
			m.saveAsInput = filepath.Join(m.rootDir, "") + "/"
		}
		return m, nil
	case FindNextMsg:
		return m.findNext()
	case FindPrevMsg:
		return m.findPrev()
	case quitMsg:
		return m.requestQuit()
	case formatFileMsg:
		ed := m.activeEditor()
		if ed == nil || ed.Buffer.FilePath == "" {
			return m, nil
		}
		return m, m.requestFormatting(ed.Buffer.FilePath, ed.Config, 0)
	case gotoDefinitionMsg:
		return m.requestDefinition()
	case renameSymbolMsg:
		m.renameMode = true
		m.renameInput = ""
		return m, nil
	case codeActionsMsg:
		return m.requestCodeActions()
	case hoverSymbolMsg:
		return m.requestHover()
	case documentSymbolsMsg:
		return m.requestDocumentSymbols()
	case toggleSplitMsg:
		m.toggleSplit()
		return m, nil
	case closeSplitMsg:
		m.unsplit()
		return m, nil
	case cycleSplitMsg:
		m.cycleSplitFocus()
		return m, nil
	case toggleBreakpointMsg:
		if ed := m.activeEditor(); ed != nil && ed.Buffer.FilePath != "" {
			return m, m.toggleBreakpoint(ed.Buffer.FilePath, ed.Buffer.Cursor.Line)
		}
		return m, nil
	case foldLineMsg:
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.Fold(ed.Buffer.Cursor.Line)
			m.setEditor(m.activeTab, *ed)
		}
		return m, nil
	case unfoldLineMsg:
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.Unfold(ed.Buffer.Cursor.Line)
			m.setEditor(m.activeTab, *ed)
		}
		return m, nil
	case foldAllMsg:
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.FoldAll()
			m.setEditor(m.activeTab, *ed)
			m.status = "All regions folded"
		}
		return m, nil
	case unfoldAllMsg:
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.UnfoldAll()
			m.setEditor(m.activeTab, *ed)
			m.status = "All regions unfolded"
		}
		return m, nil
	case nextTabMsg:
		if len(m.editors) > 1 {
			m.activateTab((m.activeTab + 1) % len(m.editors))
		}
		return m, nil
	case prevTabMsg:
		if len(m.editors) > 1 {
			m.activateTab((m.activeTab - 1 + len(m.editors)) % len(m.editors))
		}
		return m, nil
	case nextProblemMsg:
		return m.jumpToProblem(false)
	case prevProblemMsg:
		return m.jumpToProblem(true)
	case restartLspMsg:
		ed := m.activeEditor()
		if ed == nil || ed.Buffer.FilePath == "" || m.lspMgr == nil {
			return m, nil
		}
		// A manual restart resets the crash budget: the user explicitly asked
		// for another attempt.
		if cfg := m.lspMgr.ConfigForFile(ed.Buffer.FilePath); cfg != nil {
			delete(m.lspRestarts, cfg.Command)
		}
		m.status = "Restarting language server..."
		return m, m.lspDidOpen(ed.Buffer)
	}
	return m, nil
}
