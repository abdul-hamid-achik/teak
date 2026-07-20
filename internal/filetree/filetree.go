package filetree

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// OpenFileMsg is sent when a file is selected in the tree (single-click = preview).
type OpenFileMsg struct {
	Path string
}

// PinFileMsg is sent when a file should be opened permanently (double-click or enter).
type PinFileMsg struct {
	Path string
}

// DirExpandedMsg is sent when a directory's children have been read asynchronously.
type DirExpandedMsg struct {
	Path     string
	Children []Entry
	Err      error
}

// RefreshResult is an immutable directory-tree snapshot built away from the
// Bubble Tea update loop. It intentionally contains only filesystem-derived
// state; diagnostics, git state, viewport position and current selection stay
// owned by the live Model when ApplyRefresh is called.
type RefreshResult struct {
	Entries           []Entry
	GitignorePatterns []string
}

const (
	// A refresh is triggered by external events, so it must have a finite cost
	// even if a generated directory is accidentally expanded. The limits apply
	// only to the expanded portion of the visible tree and leave the previous
	// tree intact when exceeded.
	maxRefreshDirectories = 2_048
	maxRefreshEntries     = 100_000
	// A single directory feeds the flattened tree and is rendered as one
	// allocation-heavy slice. Cap it separately from the recursive refresh
	// budget so one generated directory cannot exhaust memory on its own.
	maxDirectoryEntries = 4_096
	// Gitignore is workspace-controlled input read during startup and every
	// watcher refresh. Keep a malformed/generated file from allocating an
	// unbounded string slice or delaying the TUI's background commands.
	maxGitignoreBytes     = 1 << 20
	maxGitignorePatterns  = 10_000
	maxGitignoreLineBytes = 4 << 10
)

var errDirectoryEntryLimit = errors.New("tree directory entry limit exceeded")

type refreshBudget struct {
	directories int
	entries     int
}

// Entry represents a file or directory in the tree.
type Entry struct {
	Name         string
	Path         string
	IsDir        bool
	Children     []Entry
	Expanded     bool
	Loading      bool // true while async directory read is in progress
	Depth        int
	IsGitIgnored bool // true if entry matches .gitignore
}

// Model is a file tree sidebar sub-model.
type Model struct {
	Root              string
	Entries           []Entry
	Cursor            int
	ScrollY           int
	Width             int
	Height            int
	theme             ui.Theme
	cachedFlat        []Entry
	sharedFlatCache   *flatEntryCache
	diagnostics       map[string]int    // path → worst severity (1=error, 2=warn, 3=info, 4=hint)
	gitStatus         map[string]string // relative path → status ("M", "A", "D", "U")
	gitignorePatterns []string          // patterns from .gitignore
	lastClickPath     string
	lastClickTime     time.Time

	// CACHED STYLES - pre-allocated to avoid per-frame allocations in View()
	cachedStyles struct {
		base            lipgloss.Style // base style for applying dynamic colors
		cursorBg        color.Color    // cached cursor background
		entryBg         color.Color    // cached entry background
		gitIgnoredColor color.Color    // cached git ignored color (Nord3)
	}
}

type flatEntryCache struct {
	entries []Entry
}

// SetDiagnostics sets the diagnostics map (file paths + directory paths → worst severity).
func (m *Model) SetDiagnostics(diags map[string]int) {
	m.diagnostics = diags
}

// SetGitStatus sets the git status map (relative paths → display status).
func (m *Model) SetGitStatus(status map[string]string) {
	m.gitStatus = status
}

// New creates a new file tree model rooted at the given directory.
// Only reads the first level synchronously for fast startup.
func New(root string, theme ui.Theme) Model {
	m := NewEmpty(root, theme)
	m.LoadRoot()
	return m
}

// NewEmpty constructs a render-safe tree without touching the filesystem. It
// is used by the application constructor so a slow network directory cannot
// delay the first Bubble Tea frame.
func NewEmpty(root string, theme ui.Theme) Model {
	m := Model{
		Root:            root,
		theme:           theme,
		sharedFlatCache: &flatEntryCache{},
	}

	// Initialize cached styles to avoid per-frame allocations
	m.cachedStyles.base = lipgloss.NewStyle()
	m.cachedStyles.cursorBg = theme.TreeCursor.GetBackground()
	m.cachedStyles.entryBg = theme.TreeEntry.GetBackground()
	m.cachedStyles.gitIgnoredColor = ui.Nord3

	return m
}

// LoadRoot performs the initial top-level read. Call this in a tea.Cmd when
// startup latency matters; New retains the convenient synchronous API for
// isolated filetree consumers and tests.
func (m *Model) LoadRoot() {
	m.gitignorePatterns = loadGitignore(m.Root)
	m.Entries = readDirEntries(m.Root, m.Root, 0, m.gitignorePatterns)
	m.invalidateFlatCache()
}

// RefreshDir re-reads a directory's children synchronously and updates the tree.
// If the directory is the root, it refreshes the top-level entries.
func (m *Model) RefreshDir(dir string) {
	if dir == m.Root {
		m.Entries = refreshEntriesPreservingExpansion(m.Root, m.Root, 0, m.gitignorePatterns, m.Entries)
		m.invalidateFlatCache()
		return
	}
	refreshInSlice(m.Entries, m.Root, dir, m.gitignorePatterns)
	m.invalidateFlatCache()
}

// SnapshotForRefresh makes the filesystem-facing portion of a Model safe to
// hand to a background tea.Cmd. Model updates mutate entry slices in place, so
// passing the live tree to a goroutine would otherwise race with keyboard and
// mouse interactions.
func (m Model) SnapshotForRefresh() Model {
	m.Entries = cloneEntries(m.Entries)
	m.gitignorePatterns = append([]string(nil), m.gitignorePatterns...)
	m.cachedFlat = nil
	m.sharedFlatCache = &flatEntryCache{}
	return m
}

// Refresh builds a replacement tree snapshot without mutating the live model.
// Call it from a tea.Cmd, using SnapshotForRefresh before scheduling the
// command. Cancellation is checked between directory batches and recursive
// expanded directories; a single operating-system ReadDir call itself cannot
// be interrupted by Go.
func (m Model) Refresh(ctx context.Context, dir string) (RefreshResult, error) {
	if err := validRefreshDirectory(m.Root, dir); err != nil {
		return RefreshResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}

	patterns, err := loadGitignoreContext(ctx, m.Root)
	if err != nil {
		return RefreshResult{}, err
	}
	budget := &refreshBudget{}
	if filepath.Clean(dir) == filepath.Clean(m.Root) {
		entries, err := refreshEntriesPreservingExpansionContext(ctx, m.Root, m.Root, 0, patterns, m.Entries, budget)
		if err != nil {
			return RefreshResult{}, err
		}
		return RefreshResult{Entries: entries, GitignorePatterns: patterns}, nil
	}
	if _, err := refreshInSliceContext(ctx, m.Entries, m.Root, filepath.Clean(dir), patterns, budget); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Entries: m.Entries, GitignorePatterns: patterns}, nil
}

// ApplyRefresh installs a snapshot generated by Refresh while retaining UI
// state that may have changed while the disk read was in flight. In particular
// it preserves the selected path rather than an unstable flattened index.
func (m *Model) ApplyRefresh(result RefreshResult) {
	selectedPath := m.selectedPath()
	m.Entries = mergeRefreshedEntries(m.Entries, result.Entries)
	m.gitignorePatterns = append([]string(nil), result.GitignorePatterns...)
	m.invalidateFlatCache()
	if selectedPath != "" {
		flat := m.flatEntries()
		for i, entry := range flat {
			if entry.Path == selectedPath {
				m.Cursor = i
				break
			}
		}
	}
	m.ensureCursorVisible()
}

func (m *Model) selectedPath() string {
	flat := m.flatEntries()
	if m.Cursor < 0 || m.Cursor >= len(flat) {
		return ""
	}
	return flat[m.Cursor].Path
}

func cloneEntries(entries []Entry) []Entry {
	if entries == nil {
		return nil
	}
	cloned := make([]Entry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Children = cloneEntries(entry.Children)
	}
	return cloned
}

// mergeRefreshedEntries gives the live model's expansion/loading state
// precedence. A user can expand or collapse a directory while a background
// refresh is running; applying an old snapshot must never undo that action.
func mergeRefreshedEntries(live, refreshed []Entry) []Entry {
	if len(live) == 0 || len(refreshed) == 0 {
		return refreshed
	}
	liveByPath := make(map[string]Entry, len(live))
	for _, entry := range live {
		liveByPath[entry.Path] = entry
	}
	for i := range refreshed {
		previous, ok := liveByPath[refreshed[i].Path]
		if !ok || !refreshed[i].IsDir {
			continue
		}
		wasExpanded := refreshed[i].Expanded
		refreshed[i].Expanded = previous.Expanded
		if previous.Loading {
			refreshed[i].Loading = true
		}
		if previous.Expanded && !wasExpanded {
			refreshed[i].Children = previous.Children
			continue
		}
		if len(refreshed[i].Children) > 0 {
			refreshed[i].Children = mergeRefreshedEntries(previous.Children, refreshed[i].Children)
		}
	}
	return refreshed
}

func validRefreshDirectory(root, dir string) error {
	if root == "" || dir == "" {
		return fmt.Errorf("tree refresh requires a workspace directory")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve tree root: %w", err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve tree root symlinks: %w", err)
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve tree refresh directory: %w", err)
	}
	dirResolved, err := filepath.EvalSymlinks(dirAbs)
	if err != nil {
		return fmt.Errorf("resolve tree refresh directory symlinks: %w", err)
	}
	rel, err := filepath.Rel(rootResolved, dirResolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("tree refresh directory is outside the workspace")
	}
	return nil
}

func refreshEntriesPreservingExpansionContext(ctx context.Context, rootDir, dir string, depth int, gitignorePatterns []string, previous []Entry, budget *refreshBudget) ([]Entry, error) {
	refreshed, err := readDirEntriesContext(ctx, rootDir, dir, depth, gitignorePatterns, budget)
	if err != nil {
		return nil, err
	}
	if len(previous) == 0 {
		return refreshed, nil
	}

	previousByPath := make(map[string]Entry, len(previous))
	for _, entry := range previous {
		previousByPath[entry.Path] = entry
	}
	for i := range refreshed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prev, ok := previousByPath[refreshed[i].Path]
		if !ok || !refreshed[i].IsDir {
			continue
		}
		refreshed[i].Expanded = prev.Expanded
		if !prev.Expanded {
			continue
		}
		children, err := refreshEntriesPreservingExpansionContext(ctx, rootDir, refreshed[i].Path, refreshed[i].Depth+1, gitignorePatterns, prev.Children, budget)
		if err != nil {
			return nil, err
		}
		refreshed[i].Children = children
		refreshed[i].Loading = false
	}
	return refreshed, nil
}

func refreshInSliceContext(ctx context.Context, entries []Entry, rootDir, dir string, gitignorePatterns []string, budget *refreshBudget) (bool, error) {
	for i := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if entries[i].Path == dir && entries[i].IsDir {
			children, err := refreshEntriesPreservingExpansionContext(ctx, rootDir, dir, entries[i].Depth+1, gitignorePatterns, entries[i].Children, budget)
			if err != nil {
				return false, err
			}
			entries[i].Children = children
			entries[i].Loading = false
			return true, nil
		}
		if entries[i].Children != nil {
			found, err := refreshInSliceContext(ctx, entries[i].Children, rootDir, dir, gitignorePatterns, budget)
			if err != nil || found {
				return found, err
			}
		}
	}
	return false, nil
}

func refreshEntriesPreservingExpansion(rootDir, dir string, depth int, gitignorePatterns []string, previous []Entry) []Entry {
	refreshed := readDirEntries(rootDir, dir, depth, gitignorePatterns)
	if len(previous) == 0 {
		return refreshed
	}

	previousByPath := make(map[string]Entry, len(previous))
	for _, entry := range previous {
		previousByPath[entry.Path] = entry
	}

	for i := range refreshed {
		prev, ok := previousByPath[refreshed[i].Path]
		if !ok || !refreshed[i].IsDir {
			continue
		}

		refreshed[i].Expanded = prev.Expanded
		if prev.Expanded {
			refreshed[i].Children = refreshEntriesPreservingExpansion(
				rootDir,
				refreshed[i].Path,
				refreshed[i].Depth+1,
				gitignorePatterns,
				prev.Children,
			)
			refreshed[i].Loading = false
		}
	}

	return refreshed
}

func refreshInSlice(entries []Entry, rootDir, dir string, gitignorePatterns []string) bool {
	for i := range entries {
		if entries[i].Path == dir && entries[i].IsDir {
			entries[i].Children = refreshEntriesPreservingExpansion(
				rootDir,
				dir,
				entries[i].Depth+1,
				gitignorePatterns,
				entries[i].Children,
			)
			entries[i].Loading = false
			return true
		}
		if entries[i].Children != nil {
			if refreshInSlice(entries[i].Children, rootDir, dir, gitignorePatterns) {
				return true
			}
		}
	}
	return false
}

// Update handles input for the file tree.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case DirExpandedMsg:
		return m.handleDirExpanded(msg)
	}
	return m, nil
}

func (m Model) handleKeyPress(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	flat := m.flatEntries()
	switch msg.String() {
	case "up":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down":
		if m.Cursor < len(flat)-1 {
			m.Cursor++
		}
	case "enter":
		if m.Cursor < len(flat) {
			entry := flat[m.Cursor]
			if entry.IsDir {
				return m.toggleDir(entry.Path)
			}
			// Enter pins the file (not a preview)
			return m, func() tea.Msg {
				return PinFileMsg{Path: entry.Path}
			}
		}
	}
	m.ensureCursorVisible()
	return m, nil
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Y < 0 || (m.Height > 0 && mouse.Y >= m.Height) {
		return m, nil
	}
	idx := m.ScrollY + mouse.Y
	flat := m.flatEntries()
	if idx < 0 || idx >= len(flat) {
		return m, nil
	}
	m.Cursor = idx
	entry := flat[idx]
	if entry.IsDir {
		return m.toggleDir(entry.Path)
	}

	// Detect double-click: same path within 400ms
	now := time.Now()
	isDoubleClick := entry.Path == m.lastClickPath && now.Sub(m.lastClickTime) < 400*time.Millisecond
	m.lastClickPath = entry.Path
	m.lastClickTime = now

	if isDoubleClick {
		return m, func() tea.Msg {
			return PinFileMsg{Path: entry.Path}
		}
	}
	return m, func() tea.Msg {
		return OpenFileMsg{Path: entry.Path}
	}
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	flat := m.flatEntries()
	maxScroll := len(flat) - m.Height
	if maxScroll < 0 {
		maxScroll = 0
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		m.ScrollY -= 3
		if m.ScrollY < 0 {
			m.ScrollY = 0
		}
	case tea.MouseWheelDown:
		m.ScrollY += 3
		if m.ScrollY > maxScroll {
			m.ScrollY = maxScroll
		}
	}
	return m, nil
}

func (m Model) handleDirExpanded(msg DirExpandedMsg) (Model, tea.Cmd) {
	setDirectoryLoadResultInSlice(m.Entries, msg.Path, msg.Children, msg.Err)
	m.invalidateFlatCache()
	m.ensureCursorVisible()
	return m, nil
}

// EntryAtY returns the entry at the given screen Y position, or nil.
func (m Model) EntryAtY(y int) *Entry {
	if y < 0 || (m.Height > 0 && y >= m.Height) {
		return nil
	}
	flat := m.flatEntries()
	idx := m.ScrollY + y
	if idx < 0 || idx >= len(flat) {
		return nil
	}
	return &flat[idx]
}

// ToggleEntry toggles the expand state of a directory entry by path.
func (m *Model) ToggleEntry(path string) (Model, tea.Cmd) {
	updated, cmd := m.toggleDir(path)
	updated.ensureCursorVisible()
	return updated, cmd
}

// toggleDir toggles a directory's expanded state.
// If expanding and children aren't loaded, starts an async read.
func (m *Model) toggleDir(path string) (Model, tea.Cmd) {
	cmd := toggleInSlice(m.Entries, m.Root, path, m.gitignorePatterns)
	m.invalidateFlatCache()
	m.ensureCursorVisible()
	return *m, cmd
}

// toggleInSlice toggles expansion and returns a command if async loading is needed.
func toggleInSlice(entries []Entry, rootDir, path string, gitignorePatterns []string) tea.Cmd {
	for i := range entries {
		if entries[i].Path == path && entries[i].IsDir {
			entries[i].Expanded = !entries[i].Expanded
			if entries[i].Expanded && entries[i].Children == nil && !entries[i].Loading {
				// Start async directory read
				entries[i].Loading = true
				dirPath := entries[i].Path
				depth := entries[i].Depth + 1
				return func() tea.Msg {
					children, err := readDirEntriesContext(context.Background(), rootDir, dirPath, depth, gitignorePatterns, &refreshBudget{})
					return DirExpandedMsg{Path: dirPath, Children: children, Err: err}
				}
			}
			return nil
		}
		if entries[i].Expanded && entries[i].Children != nil {
			if cmd := toggleInSlice(entries[i].Children, rootDir, path, gitignorePatterns); cmd != nil {
				return cmd
			}
		}
	}
	return nil
}

// setChildrenInSlice finds the entry by path and sets its children.
func setChildrenInSlice(entries []Entry, path string, children []Entry) bool {
	return setDirectoryLoadResultInSlice(entries, path, children, nil)
}

// setDirectoryLoadResultInSlice completes an asynchronous read. On failure it
// clears the spinner but retains cached children: a resource-limit error must
// never turn a previously visible subtree into a partial or empty listing.
func setDirectoryLoadResultInSlice(entries []Entry, path string, children []Entry, loadErr error) bool {
	for i := range entries {
		if entries[i].Path == path && entries[i].IsDir {
			entries[i].Loading = false
			if loadErr == nil {
				entries[i].Children = children
			}
			return true
		}
		if entries[i].Children != nil {
			if setDirectoryLoadResultInSlice(entries[i].Children, path, children, loadErr) {
				return true
			}
		}
	}
	return false
}

func (m *Model) ensureCursorVisible() {
	m.clampCursor()
	if m.Height <= 0 {
		m.ScrollY = 0
		return
	}
	if m.Cursor < m.ScrollY {
		m.ScrollY = m.Cursor
	}
	if m.Cursor >= m.ScrollY+m.Height {
		m.ScrollY = m.Cursor - m.Height + 1
	}
}

func (m *Model) clampCursor() {
	flat := m.flatEntries()
	if len(flat) == 0 {
		m.Cursor = 0
		m.ScrollY = 0
		return
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= len(flat) {
		m.Cursor = len(flat) - 1
	}
	maxScroll := max(0, len(flat)-max(1, m.Height))
	if m.ScrollY < 0 {
		m.ScrollY = 0
	}
	if m.ScrollY > maxScroll {
		m.ScrollY = maxScroll
	}
}

func (m *Model) flatEntries() []Entry {
	if m.sharedFlatCache != nil && m.sharedFlatCache.entries != nil {
		return m.sharedFlatCache.entries
	}
	if m.cachedFlat != nil {
		return m.cachedFlat
	}
	var flat []Entry
	flattenEntries(m.Entries, &flat)
	m.cachedFlat = flat
	if m.sharedFlatCache != nil {
		m.sharedFlatCache.entries = flat
	}
	return flat
}

func (m *Model) invalidateFlatCache() {
	m.cachedFlat = nil
	if m.sharedFlatCache != nil {
		m.sharedFlatCache.entries = nil
	}
}

func flattenEntries(entries []Entry, flat *[]Entry) {
	for _, e := range entries {
		*flat = append(*flat, e)
		if e.IsDir && e.Expanded && e.Children != nil {
			flattenEntries(e.Children, flat)
		}
	}
}

func readDirEntries(rootDir, path string, depth int, gitignorePatterns []string) []Entry {
	entries, err := readDirEntriesContext(context.Background(), rootDir, path, depth, gitignorePatterns, &refreshBudget{})
	if err != nil {
		return nil
	}
	return entries
}

// readDirEntriesContext is the bounded, cancellable counterpart to
// readDirEntries used for watcher-driven refreshes. Reading in batches lets a
// superseding filesystem event stop a deep refresh promptly.
func readDirEntriesContext(ctx context.Context, rootDir, path string, depth int, gitignorePatterns []string, budget *refreshBudget) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if budget.directories >= maxRefreshDirectories {
		return nil, fmt.Errorf("tree refresh exceeds %d expanded directories", maxRefreshDirectories)
	}
	budget.directories++

	dir, err := os.Open(path)
	if err != nil {
		// A directory can disappear between fsnotify's event and this command.
		// Treat it as an empty listing so the next snapshot removes it cleanly.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = dir.Close() }()

	var dirs, files []Entry
	directoryEntries := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dirEntries, readErr := dir.ReadDir(256)
		for _, de := range dirEntries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if budget.entries >= maxRefreshEntries {
				return nil, fmt.Errorf("tree refresh exceeds %d entries", maxRefreshEntries)
			}
			if directoryEntries >= maxDirectoryEntries {
				return nil, fmt.Errorf("%w: %q exceeds %d entries", errDirectoryEntryLimit, path, maxDirectoryEntries)
			}
			budget.entries++
			directoryEntries++
			name := de.Name()
			fullPath := filepath.Join(path, name)
			relPath, err := filepath.Rel(rootDir, fullPath)
			if err != nil {
				relPath = fullPath
			}
			entry := Entry{
				Name:         name,
				Path:         fullPath,
				IsDir:        de.IsDir(),
				Depth:        depth,
				IsGitIgnored: matchesGitignore(relPath, gitignorePatterns, de.IsDir()),
			}
			if entry.IsDir {
				dirs = append(dirs, entry)
			} else {
				files = append(files, entry)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, readErr
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return append(dirs, files...), nil
}

// loadGitignore reads a top-level .gitignore and returns simple patterns.
func loadGitignore(rootDir string) []string {
	patterns, err := loadGitignoreContext(context.Background(), rootDir)
	if err != nil {
		return nil
	}
	return patterns
}

// loadGitignoreContext reads simple top-level patterns without letting an
// untrusted workspace file monopolize a refresh command. Invalid, oversized,
// or symlinked ignore files deliberately act as no patterns; cancellation is
// still returned to preserve the tree snapshot already visible to the user.
func loadGitignoreContext(ctx context.Context, rootDir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(rootDir, ".gitignore")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxGitignoreBytes {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Size() > maxGitignoreBytes {
		return nil, nil
	}

	var patterns []string
	scanner := bufio.NewScanner(io.LimitReader(file, maxGitignoreBytes+1))
	scanner.Buffer(make([]byte, 1024), maxGitignoreLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(patterns) >= maxGitignorePatterns {
			return nil, nil
		}
		patterns = append(patterns, line)
	}
	if scanner.Err() != nil {
		return nil, nil
	}
	return patterns, nil
}

// LoadGitignorePatterns reads the top-level .gitignore and returns simple patterns.
func LoadGitignorePatterns(rootDir string) []string {
	return loadGitignore(rootDir)
}

// LoadGitignorePatternsContext is the cancellable counterpart used by
// background workspace scans outside the tree model. It shares the same
// bounded, regular-file-only parsing policy as tree refreshes.
func LoadGitignorePatternsContext(ctx context.Context, rootDir string) ([]string, error) {
	return loadGitignoreContext(ctx, rootDir)
}

// matchesGitignore checks if a relative path matches any gitignore pattern.
func matchesGitignore(rel string, patterns []string, isDir bool) bool {
	for _, pat := range patterns {
		dirOnly := strings.HasSuffix(pat, "/")
		if dirOnly {
			if !isDir {
				continue
			}
			pat = strings.TrimSuffix(pat, "/")
		}

		// Match against basename
		base := filepath.Base(rel)
		if matched, _ := filepath.Match(pat, base); matched {
			return true
		}
		// Match against full relative path
		if matched, _ := filepath.Match(pat, rel); matched {
			return true
		}
		// Handle patterns like "dir/**"
		prefix := strings.TrimSuffix(pat, "/**")
		if prefix != pat {
			if strings.HasPrefix(rel, prefix+"/") || rel == prefix {
				return true
			}
		}
		// Handle directory prefix patterns like "bin"
		if !strings.Contains(pat, "*") && !strings.Contains(pat, "?") {
			if strings.HasPrefix(rel, pat+"/") || rel == pat {
				return true
			}
		}
	}
	return false
}

// MatchesGitignore reports whether a relative path matches any gitignore pattern.
func MatchesGitignore(rel string, patterns []string, isDir bool) bool {
	return matchesGitignore(rel, patterns, isDir)
}

// View renders the file tree.
func (m Model) View() string {
	// A resize can briefly report a zero-sized terminal. Do not let the
	// structural columns (indent and icon) escape the available viewport in
	// that state; an over-wide sidebar makes the whole TUI wrap horizontally.
	if m.Width <= 0 || m.Height <= 0 {
		return ""
	}

	flat := m.flatEntries()
	var sb strings.Builder

	for i := range m.Height {
		idx := m.ScrollY + i
		if i > 0 {
			sb.WriteByte('\n')
		}
		if idx < len(flat) {
			entry := flat[idx]
			isCursor := idx == m.Cursor
			icon, iconColor := iconForEntry(entry)

			// Determine background based on cursor state using cached colors
			var bg color.Color
			var baseStyle lipgloss.Style
			if isCursor {
				bg = m.cachedStyles.cursorBg
				baseStyle = m.theme.TreeCursor
			} else {
				bg = m.cachedStyles.entryBg
				baseStyle = m.theme.TreeEntry
			}

			// Set background on cached base style (minimal allocation vs NewStyle)
			cachedBase := m.cachedStyles.base.Background(bg)

			// Build plain text parts to calculate widths accurately
			indent := " " + strings.Repeat("  ", entry.Depth)
			iconWidth := ansi.StringWidth(icon)
			nameStr := entry.Name

			// Git status indicator + color for the filename
			var gitNameColor color.Color
			var gitIndicator string // e.g. "M", "A", "D", "U"
			if m.gitStatus != nil && !entry.IsDir {
				// Build relative path from root
				relPath := entry.Path
				if m.Root != "" && strings.HasPrefix(entry.Path, m.Root) {
					relPath = strings.TrimPrefix(entry.Path[len(m.Root):], "/")
					relPath = strings.TrimPrefix(relPath, string(filepath.Separator))
				}
				if st, ok := m.gitStatus[relPath]; ok {
					gitIndicator = st
					switch st {
					case "M":
						gitNameColor = ui.Nord13 // yellow
					case "A":
						gitNameColor = ui.Nord14 // green
					case "D":
						gitNameColor = ui.Nord11 // red
					case "U":
						gitNameColor = ui.Nord14 // green for untracked
					}
				}
			}

			// Git status indicator width
			hasGitInd := gitIndicator != ""
			gitIndWidth := 0
			if hasGitInd {
				gitIndWidth = 2 // " M"
			}

			// Diagnostic dot
			hasDiag := false
			var diagColor color.Color
			if m.diagnostics != nil {
				if sev, ok := m.diagnostics[entry.Path]; ok && sev > 0 {
					hasDiag = true
					diagColor = ui.Nord13
					if sev == 1 {
						diagColor = ui.Nord11
					}
				}
			}

			// Truncate name if needed
			maxNameWidth := m.Width - (ansi.StringWidth(indent) + iconWidth + 1)
			if hasGitInd {
				maxNameWidth -= gitIndWidth
			}
			if hasDiag {
				maxNameWidth -= 2
			}
			if maxNameWidth <= 0 {
				nameStr = ""
			} else if ansi.StringWidth(nameStr) > maxNameWidth {
				nameStr = ansi.Truncate(nameStr, maxNameWidth, "")
			}

			// Render parts with consistent background using cached style
			styledIcon := cachedBase.Foreground(iconColor).Render(icon)
			nameFg := baseStyle.GetForeground()
			if gitNameColor != nil {
				nameFg = gitNameColor
			}
			// Dim gitignored entries
			if entry.IsGitIgnored {
				nameFg = m.cachedStyles.gitIgnoredColor // dim gray
			}
			styledName := cachedBase.Foreground(nameFg).Render(nameStr)
			if entry.IsGitIgnored {
				styledIcon = cachedBase.Foreground(m.cachedStyles.gitIgnoredColor).Render(icon)
			}

			// Git status indicator part
			var gitIndPart string
			if hasGitInd {
				gitIndPart = cachedBase.Foreground(gitNameColor).Render(" " + gitIndicator)
			}

			var diagPart string
			if hasDiag {
				diagPart = cachedBase.Foreground(diagColor).Render(" ●")
			}

			// Calculate padding needed
			contentWidth := ansi.StringWidth(indent) + iconWidth + 1 + ansi.StringWidth(nameStr)
			if hasGitInd {
				contentWidth += gitIndWidth
			}
			if hasDiag {
				contentWidth += 2
			}
			padWidth := m.Width - contentWidth
			if padWidth < 0 {
				padWidth = 0
			}
			padding := strings.Repeat(" ", padWidth)

			// Assemble: indent + icon + space + name + diag + padding
			// Render indent and padding with background too using cached style
			indentStyled := cachedBase.Render(indent)
			spaceStyled := cachedBase.Render(" ")
			padStyled := cachedBase.Render(padding)

			line := indentStyled + styledIcon + spaceStyled + styledName + gitIndPart + diagPart + padStyled
			// Name truncation above only reserves room for the name. At very
			// narrow widths the fixed indent/icon columns can still exceed the
			// viewport, so clip the fully styled line by display cells as the
			// final invariant. ansi.Truncate preserves escape sequences and does
			// not split a grapheme cluster.
			sb.WriteString(ansi.Truncate(line, m.Width, ""))
		} else {
			// Use entry background for empty lines
			emptyLine := m.cachedStyles.base.Background(m.cachedStyles.entryBg).
				Render(strings.Repeat(" ", m.Width))
			sb.WriteString(emptyLine)
		}
	}

	return sb.String()
}

// SetSize updates the tree dimensions.
func (m *Model) SetSize(width, height int) {
	m.Width = max(0, width)
	m.Height = max(0, height)
}
