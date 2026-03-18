package keybindings

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
	"teak/internal/app/modes"
)

// RegisterDefaults registers all default Teak keybindings
// Note: Command methods will be added to app.Model in later refactoring phases
func RegisterDefaults(r *Registry) {
	// File operations
	r.Bind(
		[]string{"ctrl+n"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.NewFileCmd()
		},
		"New file",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+o"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.OpenFileCmd()
		},
		"Open file",
		InContext(app.FocusEditor, app.FocusTree),
	)

	r.Bind(
		[]string{"ctrl+s"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.SaveFileCmd()
		},
		"Save file",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+shift+s"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.SaveAsCmd()
		},
		"Save as...",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+w"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.CloseTabCmd()
		},
		"Close tab",
		InContext(app.FocusEditor),
	)

	// Navigation
	r.Bind(
		[]string{"ctrl+p"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.QuickOpenCmd()
		},
		"Quick open",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+g"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.GoToLineCmd()
		},
		"Go to line",
		InContext(app.FocusEditor),
	)

	// Search
	r.Bind(
		[]string{"ctrl+f"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.SearchCmd()
		},
		"Find",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+h"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ReplaceCmd()
		},
		"Find & replace",
		InContext(app.FocusEditor),
	)

	// LSP - these will be wired to LSP coordinator
	r.Bind(
		[]string{"gd", "F12"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.GoToDefinitionCmd()
		},
		"Go to definition",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"gr"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.FindReferencesCmd()
		},
		"Find references",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"K", "shift+K"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.HoverCmd()
		},
		"Show hover",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"F2"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.RenameSymbolCmd()
		},
		"Rename symbol",
		InContext(app.FocusEditor),
	)

	// UI toggles
	r.Bind(
		[]string{"ctrl+b"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleTreeCmd()
		},
		"Toggle file tree",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+`"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleGitPanelCmd()
		},
		"Toggle git panel",
	)

	r.Bind(
		[]string{"ctrl+shift+m"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleProblemsCmd()
		},
		"Toggle problems panel",
	)

	r.Bind(
		[]string{"ctrl+,"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.OpenSettingsCmd()
		},
		"Open settings",
	)

	r.Bind(
		[]string{"ctrl+?"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleHelpCmd()
		},
		"Toggle help",
	)

	// Tab navigation
	r.Bind(
		[]string{"ctrl+tab"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.NextTabCmd()
		},
		"Next tab",
		InContext(app.FocusEditor),
	)

	r.Bind(
		[]string{"ctrl+shift+tab"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.PrevTabCmd()
		},
		"Previous tab",
		InContext(app.FocusEditor),
	)

	// Git panel
	r.Bind(
		[]string{"ga"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.StageAllCmd()
		},
		"Stage all",
		InContext(app.FocusGitPanel),
	)

	r.Bind(
		[]string{"gu"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.UnstageAllCmd()
		},
		"Unstage all",
		InContext(app.FocusGitPanel),
	)

	// Debugger
	r.Bind(
		[]string{"F5"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.DebugStartCmd()
		},
		"Start debugging",
		InContext(app.FocusEditor, app.FocusDebugger),
	)

	r.Bind(
		[]string{"shift+F5"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.DebugStopCmd()
		},
		"Stop debugging",
		InContext(app.FocusDebugger),
	)

	r.Bind(
		[]string{"F9"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleBreakpointCmd()
		},
		"Toggle breakpoint",
		InContext(app.FocusEditor),
	)

	// Agent
	r.Bind(
		[]string{"ctrl+alt+a"},
		func(m *app.Model) tea.Cmd {
			return nil // TODO: app.ToggleAgentCmd()
		},
		"Toggle agent panel",
	)

	// Mode switching
	r.Bind(
		[]string{"i"},
		func(m *app.Model) tea.Cmd {
			return func() tea.Msg { return modes.ModeTransitionMsg{To: modes.ModeInsert} }
		},
		"Insert mode",
		InContext(app.FocusEditor),
		InModes(modes.ModeNormal),
	)

	r.Bind(
		[]string{"esc"},
		func(m *app.Model) tea.Cmd {
			return func() tea.Msg { return modes.ModeTransitionMsg{To: modes.ModeNormal} }
		},
		"Normal mode",
		InContext(app.FocusEditor),
		InModes(modes.ModeInsert),
	)
}
