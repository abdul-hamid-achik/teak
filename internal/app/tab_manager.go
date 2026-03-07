package app

import (
	"fmt"
	"path/filepath"

	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/text"
	"teak/internal/ui"
)

// TabManager handles all tab-related state and operations.
type TabManager struct {
	editors         []editor.Editor
	activeTab       int
	tabBar          editor.TabBar
	welcome         *editor.Welcome
	diffViews       map[int]diff.Model
	closedTabs      []ClosedTab
	pendingCloseTab int
	untitledCounter int
}

// NewTabManager creates a new tab manager.
func NewTabManager(theme ui.Theme) *TabManager {
	return &TabManager{
		tabBar:          editor.NewTabBar(theme),
		diffViews:       make(map[int]diff.Model),
		pendingCloseTab: -1,
	}
}

// EditorCount returns the number of open editors.
func (tm *TabManager) EditorCount() int {
	return len(tm.editors)
}

// GetEditor returns the editor at the given index.
func (tm *TabManager) GetEditor(idx int) (*editor.Editor, bool) {
	if idx < 0 || idx >= len(tm.editors) {
		return nil, false
	}
	return &tm.editors[idx], true
}

// GetActiveEditor returns the currently active editor.
func (tm *TabManager) GetActiveEditor() *editor.Editor {
	if tm.activeTab < 0 || tm.activeTab >= len(tm.editors) {
		return nil
	}
	return &tm.editors[tm.activeTab]
}

// GetActiveTab returns the active tab index.
func (tm *TabManager) GetActiveTab() int {
	return tm.activeTab
}

// SetActiveTab sets the active tab index.
func (tm *TabManager) SetActiveTab(idx int) {
	if idx >= 0 && idx < len(tm.editors) {
		tm.activeTab = idx
		tm.tabBar.ActiveIdx = idx
	}
}

// AddEditor adds a new editor and returns its index.
func (tm *TabManager) AddEditor(ed editor.Editor, label string, filePath string) int {
	tm.editors = append(tm.editors, ed)
	idx := len(tm.editors) - 1
	tm.tabBar.AddTab(label, filePath)
	return idx
}

// AddDiffView adds a diff view as a tab.
func (tm *TabManager) AddDiffView(diffView diff.Model, label string, filePath string) int {
	// Create a placeholder editor for the diff view
	buf := text.NewBuffer()
	cfg := editor.DefaultConfig()
	ed := editor.New(buf, ui.DefaultTheme(), cfg)

	idx := tm.AddEditor(ed, label, filePath)
	tm.diffViews[idx] = diffView
	return idx
}

// CloseTab closes the tab at the given index.
func (tm *TabManager) CloseTab(idx int) (*ClosedTab, error) {
	if idx < 0 || idx >= len(tm.editors) {
		return nil, fmt.Errorf("invalid tab index: %d", idx)
	}

	// Save tab info for potential reopen
	ed := tm.editors[idx]
	var closedTab *ClosedTab
	if ed.Buffer.FilePath != "" {
		// Only track file-backed tabs, not untitled or diff views
		if _, isDiff := tm.diffViews[idx]; !isDiff {
			closedTab = &ClosedTab{
				FilePath: ed.Buffer.FilePath,
				Label:    filepath.Base(ed.Buffer.FilePath),
			}
		}
	}

	// Remove from diff views if present
	delete(tm.diffViews, idx)

	// Remove from editors
	tm.editors = append(tm.editors[:idx], tm.editors[idx+1:]...)
	tm.tabBar.RemoveTab(idx)

	// Adjust active tab
	if tm.activeTab >= len(tm.editors) {
		tm.activeTab = len(tm.editors) - 1
	}
	if tm.activeTab < 0 {
		tm.activeTab = 0
	}

	// Reindex diff views
	newDiffViews := make(map[int]diff.Model)
	for oldIdx, dv := range tm.diffViews {
		if oldIdx > idx {
			newDiffViews[oldIdx-1] = dv
		} else {
			newDiffViews[oldIdx] = dv
		}
	}
	tm.diffViews = newDiffViews

	return closedTab, nil
}

// CloseActiveTab closes the currently active tab.
func (tm *TabManager) CloseActiveTab() (*ClosedTab, error) {
	return tm.CloseTab(tm.activeTab)
}

// ReopenLastTab attempts to reopen the most recently closed tab.
func (tm *TabManager) ReopenLastTab() (*ClosedTab, bool) {
	if len(tm.closedTabs) == 0 {
		return nil, false
	}

	// Get last closed tab
	lastIdx := len(tm.closedTabs) - 1
	ct := tm.closedTabs[lastIdx]
	tm.closedTabs = tm.closedTabs[:lastIdx]

	return &ct, true
}

// AddClosedTab adds a tab to the closed tabs history.
func (tm *TabManager) AddClosedTab(ct ClosedTab) {
	tm.closedTabs = append(tm.closedTabs, ct)
	// Limit history to 10
	if len(tm.closedTabs) > 10 {
		tm.closedTabs = tm.closedTabs[len(tm.closedTabs)-10:]
	}
}

// IsDiffView returns true if the tab at idx is a diff view.
func (tm *TabManager) IsDiffView(idx int) bool {
	_, ok := tm.diffViews[idx]
	return ok
}

// GetDiffView returns the diff view at the given index.
func (tm *TabManager) GetDiffView(idx int) (diff.Model, bool) {
	dv, ok := tm.diffViews[idx]
	return dv, ok
}

// GetTabBar returns the tab bar model.
func (tm *TabManager) GetTabBar() *editor.TabBar {
	return &tm.tabBar
}

// GetWelcome returns the welcome screen model.
func (tm *TabManager) GetWelcome() *editor.Welcome {
	return tm.welcome
}

// SetWelcome sets the welcome screen model.
func (tm *TabManager) SetWelcome(w *editor.Welcome) {
	tm.welcome = w
}

// HasWelcome returns true if welcome screen is active.
func (tm *TabManager) HasWelcome() bool {
	return tm.welcome != nil
}

// DismissWelcome removes the welcome screen.
func (tm *TabManager) DismissWelcome() {
	tm.welcome = nil
}

// NextTab switches to the next tab.
func (tm *TabManager) NextTab() {
	if len(tm.editors) > 0 {
		tm.activeTab = (tm.activeTab + 1) % len(tm.editors)
		tm.tabBar.ActiveIdx = tm.activeTab
	}
}

// PrevTab switches to the previous tab.
func (tm *TabManager) PrevTab() {
	if len(tm.editors) > 0 {
		tm.activeTab--
		if tm.activeTab < 0 {
			tm.activeTab = len(tm.editors) - 1
		}
		tm.tabBar.ActiveIdx = tm.activeTab
	}
}

// GetUntitledCounter returns and increments the untitled counter.
func (tm *TabManager) GetUntitledCounter() int {
	tm.untitledCounter++
	return tm.untitledCounter
}

// SetPendingCloseTab sets the tab index pending close confirmation.
func (tm *TabManager) SetPendingCloseTab(idx int) {
	tm.pendingCloseTab = idx
}

// GetPendingCloseTab returns the tab index pending close confirmation.
func (tm *TabManager) GetPendingCloseTab() int {
	return tm.pendingCloseTab
}

// ClearPendingCloseTab clears the pending close tab.
func (tm *TabManager) ClearPendingCloseTab() {
	tm.pendingCloseTab = -1
}

// UpdateTabBar updates the tab bar model.
func (tm *TabManager) UpdateTabBar(msg interface{}) {
	// TabBar update logic here
}

// Resize updates dimensions for all tabs.
func (tm *TabManager) Resize(width, height int) {
	for i := range tm.editors {
		tm.editors[i].SetSize(width, height)
	}
	if tm.welcome != nil {
		tm.welcome.SetSize(width, height)
	}
}

// SaveAll saves all open editors.
func (tm *TabManager) SaveAll() []error {
	var errors []error
	for i := range tm.editors {
		if err := tm.editors[i].Buffer.Save(); err != nil {
			errors = append(errors, fmt.Errorf("tab %d: %w", i, err))
		}
	}
	return errors
}

// AnyDirty returns true if any editor has unsaved changes.
func (tm *TabManager) AnyDirty() bool {
	for _, ed := range tm.editors {
		if ed.Buffer.Dirty() {
			return true
		}
	}
	return false
}

// GetTabState returns state for session saving.
func (tm *TabManager) GetTabState() ([]TabState, int) {
	var tabs []TabState
	activeIdx := tm.activeTab

	for i, ed := range tm.editors {
		// Skip diff views and untitled tabs
		if _, isDiff := tm.diffViews[i]; isDiff {
			continue
		}
		if ed.Buffer.FilePath == "" {
			continue
		}

		tabs = append(tabs, TabState{
			FilePath:   ed.Buffer.FilePath,
			CursorLine: ed.Buffer.Cursor.Line,
			CursorCol:  ed.Buffer.Cursor.Col,
			Pinned:     !tm.tabBar.Tabs[i].Preview,
		})
	}

	return tabs, activeIdx
}

// TabState represents a tab for session saving.
type TabState struct {
	FilePath   string
	CursorLine int
	CursorCol  int
	Pinned     bool
}
