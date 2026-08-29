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
			ID:    "toggle_problems",
			Label: "Show Problems Panel",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleProblemsMsg{}}
			},
		},
		{
			ID:    "health_dashboard",
			Label: "Workspace Health Dashboard",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openHealthDashboardMsg{}}
			},
		},
		{
			// This entry opens the project-wide search overlay, which is bound
			// to Ctrl+Shift+F — Ctrl+F opens the in-buffer find widget instead,
			// so the labels must not be swapped.
			ID:       "find",
			Label:    "Find in Project",
			Shortcut: "Ctrl+Shift+F",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: openSearchMsg{mode: search.ModeText}}
			},
		},
		{
			ID:       "find_in_file",
			Label:    "Find in File",
			Shortcut: "Ctrl+F",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: showEditorFindMsg{}}
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
			ID:    "semantic_search",
			Label: "Semantic Search",
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
			ID:       "new_file",
			Label:    "New File",
			Shortcut: "Ctrl+N",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: newFileMsg{}}
			},
		},
		{
			ID:       "save_as",
			Label:    "Save As...",
			Shortcut: "Ctrl+Shift+S",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: saveAsMsg{}}
			},
		},
		{
			ID:       "find_next",
			Label:    "Find Next",
			Shortcut: "F3",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: FindNextMsg{}}
			},
		},
		{
			ID:       "find_prev",
			Label:    "Find Previous",
			Shortcut: "Shift+F3",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: FindPrevMsg{}}
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
			ID:    "agent_cancel",
			Label: "Cancel Agent",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: agentCancelMsg{}}
			},
		},
		{
			ID:    "codemap_callers",
			Label: "Code Map: Find Callers",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: codemapCallersMsg{}}
			},
		},
		{
			ID:    "codemap_callees",
			Label: "Code Map: Find Callees",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: codemapCalleesMsg{}}
			},
		},
		{
			ID:    "codemap_impact",
			Label: "Code Map: Symbol Impact",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: codemapImpactMsg{}}
			},
		},
		{
			ID:    "bob_plan",
			Label: "Bob: Plan Review",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: bobPlanMsg{}}
			},
		},
		{
			ID:    "bob_check",
			Label: "Bob: Check Drift",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: bobCheckMsg{}}
			},
		},
		{
			ID:       "format_file",
			Label:    "Format File",
			Shortcut: "Ctrl+Alt+F",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: formatFileMsg{}}
			},
		},
		{
			ID:       "goto_definition",
			Label:    "Go to Definition",
			Shortcut: "F12",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: gotoDefinitionMsg{}}
			},
		},
		{
			ID:       "rename_symbol",
			Label:    "Rename Symbol...",
			Shortcut: "F2",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: renameSymbolMsg{}}
			},
		},
		{
			ID:       "code_actions",
			Label:    "Code Actions...",
			Shortcut: "Ctrl+K",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: codeActionsMsg{}}
			},
		},
		{
			ID:       "hover",
			Label:    "Show Hover",
			Shortcut: "Alt+K",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: hoverSymbolMsg{}}
			},
		},
		{
			ID:       "document_symbols",
			Label:    "Document Symbols...",
			Shortcut: "Ctrl+Shift+O",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: documentSymbolsMsg{}}
			},
		},
		{
			ID:       "toggle_split",
			Label:    "Toggle Split View",
			Shortcut: "Ctrl+\\",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleSplitMsg{}}
			},
		},
		{
			ID:       "close_split",
			Label:    "Close Split",
			Shortcut: "Ctrl+Shift+\\",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: closeSplitMsg{}}
			},
		},
		{
			ID:       "cycle_split",
			Label:    "Cycle Split Pane",
			Shortcut: "F6",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: cycleSplitMsg{}}
			},
		},
		{
			ID:       "toggle_breakpoint",
			Label:    "Toggle Breakpoint",
			Shortcut: "F9",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: toggleBreakpointMsg{}}
			},
		},
		{
			ID:       "fold",
			Label:    "Fold Region",
			Shortcut: "Ctrl+Shift+[",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: foldLineMsg{}}
			},
		},
		{
			ID:       "unfold",
			Label:    "Unfold Region",
			Shortcut: "Ctrl+Shift+]",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: unfoldLineMsg{}}
			},
		},
		{
			ID:       "fold_all",
			Label:    "Fold All Regions",
			Shortcut: "Ctrl+Shift+0",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: foldAllMsg{}}
			},
		},
		{
			ID:       "unfold_all",
			Label:    "Unfold All Regions",
			Shortcut: "Ctrl+Shift+J",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: unfoldAllMsg{}}
			},
		},
		{
			ID:       "next_tab",
			Label:    "Next Tab",
			Shortcut: "Ctrl+Tab",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: nextTabMsg{}}
			},
		},
		{
			ID:       "prev_tab",
			Label:    "Previous Tab",
			Shortcut: "Ctrl+Shift+Tab",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: prevTabMsg{}}
			},
		},
		{
			ID:       "next_problem",
			Label:    "Next Problem",
			Shortcut: "F8",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: nextProblemMsg{}}
			},
		},
		{
			ID:       "prev_problem",
			Label:    "Previous Problem",
			Shortcut: "Shift+F8",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: prevProblemMsg{}}
			},
		},
		{
			ID:    "restart_lsp",
			Label: "Restart Language Server",
			Execute: func() tea.Msg {
				return commandPaletteMsg{inner: restartLspMsg{}}
			},
		},
	}
}

// Internal message types for command palette actions.
type (
	saveRequestMsg         struct{}
	toggleTreeMsg          struct{}
	toggleGitMsg           struct{}
	toggleProblemsMsg      struct{}
	openSearchMsg          struct{ mode search.Mode }
	openSearchReplaceMsg   struct{}
	showEditorFindMsg      struct{}
	goToLineMsg            struct{}
	quickOpenMsg           struct{}
	showHelpMsg            struct{}
	openSettingsMsg        struct{}
	openHealthDashboardMsg struct{}
	reopenTabMsg           struct{}
	debugStartMsg          struct{}
	debugStopMsg           struct{}
	quitMsg                struct{}
	newFileMsg             struct{}
	saveAsMsg              struct{}
	codemapCallersMsg      struct{}
	codemapCalleesMsg      struct{}
	codemapImpactMsg       struct{}
	bobPlanMsg             struct{}
	bobCheckMsg            struct{}
	formatFileMsg          struct{}
	gotoDefinitionMsg      struct{}
	renameSymbolMsg        struct{}
	codeActionsMsg         struct{}
	hoverSymbolMsg         struct{}
	documentSymbolsMsg     struct{}
	toggleSplitMsg         struct{}
	closeSplitMsg          struct{}
	cycleSplitMsg          struct{}
	toggleBreakpointMsg    struct{}
	foldLineMsg            struct{}
	unfoldLineMsg          struct{}
	foldAllMsg             struct{}
	unfoldAllMsg           struct{}
	nextTabMsg             struct{}
	prevTabMsg             struct{}
	nextProblemMsg         struct{}
	prevProblemMsg         struct{}
	restartLspMsg          struct{}
)
