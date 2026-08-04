package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

func TestModelIgnoresStaleSearchResults(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.debounceGen = 2
	m.results = []Result{{FilePath: "current.go"}}
	m.searching = true

	updated, _ := m.Update(SearchResultsMsg{
		Generation: 1,
		Results:    []Result{{FilePath: "stale.go"}},
	})

	if got := updated.Results(); len(got) != 1 || got[0].FilePath != "current.go" {
		t.Fatalf("stale result replaced current results: %#v", got)
	}
	if !updated.searching {
		t.Fatal("stale result cleared active search state")
	}
}

func TestModelDispatchesCurrentDebounceTick(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.input.SetValue("needle")
	m.lastQuery = "needle"
	m.debounceGen = 3

	_, cmd := m.Update(DebounceTickMsg{Generation: 3})
	if cmd == nil {
		t.Fatal("current debounce tick did not dispatch a search")
	}

	_, cmd = m.Update(DebounceTickMsg{Generation: 2})
	if cmd != nil {
		t.Fatal("stale debounce tick dispatched a search")
	}
}

func TestModelSearchResultGenerationDoesNotRequireUIInput(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.debounceGen = 0

	updated, _ := m.Update(SearchResultsMsg{Generation: 0, Results: []Result{{FilePath: "result.go"}}})
	if got := updated.Results(); len(got) != 1 || got[0].FilePath != "result.go" {
		t.Fatalf("current result was not applied: %#v", got)
	}

	// Keep tea imported in the test package's API surface: the model must still
	// accept normal Bubble Tea messages after search result handling.
	_, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyDown})
}

func TestModelViewRendersReplaceStatusSymbolsAndScrollHint(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.SetSize(100, 40)
	m.input.SetValue("needle")
	m.replaceInput.SetValue("replacement")
	m.showReplace = true
	m.indexing = true
	m.errMsg = "fixture error"
	m.results = make([]Result, 25)
	for index := range m.results {
		m.results[index] = Result{
			FilePath:   "very/long/project/path/file.go",
			Line:       index,
			Preview:    "a matching preview",
			SymbolName: "LongSymbolName",
		}
	}
	m.cursor = 4
	view := m.View()
	for _, expected := range []string{"Search", "Semantic", "Replace", "All", "Indexing project...", "fixture error", "LongSymbolName", "results"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("View() = %q, want substring %q", view, expected)
		}
	}
	if x, y := m.OverlayOrigin(120, 50); x < 0 || y < 0 {
		t.Fatalf("OverlayOrigin() = (%d, %d), want non-negative coordinates", x, y)
	}
}

func TestModelViewEmptyQueryAndTinyHeightStayRenderable(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()
	m := New(ui.DefaultTheme(), t.TempDir(), ModeSemantic)
	m.SetSize(10, 1)
	view := m.View()
	if view == "" || !strings.Contains(view, "Search") {
		t.Fatalf("tiny empty View() = %q, want rendered search overlay", view)
	}
	if got := m.maxVisibleResults(); got < 3 {
		t.Fatalf("maxVisibleResults() = %d, want lower bound 3", got)
	}
}

func TestModelKeyboardAndMouseContracts(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	m.SetSize(100, 40)
	m.SetShowReplace(true)
	if !m.showReplace || m.Query() != "" || m.Replacement() != "" {
		t.Fatalf("initial search accessors = showReplace:%t query:%q replacement:%q", m.showReplace, m.Query(), m.Replacement())
	}
	m.SetShowReplace(false)
	if cmd := m.Focus(); cmd == nil {
		t.Fatal("Focus() returned nil blink command")
	}
	m.input.SetValue("needle")
	m.replaceInput.SetValue("replacement")
	m.results = []Result{{FilePath: "main.go", Line: 2, Col: 4, Preview: "needle"}, {FilePath: "other.go", Line: 4, Col: 1, Preview: "needle"}}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if updated.cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", updated.cursor)
	}
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if updated.cursor != 0 {
		t.Fatalf("up cursor = %d, want 0", updated.cursor)
	}
	updated, cmd := updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a result did not create an open command")
	}
	if opened, ok := cmd().(OpenResultMsg); !ok || opened.FilePath != "main.go" || opened.Line != 2 || opened.Col != 4 {
		t.Fatalf("open result = %#v, want main.go:2:4", cmd())
	}

	updated.showReplace = true
	updated.focusedInput = 0
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if updated.focusedInput != 1 || cmd == nil {
		t.Fatalf("tab to replace input = focus %d cmd %v", updated.focusedInput, cmd)
	}
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in replace input did not create replace command")
	}
	if replace, ok := cmd().(ReplaceOneMsg); !ok || replace.Query != "needle" || replace.Replacement != "replacement" {
		t.Fatalf("replace command = %#v", cmd())
	}
	updated.focusedInput = 0
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl | tea.ModShift})
	if cmd == nil {
		t.Fatal("ctrl+shift+enter did not create replace-all command")
	}
	if replace, ok := cmd().(ReplaceAllMsg); !ok || replace.Query != "needle" || replace.Replacement != "replacement" {
		t.Fatalf("replace-all command = %#v", cmd())
	}

	updated.showReplace = false
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if updated.mode != ModeSemantic || cmd == nil {
		t.Fatalf("mode toggle = mode:%v cmd:%v, want semantic search", updated.mode, cmd)
	}
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if updated.regex {
		t.Fatal("semantic mode toggled regex; regex is text-search only")
	}
	updated.mode = ModeText
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !updated.regex {
		t.Fatal("text mode did not toggle regex")
	}

	updated.results = []Result{{FilePath: "clicked.go", Line: 1, Col: 2, Preview: "needle"}}
	updated.scrollY = 0
	updated.cursor = 0
	updated, _ = updated.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if updated.scrollY != 0 {
		t.Fatalf("wheel down with short result list scrollY = %d, want 0", updated.scrollY)
	}
	updated, cmd = updated.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, Y: updated.headerLines()}))
	if cmd == nil {
		t.Fatal("result click did not create an open command")
	}
}

func TestModelSearchStatusMessagesRespectGeneration(t *testing.T) {
	m := New(ui.DefaultTheme(), t.TempDir(), ModeSemantic)
	m.debounceGen = 4
	stale, _ := m.Update(SearchIndexingMsg{Generation: 3})
	if stale.indexing {
		t.Fatal("stale indexing message changed state")
	}
	current, cmd := m.Update(SearchIndexingMsg{Generation: 4})
	if !current.indexing || cmd == nil {
		t.Fatalf("current indexing message = indexing:%t cmd:%v", current.indexing, cmd)
	}
	current, _ = current.Update(SearchResultsMsg{Generation: 4, Err: fmt.Errorf("fixture failure")})
	if current.searching || current.indexing || current.errMsg != "fixture failure" {
		t.Fatalf("error result state = searching:%t indexing:%t err:%q", current.searching, current.indexing, current.errMsg)
	}
}

func TestSemanticSearchContextHonorsCancellationBeforeToolResolution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SemanticSearchContext(ctx, t.TempDir(), "needle"); !errors.Is(err, context.Canceled) {
		t.Fatalf("SemanticSearchContext() error = %v, want context.Canceled", err)
	}
}
