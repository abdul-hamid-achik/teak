package editor

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/text"
	"teak/internal/ui"
)

// sgrParams returns the SGR parameters (for example "38;2;1;2;3") lipgloss
// emits for fg or bg, so raw View output can be checked without depending on
// how the styles combine them into one escape.
func sgrParams(t *testing.T, style lipgloss.Style) string {
	t.Helper()
	probe := style.Render("x")
	end := strings.Index(probe, "x")
	if end < 0 || !strings.HasPrefix(probe, "\x1b[") {
		t.Fatalf("style rendered without an escape: %q", probe)
	}
	return strings.TrimSuffix(strings.TrimPrefix(probe[:end], "\x1b["), "m")
}

func colorOf(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func bgOf(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Background(c) }

// Separator rows were drawn in the normal item text color, which made them
// the loudest thing in the menu. They are drawn in the border color instead,
// and like every other row they carry the panel background.
func TestContextMenuSeparatorIsSubduedAndKeepsPanelBackground(t *testing.T) {
	for _, id := range ui.ThemeIDs() {
		t.Run(id, func(t *testing.T) {
			theme := ui.ThemeByName(id)
			menu := NewContextMenu(theme)
			menu.Show([]ContextMenuItem{
				{Label: "Cut", Action: "cut"},
				{Label: ""}, // separator
				{Label: "Paste", Action: "paste"},
			}, 0, 0)

			var sepRows []string
			for _, row := range strings.Split(menu.View(), "\n") {
				plain := strings.Trim(ansi.Strip(row), "│ ")
				if plain != "" && strings.Trim(plain, "─") == "" {
					sepRows = append(sepRows, row)
				}
			}
			if len(sepRows) != 1 {
				t.Fatalf("found %d separator rows, want 1", len(sepRows))
			}
			// The escape in force where the dashes start, not the box border
			// drawn around them.
			row := sepRows[0]
			dashes := strings.Index(row, "─")
			open := strings.LastIndex(row[:dashes], "\x1b[")
			row = row[open:dashes]

			border := sgrParams(t, colorOf(theme.AutocompleteBox.GetBorderTopForeground()))
			itemFg := sgrParams(t, colorOf(theme.AutocompleteItem.GetForeground()))
			bg := sgrParams(t, bgOf(theme.AutocompleteBox.GetBackground()))

			if !strings.Contains(row, border) {
				t.Errorf("separator row %q does not use the border color %q", row, border)
			}
			if border != itemFg && strings.Contains(row, itemFg) {
				t.Errorf("separator row %q is drawn in the item text color %q", row, itemFg)
			}
			if !strings.Contains(row, bg) {
				t.Errorf("separator row %q lacks the menu background %q", row, bg)
			}
		})
	}
}

// The context menu advertises the chords the keyboard already has; Find
// References and Rename Symbol were bound but showed no hint.
func TestEditorMenuLSPItemsShowTheirShortcuts(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.HasLSP = true

	want := map[string]string{
		"goto_definition": "F12",
		"find_references": "Shift+F12",
		"rename_symbol":   "F2",
		"format_document": "Ctrl+Alt+F",
	}
	got := make(map[string]string)
	for _, item := range ed.buildEditorMenuItems() {
		if _, ok := want[item.Action]; ok {
			got[item.Action] = item.Shortcut
		}
	}
	for action, shortcut := range want {
		if got[action] != shortcut {
			t.Errorf("%s shortcut = %q, want %q", action, got[action], shortcut)
		}
	}
}
