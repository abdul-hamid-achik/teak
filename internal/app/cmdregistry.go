package app

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/overlay"
	"teak/internal/search"
)

// Command describes a registered editor command for the command palette.
type Command struct {
	ID       string
	Label    string
	Shortcut string
	Execute  func() tea.Msg
}

// commandPaletteMsg wraps a message from a command palette selection
// so it can be re-dispatched through the normal Update cycle.
type commandPaletteMsg struct {
	inner tea.Msg
}

// buildCommandList returns the full list of commands as picker items.
func (m *Model) buildCommandList() []overlay.PickerItem {
	commands := m.commandRegistry()
	items := make([]overlay.PickerItem, len(commands))
	for i, cmd := range commands {
		label := cmd.Label
		if cmd.Shortcut != "" {
			label += "  " + cmd.Shortcut
		}
		items[i] = overlay.PickerItem{
			Label:       label,
			Description: "",
			Value:       cmd,
		}
	}
	return items
}

// commandRegistry returns all available commands.
func (m *Model) commandRegistry() []Command {
	return []Command{
		{
			ID:       "save",
			Label:    "Save File",
			Shortcut: "Ctrl+S",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: saveRequestMsg{}}
			},
		},
		{
			ID:       "close_tab",
			Label:    "Close Tab",
			Shortcut: "Ctrl+W",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: CloseTabMsg{Index: -1}} // -1 = active tab
			},
		},
		{
			ID:       "reopen_tab",
			Label:    "Reopen Closed Tab",
			Shortcut: "Ctrl+Shift+T",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: reopenTabMsg{}}
			},
		},
		{
			ID:       "toggle_tree",
			Label:    "Toggle File Tree",
			Shortcut: "Ctrl+B",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleTreeMsg{}}
			},
		},
		{
			ID:       "toggle_git",
			Label:    "Toggle Git Panel",
			Shortcut: "Ctrl+Shift+G",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleGitMsg{}}
			},
		},
		{
			ID:       "toggle_problems",
			Label:    "Toggle Problems Panel",
			Shortcut: "F8",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleProblemsMsg{}}
			},
		},
		{
			ID:       "find",
			Label:    "Find",
			Shortcut: "Ctrl+F",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openSearchMsg{mode: search.ModeText}}
			},
		},
		{
			ID:       "find_replace",
			Label:    "Find & Replace",
			Shortcut: "Ctrl+H",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openSearchReplaceMsg{}}
			},
		},
		{
			ID:       "semantic_search",
			Label:    "Semantic Search",
			Shortcut: "Ctrl+Shift+F",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openSearchMsg{mode: search.ModeSemantic}}
			},
		},
		{
			ID:       "goto_line",
			Label:    "Go to Line...",
			Shortcut: "Ctrl+G",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: goToLineMsg{}}
			},
		},
		{
			ID:       "quick_open",
			Label:    "Quick Open...",
			Shortcut: "Ctrl+P",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: quickOpenMsg{}}
			},
		},
		{
			ID:       "help",
			Label:    "Show Help",
			Shortcut: "F1",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: showHelpMsg{}}
			},
		},
		{
			ID:       "settings",
			Label:    "Open Settings",
			Shortcut: "Ctrl+,",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openSettingsMsg{}}
			},
		},
		{
			ID:       "debug_start",
			Label:    "Start Debugging",
			Shortcut: "F5",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: debugStartMsg{}}
			},
		},
		{
			ID:       "debug_stop",
			Label:    "Stop Debugging",
			Shortcut: "Shift+F5",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: debugStopMsg{}}
			},
		},
		{
			ID:       "quit",
			Label:    "Quit",
			Shortcut: "Ctrl+Q",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: quitMsg{}}
			},
		},
		{
			ID:       "toggle_agent",
			Label:    "Toggle Agent Panel",
			Shortcut: "Ctrl+J",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleAgentMsg{}}
			},
		},
		{
			ID:       "focus_agent",
			Label:    "Focus Agent Panel",
			Shortcut: "Ctrl+'",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: focusAgentMsg{}}
			},
		},
		{
			ID:       "agent_cancel",
			Label:    "Cancel Agent",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: agentCancelMsg{}}
			},
		},
	}
}

// Internal message types for command palette actions.
type (
	saveRequestMsg      struct{}
	toggleTreeMsg       struct{}
	toggleGitMsg        struct{}
	toggleProblemsMsg   struct{}
	openSearchMsg       struct{ mode search.Mode }
	openSearchReplaceMsg struct{}
	goToLineMsg         struct{}
	quickOpenMsg        struct{}
	showHelpMsg         struct{}
	openSettingsMsg     struct{}
	reopenTabMsg        struct{}
	debugStartMsg       struct{}
	debugStopMsg        struct{}
	quitMsg             struct{}
)
