package git

import (
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// GitSection identifies which section has focus within the git panel.
type GitSection int

const (
	SectionUnstaged GitSection = iota
	SectionStaged
	SectionCommitTitle
	SectionCommitBody
)

// StatusEntry represents a file with git status.
type StatusEntry struct {
	Path        string
	IndexStatus byte // X column from porcelain (staged state)
	WorkStatus  byte // Y column from porcelain (working tree state)
	IsDir       bool // true if this was a directory entry (trailing / in porcelain)
}

// IsStagedChange returns true if this entry has a staged change.
func (e StatusEntry) IsStagedChange() bool {
	return e.IndexStatus != ' ' && e.IndexStatus != '?'
}

// IsUnstagedChange returns true if this entry has an unstaged change.
func (e StatusEntry) IsUnstagedChange() bool {
	return e.WorkStatus != ' ' || e.IndexStatus == '?'
}

// IsUntracked returns true if this is an untracked file.
func (e StatusEntry) IsUntracked() bool {
	return e.IndexStatus == '?' && e.WorkStatus == '?'
}

// DisplayStatus returns a human-readable status character.
func (e StatusEntry) DisplayStatus(staged bool) string {
	if staged {
		return displayChar(e.IndexStatus)
	}
	if e.IsUntracked() {
		return "U"
	}
	return displayChar(e.WorkStatus)
}

func displayChar(b byte) string {
	switch b {
	case 'M':
		return "M"
	case 'A':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	case 'C':
		return "C"
	case '?':
		return "U"
	default:
		return string(b)
	}
}

// RefreshMsg is sent when git status data has been fetched.
type RefreshMsg struct {
	Branch  string
	Entries []StatusEntry
	Err     error
}

// GitTreeNode represents a file or directory in the git changed-files tree.
type GitTreeNode struct {
	Name     string       // display name (just the basename)
	Path     string       // full relative path
	IsDir    bool         // true for directories
	Depth    int          // nesting depth
	Entry    *StatusEntry // nil for directories
	Staged   bool         // whether this entry is in staged section
	Children []*GitTreeNode
	Expanded bool
}

// buildTree creates a tree structure from a flat list of status entries.
func buildTree(entries []StatusEntry, staged bool) []*GitTreeNode {
	root := &GitTreeNode{IsDir: true, Expanded: true}

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
			for _, part := range parts {
				found := false
				for _, c := range node.Children {
					if c.IsDir && c.Name == part {
						node = c
						found = true
						break
					}
				}
				if !found {
					dir := &GitTreeNode{
						Name:     part,
						Path:     path,
						IsDir:    true,
						Depth:    node.Depth + 1,
						Expanded: true,
						Entry:    e,
						Staged:   staged,
					}
					if node == root {
						dir.Depth = 0
					}
					node.Children = append(node.Children, dir)
					node = dir
				}
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
				// Find or create directory
				found := false
				for _, c := range node.Children {
					if c.IsDir && c.Name == part {
						node = c
						found = true
						break
					}
				}
				if !found {
					dirPath := strings.Join(parts[:j+1], "/")
					dir := &GitTreeNode{
						Name:     part,
						Path:     dirPath,
						IsDir:    true,
						Depth:    j,
						Expanded: true,
					}
					node.Children = append(node.Children, dir)
					node = dir
				}
			}
		}
	}

	return root.Children
}

// flattenTree flattens a tree into a list of nodes for rendering.
func flattenTree(nodes []*GitTreeNode) []*GitTreeNode {
	var flat []*GitTreeNode
	for _, n := range nodes {
		flat = append(flat, n)
		if n.IsDir && n.Expanded && n.Children != nil {
			flat = append(flat, flattenTree(n.Children)...)
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

	// Commit form
	commitTitle  textinput.Model // single-line title (required)
	commitBody   textarea.Model  // multi-line body (optional) - using bubbles textarea
	titleFocused bool
	bodyFocused  bool

	// Spinner for async operations
	spinner    spinner.Model
	spinning   bool   // true when an async operation is in progress
	spinStatus string // label shown next to spinner (e.g. "Pushing...")
}

// New creates a new git panel model.
func New(rootDir string, theme ui.Theme) Model {
	ti := textinput.New()
	ti.Placeholder = "Commit message"
	ti.CharLimit = 72
	ti.Prompt = ""

	// Initialize textarea for commit body
	ta := textarea.New()
	ta.Placeholder = "Description (optional)"
	ta.SetHeight(5)
	ta.SetWidth(50)
	ta.CharLimit = 10000

	// Apply theme styling to textarea
	taStyles := ta.Styles()
	taStyles.Focused.Text = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord6)
	taStyles.Focused.Placeholder = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	taStyles.Blurred.Text = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	taStyles.Blurred.Placeholder = lipgloss.NewStyle().
		Background(ui.Nord1).
		Foreground(ui.Nord4)
	ta.SetStyles(taStyles)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.Nord8)

	m := Model{
		theme:       theme,
		rootDir:     rootDir,
		commitTitle: ti,
		commitBody:  ta,
		spinner:     sp,
	}
	// Check if inside a git repo
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = rootDir
	if err := cmd.Run(); err == nil {
		m.isGitRepo = true
	}
	return m
}

// IsGitRepo returns whether the root dir is inside a git repository.
func (m Model) IsGitRepo() bool {
	return m.isGitRepo
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
		branch := ""
		branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		branchCmd.Dir = rootDir
		if out, err := branchCmd.Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}

		var entries []StatusEntry
		statusCmd := exec.Command("git", "status", "--porcelain", "-uall")
		statusCmd.Dir = rootDir
		if out, err := statusCmd.Output(); err == nil {
			entries = ParseStatusLines(strings.TrimRight(string(out), "\n"))
		}

		return RefreshMsg{Branch: branch, Entries: entries}
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
	m.stagedTree = buildTree(m.Staged, true)
	m.unstagedTree = buildTree(m.Unstaged, false)
}

// activeList returns the currently focused file list.
func (m Model) activeList() []StatusEntry {
	switch m.activeSection {
	case SectionStaged:
		return m.Staged
	case SectionUnstaged:
		return m.Unstaged
	default:
		return nil
	}
}

// activeFlatTree returns the flattened tree for the active section.
func (m Model) activeFlatTree() []*GitTreeNode {
	switch m.activeSection {
	case SectionStaged:
		return flattenTree(m.stagedTree)
	case SectionUnstaged:
		return flattenTree(m.unstagedTree)
	default:
		return nil
	}
}

// Update handles messages for the git panel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case RefreshMsg:
		if msg.Err == nil {
			m.Branch = msg.Branch
			m.Entries = msg.Entries
			m.deriveGroups()

			// If the active section is now empty, move focus to the other section.
			// This handles the case where unstaging the last staged file leaves the
			// cursor stranded in an empty Staged section (and vice versa for staging).
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
		}
		return m, nil

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
		// Body scrolling is handled internally by textarea component
		// No need for manual scroll handling here
		flat := m.activeFlatTree()
		if len(flat) > 0 && !m.Collapsed {
			if mouse.Button == tea.MouseWheelUp {
				if m.Cursor > 0 {
					m.Cursor--
				}
			} else if mouse.Button == tea.MouseWheelDown {
				if m.Cursor < len(flat)-1 {
					m.Cursor++
				}
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

func (m Model) handleClick(y int) (Model, tea.Cmd) {
	// Zone-based clicks (buttons, stage-all, unstage-all) are handled by app.go
	// which has access to the original absolute-coordinate message.
	// This method only handles positional Y-based clicks.
	line := 0

	// Staged header
	if y == line {
		m.stagedCollapsed = !m.stagedCollapsed
		return m, nil
	}
	line++

	// Staged entries (tree)
	if !m.stagedCollapsed {
		stagedFlat := flattenTree(m.stagedTree)
		for i, node := range stagedFlat {
			if y == line {
				if node.IsDir {
					// Toggle directory expansion
					node.Expanded = !node.Expanded
					return m, nil
				}
				m.activeSection = SectionStaged
				m.Cursor = i
				m.unfocusCommit()
				e := node.Entry
				return m, func() tea.Msg {
					return OpenDiffMsg{Path: e.Path, Status: e.DisplayStatus(true)}
				}
			}
			line++
		}
	}

	// Unstaged header
	if y == line {
		m.unstagedCollapsed = !m.unstagedCollapsed
		return m, nil
	}
	line++

	// Unstaged entries (tree)
	if !m.unstagedCollapsed {
		unstagedFlat := flattenTree(m.unstagedTree)
		for i, node := range unstagedFlat {
			if y == line {
				if node.IsDir {
					node.Expanded = !node.Expanded
					return m, nil
				}
				m.activeSection = SectionUnstaged
				m.Cursor = i
				m.unfocusCommit()
				e := node.Entry
				return m, func() tea.Msg {
					return OpenDiffMsg{Path: e.Path, Status: e.DisplayStatus(false)}
				}
			}
			line++
		}
	}

	return m, nil
}

func (m *Model) unfocusCommit() {
	m.titleFocused = false
	m.bodyFocused = false
	m.commitTitle.Blur()
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
			m.titleFocused = false
			m.commitTitle.Blur()
			m.bodyFocused = true
			m.activeSection = SectionCommitBody
			return m, nil
		case "enter":
			// Move focus to body (like Tab) — commit only via button click
			m.titleFocused = false
			m.commitTitle.Blur()
			m.bodyFocused = true
			m.activeSection = SectionCommitBody
			return m, nil
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
		flat := m.activeFlatTree()
		if m.Cursor > 0 {
			m.Cursor--
		} else if m.activeSection == SectionUnstaged && len(m.stagedTree) > 0 {
			m.activeSection = SectionStaged
			stagedFlat := flattenTree(m.stagedTree)
			m.Cursor = len(stagedFlat) - 1
		}
		_ = flat
		return m, nil
	case "down":
		flat := m.activeFlatTree()
		if m.Cursor < len(flat)-1 {
			m.Cursor++
		} else if m.activeSection == SectionStaged && len(m.unstagedTree) > 0 {
			m.activeSection = SectionUnstaged
			m.Cursor = 0
		}
		return m, nil
	case "enter":
		flat := m.activeFlatTree()
		if len(flat) > 0 && m.Cursor < len(flat) {
			node := flat[m.Cursor]
			if node.IsDir {
				node.Expanded = !node.Expanded
				return m, nil
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
				m.activeSection = SectionCommitTitle
				m.titleFocused = true
				return m, m.commitTitle.Focus()
			}
		case SectionStaged:
			m.activeSection = SectionCommitTitle
			m.titleFocused = true
			return m, m.commitTitle.Focus()
		case SectionCommitTitle, SectionCommitBody:
			m.unfocusCommit()
			m.bodyFocused = false
			m.activeSection = SectionUnstaged
			m.Cursor = 0
		}
		return m, nil
	case "c":
		// Quick focus commit title
		m.activeSection = SectionCommitTitle
		m.titleFocused = true
		return m, m.commitTitle.Focus()
	}
	return m, nil
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
	return m.commitTitle.Focus()
}

// FocusBody focuses the commit body area.
func (m *Model) FocusBody() {
	m.activeSection = SectionCommitBody
	m.bodyFocused = true
	m.titleFocused = false
	m.commitTitle.Blur()
}

// commitFormStartY returns the Y offset within the panel where the commit form
// top border renders, or -1 if the form is not visible.
func (m Model) commitFormStartY() int {
	line := 0
	// Staged header
	line++
	if !m.stagedCollapsed {
		line += len(flattenTree(m.stagedTree))
	}
	// Unstaged header
	line++
	if !m.unstagedCollapsed {
		line += len(flattenTree(m.unstagedTree))
	}

	remaining := m.Height - line
	if remaining < 4 {
		return -1
	}

	bodyHeight := m.bodyViewHeight()
	formMinHeight := 1 + 1 + bodyHeight + 1 + 1
	if formMinHeight > remaining {
		bodyHeight = remaining - 4
		if bodyHeight < 1 {
			bodyHeight = 1
		}
	}

	// Padding before form
	padLines := remaining - (1 + 1 + bodyHeight + 1 + 1)
	line += padLines
	return line
}

// FocusBodyAt focuses the body at the clicked location.
// Note: With textarea component, precise cursor positioning on click is handled internally.
func (m *Model) FocusBodyAt(panelY, panelX int) {
	m.activeSection = SectionCommitBody
	m.bodyFocused = true
	m.titleFocused = false
	m.commitTitle.Blur()
	// Textarea handles cursor positioning internally when focused
}

// FocusTitleAt focuses the title and positions the cursor near the click X.
func (m *Model) FocusTitleAt(panelX int) tea.Cmd {
	m.activeSection = SectionCommitTitle
	m.titleFocused = true
	m.bodyFocused = false

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
	bodyHeight := m.bodyViewHeight()
	// Adjust body height the same way commitFormStartY does
	line := 0
	line++ // staged header
	if !m.stagedCollapsed {
		line += len(flattenTree(m.stagedTree))
	}
	line++ // unstaged header
	if !m.unstagedCollapsed {
		line += len(flattenTree(m.unstagedTree))
	}
	remaining := m.Height - line
	formMinHeight := 1 + 1 + bodyHeight + 1 + 1
	if formMinHeight > remaining {
		bodyHeight = remaining - 4
		if bodyHeight < 1 {
			bodyHeight = 1
		}
	}
	if panelY >= bodyStartY && panelY < bodyStartY+bodyHeight {
		return "body"
	}
	return ""
}

// EntryAtY returns the status entry at the given panel Y coordinate and whether it's staged.
// Returns nil if Y doesn't correspond to a file entry.
func (m Model) EntryAtY(y int) (*StatusEntry, bool) {
	line := 0

	// Staged header
	if y == line {
		return nil, false
	}
	line++

	// Staged entries (tree)
	if !m.stagedCollapsed {
		stagedFlat := flattenTree(m.stagedTree)
		for _, node := range stagedFlat {
			if y == line {
				if node.Entry != nil {
					return node.Entry, true
				}
				return nil, false
			}
			line++
		}
	}

	// Unstaged header
	if y == line {
		return nil, false
	}
	line++

	// Unstaged entries (tree)
	if !m.unstagedCollapsed {
		unstagedFlat := flattenTree(m.unstagedTree)
		for _, node := range unstagedFlat {
			if y == line {
				if node.Entry != nil {
					return node.Entry, false
				}
				return nil, false
			}
			line++
		}
	}

	return nil, false
}

// NodeAtY returns the tree node at the given panel Y coordinate and whether it's in the staged section.
// Returns nil if Y doesn't correspond to any node.
func (m Model) NodeAtY(y int) (*GitTreeNode, bool) {
	line := 0

	// Staged header
	if y == line {
		return nil, false
	}
	line++

	// Staged entries (tree)
	if !m.stagedCollapsed {
		stagedFlat := flattenTree(m.stagedTree)
		for _, node := range stagedFlat {
			if y == line {
				return node, true
			}
			line++
		}
	}

	// Unstaged header
	if y == line {
		return nil, false
	}
	line++

	// Unstaged entries (tree)
	if !m.unstagedCollapsed {
		unstagedFlat := flattenTree(m.unstagedTree)
		for _, node := range unstagedFlat {
			if y == line {
				return node, false
			}
			line++
		}
	}

	return nil, false
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
	m.Width = w
	m.Height = h
	// Keep the commit title input width in sync so its internal
	// cursor/scroll logic works correctly during Update().
	innerWidth := w - 2 // minus left+right border chars
	if innerWidth < 1 {
		innerWidth = 1
	}
	m.commitTitle.SetWidth(innerWidth)
}

// View renders the git panel.
func (m Model) View() string {
	if !m.isGitRepo || m.Width == 0 || m.Height == 0 {
		return ""
	}

	var sb strings.Builder
	linesUsed := 0

	// 1. Staged section header with unstage-all button
	stagedArrow := "▾"
	if m.stagedCollapsed {
		stagedArrow = "▸"
	}
	stagedLabel := fmt.Sprintf(" STAGED (%d) %s", len(m.Staged), stagedArrow)
	stagedHeaderText := m.theme.GitSectionHeader.Render(stagedLabel)
	if len(m.Staged) > 0 {
		unstageAllBtn := zone.Mark("git-unstage-all",
			m.theme.GitUntracked.Render(" −"))
		stagedHeaderText += unstageAllBtn
	}
	sb.WriteString(stagedHeaderText)
	linesUsed++

	// Staged entries (tree view)
	if !m.stagedCollapsed {
		stagedFlat := flattenTree(m.stagedTree)
		for i, node := range stagedFlat {
			if linesUsed >= m.Height {
				break
			}
			sb.WriteByte('\n')
			sb.WriteString(m.renderTreeNode(node, i, true))
			linesUsed++
		}
	}

	if linesUsed >= m.Height {
		return sb.String()
	}

	// 2. Unstaged / Changes section header with stage-all button
	unstagedArrow := "▾"
	if m.unstagedCollapsed {
		unstagedArrow = "▸"
	}
	unstagedLabel := fmt.Sprintf(" CHANGES (%d) %s", len(m.Unstaged), unstagedArrow)
	unstagedHeaderText := m.theme.GitSectionHeader.Render(unstagedLabel)
	if len(m.Unstaged) > 0 {
		stageAllBtn := zone.Mark("git-stage-all",
			m.theme.GitAdded.Render(" +"))
		unstagedHeaderText += stageAllBtn
	}
	sb.WriteByte('\n')
	sb.WriteString(unstagedHeaderText)
	linesUsed++

	// Unstaged entries (tree view)
	if !m.unstagedCollapsed {
		unstagedFlat := flattenTree(m.unstagedTree)
		for i, node := range unstagedFlat {
			if linesUsed >= m.Height {
				break
			}
			sb.WriteByte('\n')
			sb.WriteString(m.renderTreeNode(node, i, false))
			linesUsed++
		}
	}

	if linesUsed >= m.Height {
		return sb.String()
	}

	// 3. Commit form area — rendered as a bordered box at the bottom
	// Calculate how much space we have left for the commit form
	// We need: 1 (push/pull buttons) + 1 (top border) + 1 (title) + bodyHeight (body) + 1 (bottom border) + 1 (commit button)
	bodyHeight := m.bodyViewHeight()
	formMinHeight := 1 + 1 + 1 + bodyHeight + 1 + 1
	remaining := m.Height - linesUsed
	if remaining < 3 {
		// Not enough space, show spinner only
		sb.WriteByte('\n')
		linesUsed++
		if m.spinning {
			sb.WriteString(" " + m.spinner.View() + " " + m.spinStatus)
		} else {
			// Show compact single row of buttons with manual spacing
			availWidth := m.Width - 2
			if availWidth < 10 {
				availWidth = 10
			}
			// Split available width into 3 parts: commit gets 1/2, push/pull get 1/4 each
			commitW := availWidth / 2
			halfW := availWidth / 4
			commitContent := "\uf417 Commit"
			pushContent := "\uf0ee Push"
			pullContent := "\uf0ed Pull"
			// Pad each to their section width
			commitPadded := commitContent + strings.Repeat(" ", commitW-len(commitContent))
			pushPadded := pushContent + strings.Repeat(" ", halfW-len(pushContent))
			pullPadded := pullContent + strings.Repeat(" ", halfW-len(pullContent))
			commitBtn := zone.Mark("git-commit-btn", m.theme.GitActionButton.Render(commitPadded))
			pushBtn := zone.Mark("git-push-btn", m.theme.GitActionButton.Render(pushPadded))
			pullBtn := zone.Mark("git-pull-btn", m.theme.GitActionButton.Render(pullPadded))
			sb.WriteString(" " + commitBtn + pushBtn + pullBtn)
		}
		linesUsed++
		for linesUsed < m.Height {
			sb.WriteByte('\n')
			linesUsed++
		}
		return sb.String()
	}

	// Adjust body height if not enough space
	if formMinHeight > remaining {
		bodyHeight = remaining - 4 // 1 (push/pull) + 1 top + 1 title + 1 bottom + 1 commit
		if bodyHeight < 1 {
			bodyHeight = 1
		}
	}

	// Pad before the commit form — push it toward the bottom
	padLines := remaining - (1 + 1 + bodyHeight + 1 + 1 + 1) // push/pull + top + title + body + bottom + commit
	for range padLines {
		sb.WriteByte('\n')
		linesUsed++
	}

	// Push and Pull buttons — rendered above the commit form
	if linesUsed < m.Height {
		if m.spinning {
			sb.WriteByte('\n')
			linesUsed++
			sb.WriteString(" " + m.spinner.View() + " " + m.spinStatus)
		} else {
			sb.WriteByte('\n')
			linesUsed++
			availWidth := m.Width - 2
			if availWidth < 10 {
				availWidth = 10
			}
			gap := 1
			btnWidth := (availWidth - gap) / 2
			btnWidthR := availWidth - gap - btnWidth
			pushContent := "\uf0ee Push"
			pullContent := "\uf0ed Pull"
			pushPadded := centerText(pushContent, btnWidth, ' ')
			pullPadded := centerText(pullContent, btnWidthR, ' ')
			pushBtn := zone.Mark("git-push-btn",
				m.theme.GitPushPullButton.Render(pushPadded))
			pullBtn := zone.Mark("git-pull-btn",
				m.theme.GitPushPullButton.Render(pullPadded))
			sb.WriteString(" " + pushBtn + " " + pullBtn)
		}
	}

	// Commit form with box border
	innerWidth := m.Width - 2 // -2 for left+right border chars
	if innerWidth < 1 {
		innerWidth = 1
	}

	borderColor := ui.Nord3
	if m.titleFocused || m.bodyFocused {
		borderColor = ui.Nord8 // highlight border when focused
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Top border: ╭─────╮
	topBorder := "╭" + strings.Repeat("─", innerWidth) + "╮"
	sb.WriteByte('\n')
	sb.WriteString(borderStyle.Render(topBorder))
	linesUsed++

	// Title line: │ Message ...         │
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
	sb.WriteByte('\n')
	sb.WriteString(borderStyle.Render("│") + zone.Mark("git-commit-title", titleClamped) + borderStyle.Render("│"))
	linesUsed++

	// Body lines: │ Description ...     │
	bodyBg := ui.Nord1
	if m.bodyFocused {
		bodyBg = ui.Nord2
	}

	// Body lines using textarea component
	m.commitBody.SetWidth(innerWidth)
	m.commitBody.SetHeight(bodyHeight)

	// Apply border background to textarea styles
	taStyles := m.commitBody.Styles()
	if m.bodyFocused {
		taStyles.Focused.Text = lipgloss.NewStyle().
			Background(bodyBg).
			Foreground(ui.Nord6)
		taStyles.Focused.Placeholder = lipgloss.NewStyle().
			Background(bodyBg).
			Foreground(ui.Nord4)
	} else {
		taStyles.Blurred.Text = lipgloss.NewStyle().
			Background(bodyBg).
			Foreground(ui.Nord4)
		taStyles.Blurred.Placeholder = lipgloss.NewStyle().
			Background(bodyBg).
			Foreground(ui.Nord4)
	}
	m.commitBody.SetStyles(taStyles)

	// Render textarea view
	bodyView := m.commitBody.View()
	bodyLines := strings.Split(bodyView, "\n")
	for i := 0; i < bodyHeight; i++ {
		sb.WriteByte('\n')
		sb.WriteString(borderStyle.Render("│"))
		linesUsed++
		if i < len(bodyLines) {
			sb.WriteString(bodyLines[i])
		} else {
			sb.WriteString(strings.Repeat(" ", innerWidth))
		}
		sb.WriteString(borderStyle.Render("│"))
	}

	// Bottom border: ╰─────╯
	bottomBorder := "╰" + strings.Repeat("─", innerWidth) + "╯"
	sb.WriteByte('\n')
	sb.WriteString(borderStyle.Render(bottomBorder))
	linesUsed++

	if linesUsed >= m.Height {
		return sb.String()
	}

	// Action buttons with icons (or spinner when busy)
	if m.spinning {
		sb.WriteByte('\n')
		linesUsed++
		sb.WriteString(" " + m.spinner.View() + " " + m.spinStatus)
	} else {
		// Calculate available width for buttons (1-space padding on each side)
		availWidth := m.Width - 2
		if availWidth < 10 {
			availWidth = 10
		}

		// Row 1: Commit button full width
		sb.WriteByte('\n')
		linesUsed++
		commitContent := "\uf417 Commit"
		commitPadded := centerText(commitContent, availWidth, ' ')
		commitBtn := zone.Mark("git-commit-btn",
			m.theme.GitCommitButton.Render(commitPadded))
		sb.WriteString(" " + commitBtn)
	}

	// Pad remaining height
	for linesUsed < m.Height {
		sb.WriteByte('\n')
		linesUsed++
	}

	return sb.String()
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
		// Build: " {indent}{arrow}  {name}{padding}"
		// The folder icon takes 2 display cells but we render it with a styled span.
		// Avoid relying on lipgloss Width which miscounts icon width.
		prefix := " " + indent + arrow + " "
		iconStr := "\uf413"
		const iconCells = 2 // Nerd Font icon is 2 cells wide in terminal
		separator := " "
		usedCells := len(prefix) + iconCells + len(separator)
		nameWidth := m.Width - usedCells
		if nameWidth < 1 {
			nameWidth = 1
		}
		dirName := truncPath(node.Name, nameWidth)
		// Pad to fill width
		padLen := m.Width - usedCells - len(dirName)
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
	nameWidth := m.Width - len(prefix)
	if nameWidth < 1 {
		nameWidth = 1
	}
	displayName := truncPath(name, nameWidth)
	padLen := m.Width - len(prefix) - len(displayName)
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
	if maxLen <= 0 || len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

// centerText pads a string with spaces to center it within a given width.
func centerText(s string, width int, pad rune) string {
	if width <= 0 {
		return s
	}
	sLen := len(s)
	if sLen >= width {
		return s[:width]
	}
	left := (width - sLen) / 2
	right := width - sLen - left
	return strings.Repeat(string(pad), left) + s + strings.Repeat(string(pad), right)
}
