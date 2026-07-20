package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/search"
)

func TestSearchOverlayMouseClickUsesCenteredOverlayCoordinates(t *testing.T) {
	zone.NewGlobal()
	m, err := NewModel("", t.TempDir(), config.DefaultConfig())
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	m.width = 100
	m.height = 30
	m.showSearch = true
	m.searchM = search.New(m.theme, m.rootDir, search.ModeText)
	m.searchM.SetSize(m.width, m.height-2)
	m.searchM, _ = m.searchM.Update(search.SearchResultsMsg{
		Results: []search.Result{{FilePath: "selected.go", Line: 4, Col: 2}},
	})

	searchView := m.searchM.View()
	_ = zone.Scan(m.View().Content)
	overlayX := (m.width - lipgloss.Width(searchView)) / 2
	overlayY := (m.height - len(strings.Split(searchView, "\n"))) / 2
	if overlayX < 0 {
		overlayX = 0
	}
	if overlayY < 0 {
		overlayY = 0
	}

	updatedAny, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      overlayX + 4,
		Y:      overlayY + 6, // first result row: border + padding + header
	}))
	updated := updatedAny.(Model)
	if !updated.showSearch {
		t.Fatal("mouse result click unexpectedly closed the search overlay")
	}
	if cmd == nil {
		t.Fatal("click on a centered result did not produce an open-result command")
	}
	msg := cmd()
	open, ok := msg.(search.OpenResultMsg)
	if !ok {
		t.Fatalf("click command = %T, want search.OpenResultMsg", msg)
	}
	if open.FilePath != "selected.go" || open.Line != 4 || open.Col != 2 {
		t.Fatalf("opened %#v, want selected result", open)
	}
}

func TestAppRoutesSearchDebounceTickToVisibleOverlay(t *testing.T) {
	m, err := NewModel("", t.TempDir(), config.DefaultConfig())
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	m.width = 100
	m.height = 30

	openedAny, _ := m.openSearch(search.ModeText)
	opened := openedAny.(Model)
	typedAny, _ := opened.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	typed := typedAny.(Model)

	_, cmd := typed.Update(search.DebounceTickMsg{Generation: 1})
	if cmd == nil {
		t.Fatal("app dropped the search debounce message instead of dispatching the query")
	}
}
