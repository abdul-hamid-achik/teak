package git

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/mattn/go-runewidth"
	"teak/internal/ui"
)

// RefreshMsg is sent when git status data has been fetched.
type RefreshMsg struct {
	Branch  string
	Entries []StatusEntry
	Err     error
	// Generation is set by app-level debounced refreshes. Zero means the
	// refresh was requested by a direct panel action and is not subject to the
	// app's latest-wins guard.
	Generation int
}

type treeRowKind int

const (
	treeRowNone treeRowKind = iota
	treeRowStagedHeader
	treeRowNode
	treeRowUnstagedHeader
)

type treeRowHit struct {
	kind    treeRowKind
	section GitSection
	index   int
	node    *GitTreeNode
}

// treeRowCache is a per-Model immutable snapshot of the visible Git trees.
// Model uses value semantics in Update, so invalidation always replaces this
// pointer instead of mutating the cache shared by an older model value.
type treeRowCache struct {
	stagedFlat    []*GitTreeNode
	unstagedFlat  []*GitTreeNode
	rows          []treeRowHit
	stagedStart   int
	unstagedStart int
}

var emptyTreeRowCache = treeRowCache{stagedStart: -1, unstagedStart: -1}

type panelFooterMode int

const (
	panelFooterNone panelFooterMode = iota
	panelFooterCompact
	panelFooterFull
)

type panelLayout struct {
	treeHeight int
	footerMode panelFooterMode
	bodyHeight int
}

// buildTree creates a tree structure from a flat list of status entries.
func buildTree(entries []StatusEntry, staged bool) []*GitTreeNode {
	root := &GitTreeNode{IsDir: true, Expanded: true}
	directories := map[*GitTreeNode]map[string]*GitTreeNode{
		root: make(map[string]*GitTreeNode),
	}
	findOrCreateDir := func(parent *GitTreeNode, name, path string, depth int, entry *StatusEntry) *GitTreeNode {
		children := directories[parent]
		if node := children[name]; node != nil {
			return node
		}
		node := &GitTreeNode{
			Name: name, Path: path, IsDir: true, Depth: depth,
			Expanded: true, Entry: entry, Staged: staged,
		}
		parent.Children = append(parent.Children, node)
		children[name] = node
		directories[node] = make(map[string]*GitTreeNode)
		return node
	}

	for i := range entries {
		e := &entries[i]
		path := strings.TrimRight(e.Path, "/")
		if path == "" {
			continue
		}

		// If this entry is a directory (from git status with trailing /),
		// render it as a directory node rather than a file leaf.
		if e.IsDir {
			parts := strings.Split(path, "/")
			node := root
			for partIndex, part := range parts {
				depth := partIndex
				node = findOrCreateDir(node, part, strings.Join(parts[:partIndex+1], "/"), depth, e)
			}
			continue
		}

		parts := strings.Split(path, "/")
		node := root
		for j, part := range parts {
			if j == len(parts)-1 {
				// Leaf file
				node.Children = append(node.Children, &GitTreeNode{
					Name:   part,
					Path:   e.Path,
					IsDir:  false,
					Depth:  j,
					Entry:  e,
					Staged: staged,
				})
			} else {
				node = findOrCreateDir(node, part, strings.Join(parts[:j+1], "/"), j, nil)
			}
		}
	}

	return root.Children
}

func rebuildTree(entries []StatusEntry, staged bool, previous []*GitTreeNode) []*GitTreeNode {
	next := buildTree(entries, staged)
	if len(previous) == 0 || len(next) == 0 {
		return next
	}

	expanded := make(map[string]bool)
	collectExpandedDirs(previous, expanded)
	restoreExpandedDirs(next, expanded)
	return next
}

func collectExpandedDirs(nodes []*GitTreeNode, expanded map[string]bool) {
	for _, node := range nodes {
		if !node.IsDir {
			continue
		}
		expanded[node.Path] = node.Expanded
		collectExpandedDirs(node.Children, expanded)
	}
}

func restoreExpandedDirs(nodes []*GitTreeNode, expanded map[string]bool) {
	for _, node := range nodes {
		if !node.IsDir {
			continue
		}
		if state, ok := expanded[node.Path]; ok {
			node.Expanded = state
		}
		restoreExpandedDirs(node.Children, expanded)
	}
}

// flattenTree flattens a tree into a list of nodes for rendering.
func flattenTree(nodes []*GitTreeNode) []*GitTreeNode {
	flat := make([]*GitTreeNode, 0, len(nodes))
	stack := make([]*GitTreeNode, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0; i-- {
		stack = append(stack, nodes[i])
	}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		flat = append(flat, node)
		if !node.IsDir || !node.Expanded {
			continue
		}
		// Push children in reverse order so the pop order remains the same
		// depth-first ordering the panel has always rendered.
		for i := len(node.Children) - 1; i >= 0; i-- {
			stack = append(stack, node.Children[i])
		}
	}
	return flat
}

// OpenDiffMsg is sent when the user wants to view a diff for a file.
type OpenDiffMsg struct {
	Path   string
	Status string
}

// Model is the git sidebar panel model.
type Model struct {
	Branch    string
	Entries   []StatusEntry
	Staged    []StatusEntry
	Unstaged  []StatusEntry
	Cursor    int
	ScrollY   int
	Width     int
	Height    int
	Collapsed bool
	theme     ui.Theme
	rootDir   string
	isGitRepo bool

	// Sections
	activeSection     GitSection
	stagedCollapsed   bool
	unstagedCollapsed bool

	// Tree views of changed files
	stagedTree   []*GitTreeNode
	unstagedTree []*GitTreeNode
	treeRowCache *treeRowCache

	// Commit form
	commitTitle  textinput.Model // single-line title (required)
	commitBody   textarea.Model  // multi-line body (optional) - using bubbles textarea
	titleFocused bool
	bodyFocused  bool

	// Spinner for async operations
	spinner    spinner.Model
	spinning   bool   // true when an async operation is in progress
	spinStatus string // label shown next to spinner (e.g. "Pushing...")

	// Double-click detection for file nodes
	lastClickTime  time.Time
	lastClickIndex int

	refreshRequestGeneration uint64
	statusGeneration         uint64
	sectionGeneration        uint64
	expansion                *expansionState
}

// New creates a new git panel model.
func New(rootDir string, theme ui.Theme) Model {
	ti := textinput.New()
	ti.Placeholder = "Commit message"
	ti.CharLimit = 72
	ti.Prompt = ""

	// Initialize textarea for commit body
	ta := textarea.New()
	configureCommitBody(&ta)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.Nord8)

	m := Model{
		theme:       theme,
		rootDir:     rootDir,
		commitTitle: ti,
		commitBody:  ta,
		spinner:     sp,
		expansion:   newExpansionState(),
	}
	m.rebuildTreeRowCache()
	return m
}

func configureCommitBody(ta *textarea.Model) {
	ta.Placeholder = "Description (optional)"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.EndOfBufferCharacter = ' '
	ta.SetHeight(5)
	ta.SetWidth(50)
	ta.CharLimit = 10000

	styles := ta.Styles()
	styles.Focused.Text = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord6)
	styles.Focused.Placeholder = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	styles.Focused.CursorLine = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord6)
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord1)
	styles.Focused.Prompt = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord1)
	styles.Blurred.Text = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	styles.Blurred.Placeholder = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	styles.Blurred.CursorLine = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord1)
	styles.Blurred.Prompt = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord1)
	ta.SetStyles(styles)
}

// IsGitRepo returns whether the root dir is inside a git repository.
func (m Model) IsGitRepo() bool {
	return m.isGitRepo
}

// SetIsGitRepo sets whether the root dir is inside a git repository.
func (m *Model) SetIsGitRepo(isGitRepo bool) {
	m.isGitRepo = isGitRepo
}

// RootDir returns the root directory of the git repo.
func (m Model) RootDir() string {
	return m.rootDir
}

// Refresh returns a command that fetches git branch and status asynchronously.
func (m Model) Refresh() tea.Cmd {
	if !m.isGitRepo {
		return nil
	}
	rootDir := m.rootDir
	return func() tea.Msg {
		return refreshAfter(rootDir)
	}
}

// deriveGroups splits entries into staged and unstaged groups and builds trees.
func (m *Model) deriveGroups() {
	m.Staged = nil
	m.Unstaged = nil
	for _, e := range m.Entries {
		if e.IsStagedChange() {
			m.Staged = append(m.Staged, e)
		}
		if e.IsUnstagedChange() {
			m.Unstaged = append(m.Unstaged, e)
		}
	}
	m.stagedTree = rebuildTree(m.Staged, true, m.stagedTree)
	m.unstagedTree = rebuildTree(m.Unstaged, false, m.unstagedTree)
	m.rebuildTreeRowCache()
}

// activeFlatTree returns the flattened tree for the active section.
func (m Model) activeFlatTree() []*GitTreeNode {
	cache := m.treeRowsSnapshot()
	switch m.activeSection {
	case SectionStaged:
		return cache.stagedFlat
	case SectionUnstaged:
		return cache.unstagedFlat
	default:
		return nil
	}
}

// treeRowsSnapshot returns only prepared tree data. Constructors and
// projection commands populate the cache; input handling must never flatten a
// repository-sized tree as a fallback inside Update.
func (m Model) treeRowsSnapshot() *treeRowCache {
	if m.treeRowCache != nil {
		return m.treeRowCache
	}
	return &emptyTreeRowCache
}

func (m *Model) rebuildTreeRowCache() {
	m.treeRowCache = buildTreeRowCache(m.stagedTree, m.unstagedTree, m.stagedCollapsed, m.unstagedCollapsed)
}

func buildTreeRowCache(stagedTree, unstagedTree []*GitTreeNode, stagedCollapsed, unstagedCollapsed bool) *treeRowCache {
	cache := &treeRowCache{
		stagedFlat:    flattenTree(stagedTree),
		unstagedFlat:  flattenTree(unstagedTree),
		stagedStart:   -1,
		unstagedStart: -1,
	}
	rows := make([]treeRowHit, 0, 2+len(cache.stagedFlat)+len(cache.unstagedFlat))
	rows = append(rows, treeRowHit{kind: treeRowStagedHeader})
	if !stagedCollapsed {
		cache.stagedStart = len(rows)
		for i, node := range cache.stagedFlat {
			rows = append(rows, treeRowHit{kind: treeRowNode, section: SectionStaged, index: i, node: node})
		}
	}
	rows = append(rows, treeRowHit{kind: treeRowUnstagedHeader})
	if !unstagedCollapsed {
		cache.unstagedStart = len(rows)
		for i, node := range cache.unstagedFlat {
			rows = append(rows, treeRowHit{kind: treeRowNode, section: SectionUnstaged, index: i, node: node})
		}
	}
	cache.rows = rows
	return cache
}

// Update handles messages for the git panel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case RefreshMsg:
		if msg.Err != nil {
			return m, nil
		}
		if m.expansion == nil {
			m.expansion = newExpansionState()
		}
		m.refreshRequestGeneration++
		return m, prepareRefreshCmd(msg, m.refreshRequestGeneration, m.expansion, m.stagedCollapsed, m.unstagedCollapsed, m.sectionGeneration)

	case PreparedRefreshMsg:
		_, cmd := m.ApplyPreparedRefresh(msg)
		return m, cmd

	case PreparedTreeProjectionMsg:
		_, cmd := m.ApplyPreparedTreeProjection(msg)
		return m, cmd

	case spinner.TickMsg:
		if m.spinning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		// When wheel is over commit form, route to title or body scroll
		if m.titleFocused || m.CommitFormHitTest(mouse.Y) == "title" {
			// Horizontal scroll moves the title cursor left/right
			switch mouse.Button {
			case tea.MouseWheelLeft, tea.MouseWheelUp:
				pos := m.commitTitle.Position()
				pos -= 3
				if pos < 0 {
					pos = 0
				}
				m.commitTitle.SetCursor(pos)
			case tea.MouseWheelRight, tea.MouseWheelDown:
				pos := m.commitTitle.Position()
				pos += 3
				titleLen := len(m.commitTitle.Value())
				if pos > titleLen {
					pos = titleLen
				}
				m.commitTitle.SetCursor(pos)
			}
			return m, nil
		}
		if m.bodyFocused || m.CommitFormHitTest(mouse.Y) == "body" {
			var cmd tea.Cmd
			m.commitBody, cmd = m.commitBody.Update(msg)
			return m, cmd
		}
		if !m.Collapsed {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollTree(-3)
			case tea.MouseWheelDown:
				m.scrollTree(3)
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		return m.handleClick(mouse.Y)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// ApplyPreparedRefresh installs a background projection in constant time. A
// directory toggle that raced preparation causes the same immutable entries to
// be projected again with the newer expansion snapshot.
func (m *Model) ApplyPreparedRefresh(msg PreparedRefreshMsg) (bool, tea.Cmd) {
	if msg.projection == nil || msg.RequestGeneration != m.refreshRequestGeneration {
		return false, nil
	}
	if m.expansion == nil {
		m.expansion = newExpansionState()
	}
	if msg.ExpansionGeneration != m.expansion.currentGeneration() {
		retry := RefreshMsg{Branch: msg.Branch, Entries: msg.projection.entries, Generation: msg.Generation}
		return false, prepareRefreshCmd(retry, msg.RequestGeneration, m.expansion, m.stagedCollapsed, m.unstagedCollapsed, m.sectionGeneration)
	}
	if msg.SectionGeneration != m.sectionGeneration {
		retry := RefreshMsg{Branch: msg.Branch, Entries: msg.projection.entries, Generation: msg.Generation}
		return false, prepareRefreshCmd(retry, msg.RequestGeneration, m.expansion, m.stagedCollapsed, m.unstagedCollapsed, m.sectionGeneration)
	}

	projection := msg.projection
	m.Branch = msg.Branch
	m.Entries = projection.entries
	m.Staged = projection.staged
	m.Unstaged = projection.unstaged
	m.stagedTree = projection.stagedTree
	m.unstagedTree = projection.unstagedTree
	m.treeRowCache = projection.rowCache
	m.statusGeneration++

	flat := m.activeFlatTree()
	if len(flat) == 0 {
		switch m.activeSection {
		case SectionStaged:
			if len(m.Unstaged) > 0 {
				m.activeSection = SectionUnstaged
				m.Cursor = 0
			}
		case SectionUnstaged:
			if len(m.Staged) > 0 {
				m.activeSection = SectionStaged
				m.Cursor = 0
			}
		}
		flat = m.activeFlatTree()
	}
	if m.Cursor >= len(flat) {
		m.Cursor = max(0, len(flat)-1)
	}
	m.ensureCursorVisible()
	return true, nil
}

// ApplyPreparedTreeProjection installs visibility changes in constant time.
// A status refresh supersedes projections based on its predecessor; expansion
// or section changes that raced preparation are projected again from the
// current immutable trees.
func (m *Model) ApplyPreparedTreeProjection(msg PreparedTreeProjectionMsg) (bool, tea.Cmd) {
	if msg.projection == nil || msg.StatusGeneration != m.statusGeneration {
		return false, nil
	}
	if m.expansion == nil {
		m.expansion = newExpansionState()
	}
	if msg.ExpansionGeneration != m.expansion.currentGeneration() || msg.SectionGeneration != m.sectionGeneration {
		return false, m.prepareTreeProjection()
	}
	m.stagedTree = msg.projection.stagedTree
	m.unstagedTree = msg.projection.unstagedTree
	m.treeRowCache = msg.projection.rowCache
	flat := m.activeFlatTree()
	if m.Cursor >= len(flat) {
		m.Cursor = max(0, len(flat)-1)
	}
	m.ensureCursorVisible()
	return true, nil
}

func (m Model) prepareTreeProjection() tea.Cmd {
	return prepareTreeProjectionCmd(
		m.stagedTree,
		m.unstagedTree,
		m.expansion,
		m.stagedCollapsed,
		m.unstagedCollapsed,
		m.statusGeneration,
		m.sectionGeneration,
	)
}

func (m Model) handleClick(y int) (Model, tea.Cmd) {
	// Zone-based clicks (buttons, stage-all, unstage-all) are handled by app.go
	// which has access to the original absolute-coordinate message.
	// This method only handles positional Y-based clicks.
	hit := m.rowHitAtY(y)
	switch hit.kind {
	case treeRowStagedHeader:
		m.stagedCollapsed = !m.stagedCollapsed
		m.sectionGeneration++
		return m, m.prepareTreeProjection()
	case treeRowUnstagedHeader:
		m.unstagedCollapsed = !m.unstagedCollapsed
		m.sectionGeneration++
		return m, m.prepareTreeProjection()
	case treeRowNode:
		m.activeSection = hit.section
		m.Cursor = hit.index
		m.unfocusCommit()
		if hit.node == nil {
			return m, nil
		}
		if hit.node.IsDir {
			m.toggleDirectoryExpansion(hit.section, hit.node)
			return m, m.prepareTreeProjection()
		}
		if hit.node.Entry == nil {
			return m, nil
		}
		// Double-click to open diff (single click just selects)
		now := time.Now()
		isDouble := hit.index == m.lastClickIndex && now.Sub(m.lastClickTime) < 400*time.Millisecond
		m.lastClickTime = now
		m.lastClickIndex = hit.index
		if !isDouble {
			return m, nil
		}
		e := hit.node.Entry
		staged := hit.section == SectionStaged
		return m, func() tea.Msg {
			return OpenDiffMsg{Path: e.Path, Status: e.DisplayStatus(staged)}
		}
	}

	return m, nil
}

// UnfocusCommit releases the commit form's inputs. The app calls this when
// focus leaves the git panel by any route: a mouse click on another sidebar tab
// bypasses this model's Update entirely, and a commit box left focused silently
// swallows navigation keys afterwards.
func (m *Model) UnfocusCommit() {
	m.unfocusCommit()
}

func (m *Model) unfocusCommit() {
	m.titleFocused = false
	m.bodyFocused = false
	m.commitTitle.Blur()
	m.commitBody.Blur()
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// Title input captures keys when focused
	if m.titleFocused {
		switch msg.String() {
		case "esc", "escape":
			m.titleFocused = false
			m.commitTitle.Blur()
			m.activeSection = SectionUnstaged
			return m, nil
		case "tab":
			// Move to body
			return m, m.FocusBody()
		case "enter":
			// Move focus to body (like Tab) — commit only via button click
			return m, m.FocusBody()
		}
		var cmd tea.Cmd
		m.commitTitle, cmd = m.commitTitle.Update(msg)
		return m, cmd
	}

	// Body editing captures keys when focused - delegate to textarea
	if m.bodyFocused {
		switch msg.String() {
		case "esc", "escape":
			m.bodyFocused = false
			m.commitBody.Blur()
			m.activeSection = SectionUnstaged
			return m, nil
		case "tab":
			m.bodyFocused = false
			m.commitBody.Blur()
			m.activeSection = SectionUnstaged
			m.Cursor = 0
			return m, nil
		default:
			// Delegate all key handling to textarea
			var cmd tea.Cmd
			m.commitBody, cmd = m.commitBody.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "up":
		if m.Cursor > 0 {
			m.Cursor--
		} else if m.activeSection == SectionUnstaged && len(m.stagedTree) > 0 {
			m.activeSection = SectionStaged
			stagedFlat := m.treeRowsSnapshot().stagedFlat
			m.Cursor = len(stagedFlat) - 1
		}
		m.ensureCursorVisible()
		return m, nil
	case "down":
		flat := m.activeFlatTree()
		if m.Cursor < len(flat)-1 {
			m.Cursor++
		} else if m.activeSection == SectionStaged && len(m.unstagedTree) > 0 {
			m.activeSection = SectionUnstaged
			m.Cursor = 0
		}
		m.ensureCursorVisible()
		return m, nil
	case "enter":
		flat := m.activeFlatTree()
		if len(flat) > 0 && m.Cursor < len(flat) {
			node := flat[m.Cursor]
			if node.IsDir {
				m.toggleDirectoryExpansion(m.activeSection, node)
				return m, m.prepareTreeProjection()
			}
			if node.Entry != nil {
				e := node.Entry
				staged := m.activeSection == SectionStaged
				return m, func() tea.Msg {
					return OpenDiffMsg{Path: e.Path, Status: e.DisplayStatus(staged)}
				}
			}
		}
		return m, nil
	case "s":
		flat := m.activeFlatTree()
		if m.activeSection == SectionUnstaged && m.Cursor < len(flat) {
			node := flat[m.Cursor]
			if node.Entry != nil {
				return m, StageCmd(m.rootDir, node.Entry.Path)
			}
		}
		return m, nil
	case "S":
		if len(m.Unstaged) > 0 {
			return m, StageAllCmd(m.rootDir)
		}
		return m, nil
	case "u":
		flat := m.activeFlatTree()
		if m.activeSection == SectionStaged && m.Cursor < len(flat) {
			node := flat[m.Cursor]
			if node.Entry != nil {
				return m, UnstageCmd(m.rootDir, node.Entry.Path)
			}
		}
		return m, nil
	case "U":
		if len(m.Staged) > 0 {
			return m, UnstageAllCmd(m.rootDir)
		}
		return m, nil
	case "tab":
		switch m.activeSection {
		case SectionUnstaged:
			if len(m.Staged) > 0 {
				m.activeSection = SectionStaged
				m.Cursor = 0
			} else {
				return m, m.FocusTitle()
			}
		case SectionStaged:
			return m, m.FocusTitle()
		case SectionCommitTitle, SectionCommitBody:
			m.unfocusCommit()
			m.bodyFocused = false
			m.activeSection = SectionUnstaged
			m.Cursor = 0
		}
		return m, nil
	case "c":
		// Quick focus commit title
		return m, m.FocusTitle()
	}
	return m, nil
}

func (m *Model) recordDirectoryExpansion(section GitSection, node *GitTreeNode) {
	if node == nil || !node.IsDir {
		return
	}
	if m.expansion == nil {
		m.expansion = newExpansionState()
	}
	m.expansion.record(section, node.Path, node.Expanded)
}

func (m *Model) toggleDirectoryExpansion(section GitSection, node *GitTreeNode) {
	if node == nil || !node.IsDir {
		return
	}
	if m.expansion == nil {
		m.expansion = newExpansionState()
	}
	m.expansion.toggle(section, node.Path, node.Expanded)
}

// bodyViewHeight returns the visible height for the textarea component.
func (m Model) bodyViewHeight() int {
	h := 3 // default visible lines for body
	if m.Height > 20 {
		h = 5
	}
	return h
}

// DoCommit commits with the current title + body.
func (m Model) DoCommit() (Model, tea.Cmd) {
	title := strings.TrimSpace(m.commitTitle.Value())
	if title == "" {
		return m, nil
	}
	// Refuse to commit when nothing is staged.
	if len(m.Staged) == 0 {
		return m, nil
	}
	// Build commit message: title + optional body
	body := strings.TrimSpace(m.commitBody.Value())
	msg := title
	if body != "" {
		msg = title + "\n\n" + body
	}
	m.commitTitle.SetValue("")
	m.commitBody.SetValue("")
	m.titleFocused = false
	m.bodyFocused = false
	m.commitTitle.Blur()
	m.commitBody.Blur()
	spinCmd := m.StartSpinner("Committing...")
	return m, tea.Batch(CommitCmd(m.rootDir, msg), spinCmd)
}

// IsSpinning returns whether the spinner is active.
func (m Model) IsSpinning() bool {
	return m.spinning
}

// StartSpinner starts the spinner with a status label and returns the tick command.
func (m *Model) StartSpinner(label string) tea.Cmd {
	m.spinning = true
	m.spinStatus = label
	return m.spinner.Tick
}

// StopSpinner stops the spinner.
func (m *Model) StopSpinner() {
	m.spinning = false
	m.spinStatus = ""
}

// IsTitleFocused returns whether the commit title input is focused.
func (m Model) IsTitleFocused() bool {
	return m.titleFocused
}

// IsBodyFocused returns whether the commit body is focused.
func (m Model) IsBodyFocused() bool {
	return m.bodyFocused
}

// FocusTitle focuses the commit title input.
func (m *Model) FocusTitle() tea.Cmd {
	m.activeSection = SectionCommitTitle
	m.titleFocused = true
	m.bodyFocused = false
	m.commitBody.Blur()
	return m.commitTitle.Focus()
}

// FocusBody focuses the commit body area.
func (m *Model) FocusBody() tea.Cmd {
	m.activeSection = SectionCommitBody
	m.bodyFocused = true
	m.titleFocused = false
	m.commitTitle.Blur()
	return m.commitBody.Focus()
}

// commitFormStartY returns the Y offset within the panel where the commit form
// top border renders, or -1 if the form is not visible.
func (m Model) commitFormStartY() int {
	layout := m.layout()
	if layout.footerMode != panelFooterFull {
		return -1
	}
	return layout.treeHeight + 1
}

// FocusBodyAt focuses the body at the clicked location.
func (m *Model) FocusBodyAt(panelY, panelX int) tea.Cmd {
	cmd := m.FocusBody()
	m.moveCommitBodyCursor(panelY, panelX)
	return cmd
}

// FocusTitleAt focuses the title and positions the cursor near the click X.
func (m *Model) FocusTitleAt(panelX int) tea.Cmd {
	m.activeSection = SectionCommitTitle
	m.titleFocused = true
	m.bodyFocused = false
	m.commitBody.Blur()

	// The textinput may have an internal scroll offset that shifts the visible
	// portion of the value.  Since the offset is not publicly accessible, we
	// first reset the cursor to the start (which zeroes the offset), then set
	// the cursor to the visual click position so the mapping is correct.
	m.commitTitle.CursorStart() // resets internal offset to 0

	// panelX is the absolute screen X; subtract 1 for the left border char.
	pos := panelX - 1
	if pos < 0 {
		pos = 0
	}
	titleLen := len(m.commitTitle.Value())
	if pos > titleLen {
		pos = titleLen
	}
	m.commitTitle.SetCursor(pos)
	return m.commitTitle.Focus()
}

type commitBodySegment struct {
	line     int
	startCol int
	endCol   int
	text     []rune
}

func (m Model) commitBodySegments(innerWidth int) []commitBodySegment {
	if innerWidth < 1 {
		innerWidth = 1
	}

	lines := strings.Split(m.commitBody.Value(), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	segments := make([]commitBodySegment, 0, len(lines))
	for lineIdx, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			segments = append(segments, commitBodySegment{line: lineIdx})
			continue
		}

		start := 0
		width := 0
		for i, r := range runes {
			rw := runewidth.RuneWidth(r)
			if rw < 1 {
				rw = 1
			}
			if width > 0 && width+rw > innerWidth {
				segments = append(segments, commitBodySegment{
					line:     lineIdx,
					startCol: start,
					endCol:   i,
					text:     append([]rune(nil), runes[start:i]...),
				})
				start = i
				width = 0
			}
			width += rw
		}

		segments = append(segments, commitBodySegment{
			line:     lineIdx,
			startCol: start,
			endCol:   len(runes),
			text:     append([]rune(nil), runes[start:]...),
		})
	}

	if len(segments) == 0 {
		return []commitBodySegment{{line: 0}}
	}
	return segments
}

func (m Model) commitBodyDisplayIndex(segments []commitBodySegment) int {
	currentLine := m.commitBody.Line()
	currentRowOffset := m.commitBody.LineInfo().RowOffset
	displayIndex := 0
	for _, segment := range segments {
		if segment.line >= currentLine {
			break
		}
		displayIndex++
	}
	return displayIndex + currentRowOffset
}

func (m *Model) moveCommitBodyCursor(panelY, panelX int) {
	innerWidth := max(1, m.Width-2)
	segments := m.commitBodySegments(innerWidth)
	if len(segments) == 0 {
		return
	}

	bodyStartY := m.commitFormStartY() + 2
	clickRow := panelY - bodyStartY
	if clickRow < 0 {
		clickRow = 0
	}

	targetDisplayIndex := m.commitBody.ScrollYOffset() + clickRow
	if targetDisplayIndex < 0 {
		targetDisplayIndex = 0
	}
	if targetDisplayIndex >= len(segments) {
		targetDisplayIndex = len(segments) - 1
	}

	currentDisplayIndex := m.commitBodyDisplayIndex(segments)
	for currentDisplayIndex < targetDisplayIndex {
		m.commitBody.CursorDown()
		currentDisplayIndex++
	}
	for currentDisplayIndex > targetDisplayIndex {
		m.commitBody.CursorUp()
		currentDisplayIndex--
	}

	segment := segments[targetDisplayIndex]
	clickX := panelX - 1
	if clickX < 0 {
		clickX = 0
	}

	targetCol := segment.startCol
	consumedWidth := 0
	for _, r := range segment.text {
		rw := runewidth.RuneWidth(r)
		if rw < 1 {
			rw = 1
		}
		if consumedWidth+rw > clickX {
			break
		}
		consumedWidth += rw
		targetCol++
	}
	if targetCol > segment.endCol {
		targetCol = segment.endCol
	}

	m.commitBody.SetCursorColumn(targetCol)
}

// IsInCommitFormArea returns true if the given panel-relative Y is in the commit form region.
func (m Model) IsInCommitFormArea(panelY int) bool {
	formY := m.commitFormStartY()
	return formY >= 0 && panelY >= formY
}

// CommitFormHitTest checks if panelY falls on the title or body of the commit form.
// Returns "title", "body", or "" if it doesn't match either.
func (m Model) CommitFormHitTest(panelY int) string {
	formY := m.commitFormStartY()
	if formY < 0 {
		return ""
	}
	titleY := formY + 1 // top border + title
	if panelY == titleY {
		return "title"
	}
	bodyStartY := formY + 2
	bodyHeight := m.layout().bodyHeight
	if panelY >= bodyStartY && panelY < bodyStartY+bodyHeight {
		return "body"
	}
	return ""
}

func (m Model) rowHitAtY(y int) treeRowHit {
	layout := m.layout()
	if y < 0 || y >= layout.treeHeight {
		return treeRowHit{}
	}

	rows := m.treeRows()
	scrollY := m.normalizedScroll(layout.treeHeight, len(rows))
	idx := scrollY + y
	if idx < 0 || idx >= len(rows) {
		return treeRowHit{}
	}
	return rows[idx]
}

// EntryAtY returns the status entry at the given panel Y coordinate and whether it's staged.
// Returns nil if Y doesn't correspond to a file entry.
func (m Model) EntryAtY(y int) (*StatusEntry, bool) {
	hit := m.rowHitAtY(y)
	if hit.kind != treeRowNode || hit.node == nil || hit.node.Entry == nil {
		return nil, false
	}
	return hit.node.Entry, hit.section == SectionStaged
}

// NodeAtY returns the tree node at the given panel Y coordinate and whether it's in the staged section.
// Returns nil if Y doesn't correspond to any node.
func (m Model) NodeAtY(y int) (*GitTreeNode, bool) {
	hit := m.rowHitAtY(y)
	if hit.kind != treeRowNode {
		return nil, false
	}
	return hit.node, hit.section == SectionStaged
}

// FilesUnderDir returns all file paths under a directory path in the given entry list.
func FilesUnderDir(entries []StatusEntry, dirPath string) []string {
	prefix := dirPath + "/"
	var paths []string
	for _, e := range entries {
		if strings.HasPrefix(e.Path, prefix) {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// ToggleCollapsed toggles the collapsed state.
func (m *Model) ToggleCollapsed() {
	m.Collapsed = !m.Collapsed
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(w, h int) {
	if m.Width == w && m.Height == h {
		return
	}
	m.Width = w
	m.Height = h
	// Keep the commit title input width in sync so its internal
	// cursor/scroll logic works correctly during Update().
	innerWidth := w - 2 // minus left+right border chars
	if innerWidth < 1 {
		innerWidth = 1
	}
	m.commitTitle.SetWidth(innerWidth)
	// Tree rows are independent from dimensions. Only the scroll bounds need
	// recomputing after an actual resize.
	m.ensureCursorVisible()
}

// View renders the git panel.
func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 {
		return ""
	}

	// Fallback UI when not in a git repo
	if !m.isGitRepo {
		var sb strings.Builder
		sb.WriteString("\n")
		sb.WriteString(m.theme.GitSectionHeader.Render("  Not a Git Repository"))
		sb.WriteString("\n\n")
		sb.WriteString("  This directory is not a Git repository.\n")
		sb.WriteString("\n")
		initBtn := zone.Mark("git-init-btn",
			m.theme.GitActionButton.Render(gitActionLabel("\uf417", "Initialize Git Repository")))
		sb.WriteString("  " + initBtn)
		sb.WriteString("\n")
		// Pad remaining height
		for i := 5; i < m.Height; i++ {
			sb.WriteByte('\n')
		}
		return sb.String()
	}

	layout := m.layout()
	rows := m.treeRows()
	scrollY := m.normalizedScroll(layout.treeHeight, len(rows))
	lines := make([]string, 0, m.Height)

	end := min(scrollY+layout.treeHeight, len(rows))
	for _, row := range rows[scrollY:end] {
		lines = append(lines, m.renderTreeRow(row))
	}
	for len(lines) < layout.treeHeight {
		lines = append(lines, "")
	}

	switch layout.footerMode {
	case panelFooterCompact:
		lines = append(lines, m.renderCompactFooterLine())
	case panelFooterFull:
		lines = append(lines, m.renderPushPullLine())
		lines = append(lines, m.renderCommitFormLines(layout.bodyHeight)...)
	}

	for len(lines) < m.Height {
		lines = append(lines, "")
	}
	if len(lines) > m.Height {
		lines = lines[:m.Height]
	}

	return strings.Join(lines, "\n")
}

func (m Model) layout() panelLayout {
	if m.Height <= 0 {
		return panelLayout{}
	}

	bodyHeight := m.bodyViewHeight()
	fullFooterHeight := bodyHeight + 5
	switch {
	case m.Height >= fullFooterHeight+1:
		return panelLayout{
			treeHeight: m.Height - fullFooterHeight,
			footerMode: panelFooterFull,
			bodyHeight: bodyHeight,
		}
	case m.Height >= 2:
		return panelLayout{
			treeHeight: m.Height - 1,
			footerMode: panelFooterCompact,
		}
	default:
		return panelLayout{treeHeight: m.Height}
	}
}

func (m Model) treeRows() []treeRowHit {
	return m.treeRowsSnapshot().rows
}

func (m Model) maxTreeScroll(treeHeight int, rowCount int) int {
	if treeHeight <= 0 || rowCount <= treeHeight {
		return 0
	}
	return rowCount - treeHeight
}

func (m Model) normalizedScroll(treeHeight int, rowCount int) int {
	maxScroll := m.maxTreeScroll(treeHeight, rowCount)
	scrollY := m.ScrollY
	if scrollY < 0 {
		scrollY = 0
	}
	if scrollY > maxScroll {
		scrollY = maxScroll
	}
	return scrollY
}

func (m *Model) scrollTree(delta int) {
	rows := m.treeRows()
	layout := m.layout()
	maxScroll := m.maxTreeScroll(layout.treeHeight, len(rows))
	m.ScrollY += delta
	if m.ScrollY < 0 {
		m.ScrollY = 0
	}
	if m.ScrollY > maxScroll {
		m.ScrollY = maxScroll
	}
}

func (m Model) activeTreeRowIndex() (int, bool) {
	cache := m.treeRowsSnapshot()
	start := -1
	length := 0
	switch m.activeSection {
	case SectionStaged:
		start = cache.stagedStart
		length = len(cache.stagedFlat)
	case SectionUnstaged:
		start = cache.unstagedStart
		length = len(cache.unstagedFlat)
	}
	if start >= 0 && m.Cursor >= 0 && m.Cursor < length {
		return start + m.Cursor, true
	}
	return 0, false
}

func (m *Model) ensureCursorVisible() {
	layout := m.layout()
	rows := m.treeRows()
	maxScroll := m.maxTreeScroll(layout.treeHeight, len(rows))

	if layout.treeHeight <= 0 {
		m.ScrollY = 0
		return
	}

	activeRow, ok := m.activeTreeRowIndex()
	if ok {
		if activeRow < m.ScrollY {
			m.ScrollY = activeRow
		}
		if activeRow >= m.ScrollY+layout.treeHeight {
			m.ScrollY = activeRow - layout.treeHeight + 1
		}
	}

	if m.ScrollY < 0 {
		m.ScrollY = 0
	}
	if m.ScrollY > maxScroll {
		m.ScrollY = maxScroll
	}
}

func (m Model) renderTreeRow(row treeRowHit) string {
	switch row.kind {
	case treeRowStagedHeader:
		stagedArrow := "▾"
		if m.stagedCollapsed {
			stagedArrow = "▸"
		}
		stagedLabel := fmt.Sprintf(" STAGED (%d) %s", len(m.Staged), stagedArrow)
		line := m.theme.GitSectionHeader.Render(stagedLabel)
		if len(m.Staged) > 0 {
			line += zone.Mark("git-unstage-all", m.theme.GitUntracked.Render(" −"))
		}
		return line
	case treeRowUnstagedHeader:
		unstagedArrow := "▾"
		if m.unstagedCollapsed {
			unstagedArrow = "▸"
		}
		unstagedLabel := fmt.Sprintf(" CHANGES (%d) %s", len(m.Unstaged), unstagedArrow)
		line := m.theme.GitSectionHeader.Render(unstagedLabel)
		if len(m.Unstaged) > 0 {
			line += zone.Mark("git-stage-all", m.theme.GitAdded.Render(" +"))
		}
		return line
	case treeRowNode:
		if row.node == nil {
			return ""
		}
		return m.renderTreeNode(row.node, row.index, row.section == SectionStaged)
	default:
		return ""
	}
}

func (m Model) renderCompactFooterLine() string {
	if m.spinning {
		return " " + m.spinner.View() + " " + m.spinStatus
	}

	availWidth := m.Width - 2
	if availWidth < 10 {
		availWidth = 10
	}
	commitW := availWidth / 2
	halfW := availWidth / 4
	commitContent := gitActionLabel("\uf417", "Commit")
	pushContent := gitActionLabel("\uf0ee", "Push")
	pullContent := gitActionLabel("\uf0ed", "Pull")
	commitPadded := commitContent + strings.Repeat(" ", max(0, commitW-len(commitContent)))
	pushPadded := pushContent + strings.Repeat(" ", max(0, halfW-len(pushContent)))
	pullPadded := pullContent + strings.Repeat(" ", max(0, halfW-len(pullContent)))
	commitBtn := zone.Mark("git-commit-btn", m.theme.GitActionButton.Render(commitPadded))
	pushBtn := zone.Mark("git-push-btn", m.theme.GitActionButton.Render(pushPadded))
	pullBtn := zone.Mark("git-pull-btn", m.theme.GitActionButton.Render(pullPadded))
	return " " + commitBtn + pushBtn + pullBtn
}

func (m Model) renderPushPullLine() string {
	if m.spinning {
		return " " + m.spinner.View() + " " + m.spinStatus
	}

	availWidth := m.Width - 2
	if availWidth < 10 {
		availWidth = 10
	}
	gap := 1
	btnWidth := (availWidth - gap) / 2
	btnWidthR := availWidth - gap - btnWidth
	pushContent := gitActionLabel("\uf0ee", "Push")
	pullContent := gitActionLabel("\uf0ed", "Pull")
	pushPadded := centerText(pushContent, btnWidth, ' ')
	pullPadded := centerText(pullContent, btnWidthR, ' ')
	pushBtn := zone.Mark("git-push-btn", m.theme.GitPushPullButton.Render(pushPadded))
	pullBtn := zone.Mark("git-pull-btn", m.theme.GitPushPullButton.Render(pullPadded))
	return " " + pushBtn + " " + pullBtn
}

func (m Model) renderCommitFormLines(bodyHeight int) []string {
	innerWidth := m.Width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	borderColor := ui.Nord3
	if m.titleFocused || m.bodyFocused {
		borderColor = ui.Nord8
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	m.commitTitle.SetWidth(innerWidth)
	tiStyles := m.commitTitle.Styles()
	titleBg := ui.Nord1
	if m.titleFocused {
		titleBg = ui.Nord2
	}
	tiStyles.Focused.Text = lipgloss.NewStyle().Background(titleBg).Foreground(ui.Nord6)
	tiStyles.Focused.Placeholder = lipgloss.NewStyle().Background(titleBg).Foreground(ui.Nord4)
	tiStyles.Blurred.Text = lipgloss.NewStyle().Background(ui.Nord1).Foreground(ui.Nord4)
	tiStyles.Blurred.Placeholder = lipgloss.NewStyle().Background(ui.Nord1).Foreground(ui.Nord4)
	m.commitTitle.SetStyles(tiStyles)
	titleView := m.commitTitle.View()
	titleClamped := lipgloss.NewStyle().MaxWidth(innerWidth).Render(titleView)

	bodyBg := ui.Nord1
	if m.bodyFocused {
		bodyBg = ui.Nord2
	}
	m.commitBody.SetWidth(innerWidth)
	m.commitBody.SetHeight(bodyHeight)
	taStyles := m.commitBody.Styles()
	taStyles.Focused.Text = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord6)
	taStyles.Focused.Placeholder = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord4)
	taStyles.Focused.CursorLine = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord6)
	taStyles.Focused.EndOfBuffer = lipgloss.NewStyle().Background(bodyBg).Foreground(bodyBg)
	taStyles.Focused.Prompt = lipgloss.NewStyle().Background(bodyBg).Foreground(bodyBg)
	taStyles.Blurred.Text = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord4)
	taStyles.Blurred.Placeholder = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord4)
	taStyles.Blurred.CursorLine = lipgloss.NewStyle().Background(bodyBg).Foreground(ui.Nord4)
	taStyles.Blurred.EndOfBuffer = lipgloss.NewStyle().Background(bodyBg).Foreground(bodyBg)
	taStyles.Blurred.Prompt = lipgloss.NewStyle().Background(bodyBg).Foreground(bodyBg)
	m.commitBody.SetStyles(taStyles)

	lines := []string{
		borderStyle.Render("╭" + strings.Repeat("─", innerWidth) + "╮"),
		borderStyle.Render("│") + zone.Mark("git-commit-title", titleClamped) + borderStyle.Render("│"),
	}

	bodyLines := strings.Split(m.commitBody.View(), "\n")
	for i := 0; i < bodyHeight; i++ {
		line := strings.Repeat(" ", innerWidth)
		if i < len(bodyLines) {
			line = zone.Mark("git-commit-body", bodyLines[i])
		}
		lines = append(lines, borderStyle.Render("│")+line+borderStyle.Render("│"))
	}

	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", innerWidth)+"╯"))
	if m.spinning {
		lines = append(lines, " "+m.spinner.View()+" "+m.spinStatus)
		return lines
	}

	availWidth := m.Width - 2
	if availWidth < 10 {
		availWidth = 10
	}
	commitContent := gitActionLabel("\uf417", "Commit")
	commitPadded := centerText(commitContent, availWidth, ' ')
	commitBtn := zone.Mark("git-commit-btn", m.theme.GitCommitButton.Render(commitPadded))
	lines = append(lines, " "+commitBtn)
	return lines
}

func (m Model) renderTreeNode(node *GitTreeNode, idx int, staged bool) string {
	isActive := false
	if staged && m.activeSection == SectionStaged && idx == m.Cursor {
		isActive = true
	} else if !staged && m.activeSection == SectionUnstaged && idx == m.Cursor {
		isActive = true
	}

	indent := strings.Repeat("  ", node.Depth)

	if node.IsDir {
		// Directory node: show folder icon with expand/collapse indicator
		arrow := "▾"
		if !node.Expanded {
			arrow = "▸"
		}
		// Build: " {indent}{arrow} {folder} {name}{padding}". Nerd Font
		// glyphs conventionally consume two cells, while the explicit fallback
		// is a single ASCII cell.
		prefix := " " + indent + arrow + " "
		iconStr, iconCells := gitFolderIcon()
		separator := " "
		usedCells := runewidth.StringWidth(prefix) + iconCells + runewidth.StringWidth(separator)
		nameWidth := m.Width - usedCells
		if nameWidth < 1 {
			nameWidth = 1
		}
		dirName := truncPath(node.Name, nameWidth)
		// Pad to fill width
		padLen := m.Width - usedCells - runewidth.StringWidth(dirName)
		if padLen < 0 {
			padLen = 0
		}
		pad := strings.Repeat(" ", padLen)
		raw := prefix + iconStr + separator + dirName + pad
		if isActive {
			return m.theme.GitCursor.Render(raw)
		}
		return m.theme.GitSectionHeader.Render(raw)
	}

	// File node
	e := node.Entry
	if e == nil {
		return ""
	}
	status := e.DisplayStatus(staged)
	name := node.Name

	// " {indent}{status} {name}{padding}"
	prefix := " " + indent + status + " "
	nameWidth := m.Width - runewidth.StringWidth(prefix)
	if nameWidth < 1 {
		nameWidth = 1
	}
	displayName := truncPath(name, nameWidth)
	padLen := m.Width - runewidth.StringWidth(prefix) - runewidth.StringWidth(displayName)
	if padLen < 0 {
		padLen = 0
	}
	pad := strings.Repeat(" ", padLen)

	if isActive {
		return m.theme.GitCursor.Render(prefix + displayName + pad)
	}

	statusStyle := m.statusStyleForByte(status)
	styledPrefix := statusStyle.Render(" " + indent + status)
	nameStr := " " + displayName + pad
	return m.theme.GitEntry.Render(styledPrefix + nameStr)
}

func (m Model) statusStyleForByte(status string) lipgloss.Style {
	switch status {
	case "U":
		return m.theme.GitUntracked
	case "A":
		return m.theme.GitAdded
	case "M":
		return m.theme.GitModified
	case "D":
		return m.theme.GitDeleted
	default:
		return m.theme.GitEntry
	}
}

func truncPath(path string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if runewidth.StringWidth(path) <= maxLen {
		return path
	}
	return runewidth.TruncateLeft(path, maxLen, "...")
}

// centerText pads a string with spaces to center it within a given width.
func centerText(s string, width int, pad rune) string {
	if width <= 0 {
		return s
	}
	sLen := runewidth.StringWidth(s)
	if sLen >= width {
		return runewidth.Truncate(s, width, "")
	}
	left := (width - sLen) / 2
	right := width - sLen - left
	return strings.Repeat(string(pad), left) + s + strings.Repeat(string(pad), right)
}

// gitActionLabel keeps all action names readable when a terminal lacks the
// private-use glyphs supplied by Nerd Font. The words remain the accessibility
// cue; the icon is decorative only.
func gitActionLabel(nerdGlyph, label string) string {
	if ui.NerdFontEnabled() {
		return nerdGlyph + " " + label
	}
	return label
}

func gitFolderIcon() (glyph string, cells int) {
	if ui.NerdFontEnabled() {
		return "\uf413", 2
	}
	return "d", 1
}
