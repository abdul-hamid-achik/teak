package app

import (
	"teak/internal/ui"
)

// LayoutManager handles window dimensions, theme, focus, and global state.
type LayoutManager struct {
	width  int
	height int
	theme  ui.Theme
	focus  FocusArea
	status string

	// File system watching
	watcher          *fileWatcher
	cachedFiles      []string
	cachedFilesReady bool

	// Diagnostics (affects multiple panels)
	fileDiagnostics map[string]int
	dirDiagnostics  map[string]int
}

// NewLayoutManager creates a new layout manager.
func NewLayoutManager(theme ui.Theme) *LayoutManager {
	return &LayoutManager{
		theme:           theme,
		width:           80,
		height:          24,
		focus:           FocusEditor,
		fileDiagnostics: make(map[string]int),
		dirDiagnostics:  make(map[string]int),
	}
}

// GetWidth returns the window width.
func (lm *LayoutManager) GetWidth() int {
	return lm.width
}

// GetHeight returns the window height.
func (lm *LayoutManager) GetHeight() int {
	return lm.height
}

// GetSize returns the window dimensions.
func (lm *LayoutManager) GetSize() (int, int) {
	return lm.width, lm.height
}

// SetSize sets the window dimensions.
func (lm *LayoutManager) SetSize(width, height int) {
	lm.width = width
	lm.height = height
}

// GetTheme returns the current theme.
func (lm *LayoutManager) GetTheme() ui.Theme {
	return lm.theme
}

// SetTheme sets the theme.
func (lm *LayoutManager) SetTheme(theme ui.Theme) {
	lm.theme = theme
}

// GetFocus returns the current focus area.
func (lm *LayoutManager) GetFocus() FocusArea {
	return lm.focus
}

// SetFocus sets the focus area.
func (lm *LayoutManager) SetFocus(focus FocusArea) {
	lm.focus = focus
}

// IsFocusEditor returns whether focus is in editor.
func (lm *LayoutManager) IsFocusEditor() bool {
	return lm.focus == FocusEditor
}

// IsFocusTree returns whether focus is in tree.
func (lm *LayoutManager) IsFocusTree() bool {
	return lm.focus == FocusTree
}

// IsFocusGitPanel returns whether focus is in git panel.
func (lm *LayoutManager) IsFocusGitPanel() bool {
	return lm.focus == FocusGitPanel
}

// IsFocusProblems returns whether focus is in problems panel.
func (lm *LayoutManager) IsFocusProblems() bool {
	return lm.focus == FocusProblems
}

// IsFocusDebugger returns whether focus is in debugger panel.
func (lm *LayoutManager) IsFocusDebugger() bool {
	return lm.focus == FocusDebugger
}

// IsFocusAgent returns whether focus is in agent panel.
func (lm *LayoutManager) IsFocusAgent() bool {
	return lm.focus == FocusAgent
}

// GetStatus returns the status message.
func (lm *LayoutManager) GetStatus() string {
	return lm.status
}

// SetStatus sets the status message.
func (lm *LayoutManager) SetStatus(status string) {
	lm.status = status
}

// ClearStatus clears the status message.
func (lm *LayoutManager) ClearStatus() {
	lm.status = ""
}

// ============== Diagnostics ==============

// UpdateDiagnostics updates the diagnostic severity for a file.
func (lm *LayoutManager) UpdateDiagnostics(path string, severity int) {
	if severity > 0 {
		lm.fileDiagnostics[path] = severity
	} else {
		delete(lm.fileDiagnostics, path)
	}
}

// GetDiagnostic returns the worst diagnostic severity for a file.
func (lm *LayoutManager) GetDiagnostic(path string) int {
	if sev, ok := lm.fileDiagnostics[path]; ok {
		return sev
	}
	return 0
}

// HasDiagnostic returns whether a file has any diagnostic.
func (lm *LayoutManager) HasDiagnostic(path string) bool {
	_, ok := lm.fileDiagnostics[path]
	return ok
}

// ClearDiagnostics clears all diagnostics.
func (lm *LayoutManager) ClearDiagnostics() {
	lm.fileDiagnostics = make(map[string]int)
	lm.dirDiagnostics = make(map[string]int)
}

// GetAllDiagnostics returns all file diagnostics.
func (lm *LayoutManager) GetAllDiagnostics() map[string]int {
	return lm.fileDiagnostics
}

// UpdateDirDiagnostics updates diagnostics for a directory.
func (lm *LayoutManager) UpdateDirDiagnostics(path string, severity int) {
	if severity > 0 {
		lm.dirDiagnostics[path] = severity
	} else {
		delete(lm.dirDiagnostics, path)
	}
}

// GetDirDiagnostic returns the worst diagnostic severity for a directory.
func (lm *LayoutManager) GetDirDiagnostic(path string) int {
	if sev, ok := lm.dirDiagnostics[path]; ok {
		return sev
	}
	return 0
}

// ============== File Watcher ==============

// StartFileWatcher starts watching the given directory for changes.
func (lm *LayoutManager) StartFileWatcher(rootDir string) error {
	watcher, err := newFileWatcher(rootDir)
	if err != nil {
		return err
	}
	lm.watcher = watcher
	return nil
}

// StopFileWatcher stops the file watcher.
func (lm *LayoutManager) StopFileWatcher() {
	if lm.watcher != nil {
		lm.watcher.Close()
		lm.watcher = nil
	}
}

// HasFileWatcher returns whether a file watcher is active.
func (lm *LayoutManager) HasFileWatcher() bool {
	return lm.watcher != nil
}

// GetFileWatcher returns the file watcher.
func (lm *LayoutManager) GetFileWatcher() *fileWatcher {
	return lm.watcher
}

// ============== File Cache ==============

// SetCachedFiles sets the cached file list.
func (lm *LayoutManager) SetCachedFiles(files []string) {
	lm.cachedFiles = files
	lm.cachedFilesReady = true
}

// GetCachedFiles returns the cached file list.
func (lm *LayoutManager) GetCachedFiles() []string {
	return lm.cachedFiles
}

// AreCachedFilesReady returns whether the file cache is ready.
func (lm *LayoutManager) AreCachedFilesReady() bool {
	return lm.cachedFilesReady
}

// RefreshFileCache marks the file cache as needing refresh.
func (lm *LayoutManager) RefreshFileCache() {
	lm.cachedFilesReady = false
}

// ============== Layout Calculations ==============

// GetSidebarWidth calculates the sidebar width based on visibility.
func (lm *LayoutManager) GetSidebarWidth(showTree bool) int {
	if !showTree {
		return 0
	}
	// Sidebar takes 1/4 of width, min 30, max 50
	width := lm.width / 4
	if width < 30 {
		width = 30
	}
	if width > 50 {
		width = 50
	}
	return width
}

// GetEditorWidth calculates the editor width.
func (lm *LayoutManager) GetEditorWidth(showTree bool) int {
	sidebarWidth := lm.GetSidebarWidth(showTree)
	return lm.width - sidebarWidth
}

// CalculateDimensions returns sidebar and editor widths.
func (lm *LayoutManager) CalculateDimensions(showTree bool) (sidebarWidth, editorWidth int) {
	sidebarWidth = lm.GetSidebarWidth(showTree)
	editorWidth = lm.width - sidebarWidth
	return
}

// IsSmallScreen returns whether the screen is small.
func (lm *LayoutManager) IsSmallScreen() bool {
	return lm.width < 100 || lm.height < 30
}

// IsWideScreen returns whether the screen is wide.
func (lm *LayoutManager) IsWideScreen() bool {
	return lm.width >= 150
}
