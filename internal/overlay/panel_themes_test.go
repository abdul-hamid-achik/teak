package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

// Every built-in theme must keep its panels on a single background.
func TestPanelsCarryBackgroundInEveryTheme(t *testing.T) {
	for _, opt := range ui.ThemeOptions() {
		t.Run(opt.ID, func(t *testing.T) {
			theme := opt.Constructor()
			panel := panelBackground(theme)
			cursorBg := theme.AutocompleteCursor.GetBackground()
			itemBg := theme.AutocompleteItem.GetBackground()

			p := NewPicker("Command Palette", []PickerItem{
				{Label: "Save File", Description: "Ctrl+S"},
				{Label: "Quit", Description: "Ctrl+Q"},
			}, theme, "themes")
			p.SetDescriptionsRightAligned(true)
			p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			assertPanelBackground(t, p.View(), panel, cursorBg, itemBg)

			in := NewInput("Name", "x", theme)
			in.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			assertPanelBackground(t, in.View(), panel)

			c := NewConfirm("T", "msg", []string{"a.go"}, quitButtons(theme), theme)
			c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			assertPanelBackground(t, c.View(), panel,
				theme.GitActionButton.GetBackground(), theme.GitPushPullButton.GetBackground(), theme.GitCommitButton.GetBackground())
		})
	}
}
