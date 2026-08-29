package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/problems"
	"teak/internal/search"
)

// --- F3: opening a search result must keep the results for F3/Shift+F3 ---

// runProjectSearchAndOpenResult drives the real overlay path: results arrive
// via SearchResultsMsg, the cursor moves down `moveDown` times, and Enter opens
// the selected result. It returns the model after the open-result message was
// processed.
func runProjectSearchAndOpenResult(t *testing.T, m Model, results []search.Result, moveDown int) Model {
	t.Helper()
	m.width = 100
	m.height = 30
	openedAny, _ := m.openSearch(search.ModeText)
	m = openedAny.(Model)

	resultsAny, _ := m.Update(search.SearchResultsMsg{Results: results})
	m = resultsAny.(Model)

	key := tea.KeyPressMsg{Code: tea.KeyDown}
	for range moveDown {
		downAny, _ := m.Update(key)
		m = downAny.(Model)
	}
	enterAny, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = enterAny.(Model)
	if cmd == nil {
		t.Fatal("enter on a search result did not produce an open-result command")
	}
	msg, ok := cmd().(search.OpenResultMsg)
	if !ok {
		t.Fatalf("enter command = %T, want search.OpenResultMsg", cmd())
	}
	openResultAny, _ := m.Update(msg)
	return openResultAny.(Model)
}

func TestSearchOpenResultKeepsResultsForFindNext(t *testing.T) {
	root := t.TempDir()
	m := newSaveFlowModel(t, config.DefaultConfig(), root)
	addDirtyEditor(t, &m, "a.txt", "alpha\nbeta\ngamma\n", "alpha\nbeta\ngamma\n")
	results := []search.Result{
		{FilePath: "a.txt", Line: 0, Col: 0},
		{FilePath: "a.txt", Line: 2, Col: 0},
	}

	opened := runProjectSearchAndOpenResult(t, m, results, 0)
	if opened.showSearch {
		t.Fatal("opening a result left the search overlay visible")
	}
	if got := opened.activeEditor().Buffer.Cursor; got.Line != 0 || got.Col != 0 {
		t.Fatalf("cursor after open = %+v, want line 0 col 0", got)
	}

	nextAny, _ := opened.Update(tea.KeyPressMsg{Code: tea.KeyF3})
	next := nextAny.(Model)
	if strings.Contains(next.status, "No search results") {
		t.Fatalf("F3 after opening a result reported %q", next.status)
	}
	if got := next.activeEditor().Buffer.Cursor.Line; got != 2 {
		t.Fatalf("cursor line after F3 = %d, want 2 (the next result)", got)
	}
	if !strings.Contains(next.status, "Match 2/2") {
		t.Fatalf("status = %q, want match counter", next.status)
	}
}

func TestSearchOpenResultContinuesFromOpenedIndex(t *testing.T) {
	root := t.TempDir()
	m := newSaveFlowModel(t, config.DefaultConfig(), root)
	addDirtyEditor(t, &m, "a.txt", "alpha\nbeta\ngamma\n", "alpha\nbeta\ngamma\n")
	results := []search.Result{
		{FilePath: "a.txt", Line: 0, Col: 0},
		{FilePath: "a.txt", Line: 2, Col: 0},
	}

	// Open the second result; F3 must wrap back to the first, not restart
	// from index 0's successor.
	opened := runProjectSearchAndOpenResult(t, m, results, 1)
	if got := opened.activeEditor().Buffer.Cursor; got.Line != 2 {
		t.Fatalf("cursor after open = %+v, want line 2", got)
	}

	nextAny, _ := opened.Update(tea.KeyPressMsg{Code: tea.KeyF3})
	next := nextAny.(Model)
	if strings.Contains(next.status, "No search results") {
		t.Fatalf("F3 after opening a result reported %q", next.status)
	}
	if got := next.activeEditor().Buffer.Cursor.Line; got != 0 {
		t.Fatalf("cursor line after F3 = %d, want 0 (wrapped to the first result)", got)
	}
	if !strings.Contains(next.status, "Match 1/2 (wrapped)") {
		t.Fatalf("status = %q, want wrapped match counter", next.status)
	}
}

// --- F12: F8/Shift+F8 must not be silent when there are no problems ---

func TestProblemNavigationKeysReportWhenPanelEmpty(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "f8", key: tea.KeyPressMsg{Code: tea.KeyF8}},
		{name: "shift+f8", key: tea.KeyPressMsg{Code: tea.KeyF8, Mod: tea.ModShift}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newInputRoutingTestModel(t)
			if got := m.problemsPanel.ProblemCount(); got != 0 {
				t.Fatalf("precondition ProblemCount() = %d, want 0", got)
			}

			updated := updateInputRoutingModel(t, m, tt.key)
			if updated.status != "No problems" {
				t.Fatalf("status = %q, want %q", updated.status, "No problems")
			}
		})
	}
}

func TestProblemNavigationWithProblemsStillNavigates(t *testing.T) {
	root := t.TempDir()
	m := newSaveFlowModel(t, config.DefaultConfig(), root)
	path := filepath.Join(root, "main.go")
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	m.problemsPanel.SetProblems([]problems.Problem{
		{FilePath: path, Line: 0, Col: 1, Severity: 1, Message: "first"},
		{FilePath: path, Line: 1, Col: 1, Severity: 1, Message: "second"},
	})

	updated := updateInputRoutingModel(t, m, tea.KeyPressMsg{Code: tea.KeyF8})
	if got := updated.problemsPanel.SelectedIndex(); got != 1 {
		t.Fatalf("selected index after F8 = %d, want 1", got)
	}
	if !strings.Contains(updated.status, "Problem 2/2") {
		t.Fatalf("status = %q, want problem counter", updated.status)
	}
	if strings.Contains(updated.status, "No problems") {
		t.Fatalf("status = %q must not claim there are no problems", updated.status)
	}
}

// --- F10: command palette Find entries must match the real bindings ---

func TestCommandPaletteFindEntriesMatchRealBindings(t *testing.T) {
	// Model copies alias the same state, so each key-binding probe needs its
	// own freshly built model.
	newFindModel := func(t *testing.T) Model {
		t.Helper()
		m := newInputRoutingTestModel(t)
		addDirtyEditor(t, &m, "a.txt", "content\n", "content\n")
		return m
	}

	m := newFindModel(t)
	registry := m.commandRegistry()
	byID := make(map[string]Command, len(registry))
	for _, cmd := range registry {
		byID[cmd.ID] = cmd
	}

	project, ok := byID["find"]
	if !ok {
		t.Fatal("palette is missing the project find entry (id \"find\")")
	}
	if project.Label != "Find in Project" {
		t.Errorf("project find label = %q, want %q", project.Label, "Find in Project")
	}
	if project.Shortcut != "Ctrl+Shift+F" {
		t.Errorf("project find shortcut = %q, want %q (the binding that opens project search)", project.Shortcut, "Ctrl+Shift+F")
	}
	executed, ok := project.Execute().(commandPaletteMsg)
	if !ok {
		t.Fatalf("project find executes %T, want commandPaletteMsg", project.Execute())
	}
	inner, ok := executed.inner.(openSearchMsg)
	if !ok || inner.mode != search.ModeText {
		t.Fatalf("project find opens %#v, want openSearchMsg{mode: ModeText}", executed.inner)
	}

	inFile, ok := byID["find_in_file"]
	if !ok {
		t.Fatal("palette is missing the in-file find entry (id \"find_in_file\")")
	}
	if inFile.Label != "Find in File" {
		t.Errorf("in-file find label = %q, want %q", inFile.Label, "Find in File")
	}
	if inFile.Shortcut != "Ctrl+F" {
		t.Errorf("in-file find shortcut = %q, want %q", inFile.Shortcut, "Ctrl+F")
	}
	if _, ok := inFile.Execute().(commandPaletteMsg); !ok {
		t.Fatalf("in-file find executes %T, want commandPaletteMsg", inFile.Execute())
	}

	// The labelled shortcuts must be the ones the key router actually uses.
	projectAny, _ := newFindModel(t).Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl | tea.ModShift})
	projectModel := projectAny.(Model)
	if !projectModel.showSearch || projectModel.searchMode != search.ModeText {
		t.Fatal("Ctrl+Shift+F does not open project text search; the palette label would be wrong")
	}

	fileAny, _ := newFindModel(t).Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	fileModel := fileAny.(Model)
	if ed := fileModel.activeEditor(); ed == nil || !ed.IsFindVisible() {
		t.Fatal("Ctrl+F does not open the in-buffer find widget; the palette label would be wrong")
	}

	// Dispatching the in-file palette entry must match the Ctrl+F behavior.
	paletteAny, _ := newFindModel(t).Update(inFile.Execute())
	paletteModel := paletteAny.(Model)
	if ed := paletteModel.activeEditor(); ed == nil || !ed.IsFindVisible() {
		t.Fatal("Find in File palette entry did not open the in-buffer find widget")
	}
}

// --- F2-label: replace completion must name the file it actually touched ---

func TestSearchReplaceStatusNamesActiveFile(t *testing.T) {
	tests := []struct {
		name string
		msg  any
		want string
	}{
		{name: "replace one", msg: search.ReplaceOneMsg{Query: "old", Replacement: "new"}, want: "1 match in replace.txt"},
		{name: "replace all", msg: search.ReplaceAllMsg{Query: "old", Replacement: "new"}, want: "2 matches in replace.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
			addDirtyEditor(t, &model, "replace.txt", "old old\n", "old old\n")

			pendingAny, cmd := model.Update(tt.msg)
			pending := pendingAny.(Model)
			if cmd == nil {
				t.Fatal("replace did not schedule background preparation")
			}
			completedAny, _ := pending.Update(cmd())
			completed := completedAny.(Model)

			if !strings.Contains(completed.status, tt.want) {
				t.Fatalf("status = %q, want it to contain %q", completed.status, tt.want)
			}
		})
	}
}

// --- F8-tabbar: middle-click closes safely, right-click is inert ---

func tabLabelClickPoint(t *testing.T, m Model, idx int) (int, int) {
	t.Helper()
	if idx < 0 || idx >= len(m.tabBar.Tabs) {
		t.Fatalf("tab index %d out of range (%d tabs)", idx, len(m.tabBar.Tabs))
	}
	// Rendering the view registers the tab zones with the zone manager, exactly
	// as a real frame does.
	_ = m.View()
	info := zone.Get(editor.TabZoneID(m.tabBar.Tabs[idx]))
	if info.IsZero() {
		t.Fatalf("tab %d label zone was not rendered", idx)
	}
	return info.StartX, info.StartY
}

func TestTabBarMiddleClickClosesTabSafely(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "package second\n", "package second\n")
	addDirtyEditor(t, &m, "third.go", "package third\n", "package third\n")
	m.relayout()
	x, y := tabLabelClickPoint(t, m, 2)

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseMiddle, X: x, Y: y}))
	updated := updatedAny.(Model)
	if got := len(updated.editors); got != 2 {
		t.Fatalf("editors after middle-click = %d, want 2 (tab 2 closed)", got)
	}
	if got := len(updated.tabBar.Tabs); got != 2 {
		t.Fatalf("tabs after middle-click = %d, want 2", got)
	}
	if updated.unsavedConfirm != nil {
		t.Fatal("middle-click on a clean tab unexpectedly asked for confirmation")
	}
}

func TestTabBarMiddleClickOnDirtyTabConfirmsInsteadOfClosing(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "clean\n", "edited\n")
	addDirtyEditor(t, &m, "third.go", "package third\n", "package third\n")
	m.relayout()
	x, y := tabLabelClickPoint(t, m, 1)

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseMiddle, X: x, Y: y}))
	updated := updatedAny.(Model)
	if got := len(updated.editors); got != 3 {
		t.Fatalf("editors after middle-click on dirty tab = %d, want 3 (close must be confirmed)", got)
	}
	if updated.unsavedConfirm == nil {
		t.Fatal("middle-click on a dirty tab force-closed instead of confirming")
	}
	if updated.pendingCloseTab != 1 {
		t.Fatalf("pendingCloseTab = %d, want 1", updated.pendingCloseTab)
	}
}

func TestTabBarRightClickDoesNotActivateTab(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "package second\n", "package second\n")
	addDirtyEditor(t, &m, "third.go", "package third\n", "package third\n")
	m.activateTab(0)
	m.relayout()
	x, y := tabLabelClickPoint(t, m, 2)

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: x, Y: y}))
	updated := updatedAny.(Model)
	if updated.activeTab != 0 || updated.tabBar.ActiveIdx != 0 {
		t.Fatalf("active tab after right-click = %d/%d, want unchanged 0", updated.activeTab, updated.tabBar.ActiveIdx)
	}
	if got := len(updated.editors); got != 3 {
		t.Fatalf("editors after right-click = %d, want 3 (right-click must not close)", got)
	}
}

func TestTabBarLeftClickStillActivatesTab(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "package second\n", "package second\n")
	addDirtyEditor(t, &m, "third.go", "package third\n", "package third\n")
	m.activateTab(0)
	m.relayout()
	x, y := tabLabelClickPoint(t, m, 2)

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y}))
	updated := updatedAny.(Model)
	if updated.activeTab != 2 || updated.tabBar.ActiveIdx != 2 {
		t.Fatalf("active tab after left-click = %d/%d, want 2", updated.activeTab, updated.tabBar.ActiveIdx)
	}
}

func TestTabBarRightClickOnCloseZoneDoesNotClose(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "second.go", "package second\n", "package second\n")
	m.relayout()
	_ = m.View()
	info := zone.Get(editor.TabCloseZoneID(m.tabBar.Tabs[1]))
	if info.IsZero() {
		t.Fatal("tab 1 close zone was not rendered")
	}

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: info.StartX, Y: info.StartY}))
	updated := updatedAny.(Model)
	if got := len(updated.editors); got != 2 {
		t.Fatalf("editors after right-click on close zone = %d, want 2 (only left-click closes)", got)
	}
}
