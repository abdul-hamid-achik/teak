package editor

import (
	"fmt"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

func welcomeRows(w *Welcome) []string {
	return strings.Split(ansi.Strip(w.View()), "\n")
}

// TestWelcomeHintsFormAlignedColumns pins that the shortcuts render as one
// centered block: every key starts at one x and every description at another.
func TestWelcomeHintsFormAlignedColumns(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w := NewWelcome(ui.DefaultTheme())
			w.SetSize(size[0], size[1])
			rows := welcomeRows(&w)

			keyCol, descCol := -1, -1
			for _, h := range welcomeHints {
				found := false
				for _, row := range rows {
					k := strings.Index(row, h.key+" ")
					d := strings.Index(row, h.desc)
					if k < 0 || d < 0 || d < k {
						continue
					}
					found = true
					if keyCol < 0 {
						keyCol, descCol = k, d
					}
					if k != keyCol || d != descCol {
						t.Errorf("%q: key at x=%d, desc at x=%d; want %d and %d\n%s", h.key, k, d, keyCol, descCol, row)
					}
				}
				if !found {
					t.Errorf("hint %q %q missing at %dx%d", h.key, h.desc, size[0], size[1])
				}
			}
		})
	}
}

// TestWelcomeHintsAreRealBindings keeps the starters honest: each key must be a
// documented shortcut, and nothing that needs an open file may be listed.
func TestWelcomeHintsAreRealBindings(t *testing.T) {
	documented := map[string]bool{}
	for _, group := range helpGroups {
		for _, b := range group.bindings {
			documented[b.key] = true
		}
	}
	for _, h := range welcomeHints {
		if !documented[h.key] {
			t.Errorf("welcome hint %q is not a documented shortcut in help.go", h.key)
		}
		if h.key == "Ctrl+F" {
			t.Error("Ctrl+F does nothing on the welcome screen")
		}
	}
}

func TestWelcomeFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {80, 22}, {60, 16}, {40, 12}, {40, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w := NewWelcome(ui.DefaultTheme())
			w.SetSize(size[0], size[1])
			header := []string{"", "", "", "", "", "", "", "tagline", "", ""}
			rows := w.fitRows(header, make([]string, len(welcomeHints)), []string{"", "footer"})
			if len(rows) > size[1] {
				t.Fatalf("fitRows = %d rows, exceeds height %d", len(rows), size[1])
			}
			if got := len(welcomeRows(&w)); got != size[1] {
				t.Fatalf("view is %d rows, want exactly %d", got, size[1])
			}
		})
	}
}

func TestWelcomeKeepsEssentialHintsWhenShort(t *testing.T) {
	w := NewWelcome(ui.DefaultTheme())
	w.SetSize(80, 18)
	view := strings.Join(welcomeRows(&w), "\n")
	for _, want := range []string{"Ctrl+P", "Ctrl+Shift+P", "F1"} {
		if !strings.Contains(view, want) {
			t.Errorf("short welcome screen lost essential hint %q:\n%s", want, view)
		}
	}
}

func TestWelcomeFooterUsesMutedForeground(t *testing.T) {
	theme := ui.DefaultTheme()
	w := NewWelcome(theme)
	w.SetSize(80, 24)
	view := w.View()

	rows := strings.Split(view, "\n")
	buf := helpCells(view)
	for y, line := range buf.Lines {
		plain := ansi.Strip(rows[y])
		idx := strings.Index(plain, "Open a file from the tree")
		if idx < 0 {
			continue
		}
		cell := line[ansi.StringWidth(plain[:idx])]
		if !sameHelpColor(cell.Style.Fg, theme.Muted.GetForeground()) {
			t.Fatalf("footer foreground = %v, want theme.Muted %v", cell.Style.Fg, theme.Muted.GetForeground())
		}
		if !sameHelpColor(cell.Style.Bg, theme.Editor.GetBackground()) {
			t.Fatalf("footer background = %v, want editor %v", cell.Style.Bg, theme.Editor.GetBackground())
		}
		return
	}
	t.Fatal("footer not found")
}

func TestWelcomeHintCellsCarryEditorBackground(t *testing.T) {
	for _, opt := range ui.ThemeOptions() {
		theme := opt.Constructor()
		t.Run(opt.ID, func(t *testing.T) {
			w := NewWelcome(theme)
			w.SetSize(80, 24)
			view := w.View()
			rows := strings.Split(view, "\n")
			buf := helpCells(view)
			for y, line := range buf.Lines {
				plain := ansi.Strip(rows[y])
				if !strings.Contains(plain, "Ctrl+P") && !strings.Contains(plain, "Quit") && !strings.Contains(plain, "Open a file") {
					continue
				}
				for x, cell := range line {
					if cell.Style.Attrs&uv.AttrReverse != 0 {
						continue
					}
					if !sameHelpColor(cell.Style.Bg, theme.Editor.GetBackground()) {
						t.Fatalf("cell (%d,%d) on row %q has background %v, want editor %v", x, y, plain, cell.Style.Bg, theme.Editor.GetBackground())
					}
				}
			}
		})
	}
}
