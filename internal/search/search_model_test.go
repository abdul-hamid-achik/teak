package search

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
