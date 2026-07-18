package modes

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

// SearchMode handles search input
type SearchMode struct {
	BaseMode
}

func (m *SearchMode) ID() ModeID { return ModeSearch }

func (m *SearchMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *SearchMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *SearchMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeSearch, nil
}

func (m *SearchMode) ShouldIntercept(msg tea.Msg) bool {
	return false // Search overlay handles its own input
}

// SearchReplaceMode handles search & replace
type SearchReplaceMode struct {
	BaseMode
}

func (m *SearchReplaceMode) ID() ModeID { return ModeSearchReplace }

func (m *SearchReplaceMode) Enter(model *app.Model) tea.Cmd {
	return nil
}

func (m *SearchReplaceMode) Exit(model *app.Model) tea.Cmd {
	return nil
}

func (m *SearchReplaceMode) Update(msg tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, ModeSearchReplace, nil
}

func (m *SearchReplaceMode) ShouldIntercept(msg tea.Msg) bool {
	return false
}
