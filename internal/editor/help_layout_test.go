package editor

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// helpCells paints a rendered view into a cell grid so tests can inspect the
// style of every cell rather than the escape sequences that produced it.
func helpCells(view string) uv.ScreenBuffer {
	w, h := 0, 0
	for _, line := range strings.Split(view, "\n") {
		w = max(w, ansi.StringWidth(line))
		h++
	}
	buf := uv.NewScreenBuffer(w, h)
	uv.NewStyledString(view).Draw(buf, uv.Rect(0, 0, w, h))
	return buf
}

func sameHelpColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func helpPlainRows(m HelpModel) []string {
	rows := make([]string, 0, len(m.filtered))
	for _, line := range m.filtered {
		rows = append(rows, ansi.Strip(line.rendered))
	}
	return rows
}

func helpViewWidth(view string) int {
	w := 0
	for _, line := range strings.Split(view, "\n") {
		w = max(w, ansi.StringWidth(line))
	}
	return w
}

func TestHelpKeyColumnFitsLongestKey(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := NewHelpModel(ui.DefaultTheme())
			m.SetSize(size[0], size[1])

			descCol := -1
			for _, row := range helpPlainRows(m) {
				// A short key plus the two keys that used to overflow the
				// fixed 16-cell column must put their descriptions together.
				for _, probe := range []struct{ key, desc string }{
					{"Ctrl+Q", "Quit"},
					{"Ctrl+Shift+Left/Right", "Select words"},
					{"Ctrl+Shift+Home/End", "Select to document"},
				} {
					if !strings.HasPrefix(row, "  "+probe.key) {
						continue
					}
					col := strings.Index(row, probe.desc)
					if col < len("  "+probe.key)+2 {
						t.Fatalf("%q: description at col %d leaves under 2 cells after the key: %q", probe.key, col, row)
					}
					if descCol < 0 {
						descCol = col
					}
					if col != descCol {
						t.Errorf("%q: description at col %d, want %d: %q", probe.key, col, descCol, row)
					}
				}
			}
			if descCol < 0 {
				t.Fatal("probe rows not found")
			}
		})
	}
}

func TestHelpKeyColumnIsCapped(t *testing.T) {
	m := NewHelpModel(ui.DefaultTheme())
	m.SetSize(80, 24)
	for _, row := range helpPlainRows(m) {
		if strings.HasPrefix(row, "  Shift+PageUp / Shift+PageDown") {
			t.Fatalf("pathological key was not wrapped inside the key column: %q", row)
		}
	}
	// The wrapped key still leaves its description legible in one piece.
	found := false
	for _, row := range helpPlainRows(m) {
		if strings.Contains(row, "Scroll shell history") {
			found = true
		}
	}
	if !found {
		t.Fatal("description of the longest key was lost or split")
	}
}

func TestHelpWrappedRowsUseHangingIndent(t *testing.T) {
	m := NewHelpModel(ui.DefaultTheme())
	m.SetSize(52, 40)

	descCol := -1
	for _, row := range helpPlainRows(m) {
		if strings.HasPrefix(row, "  Ctrl+Q") {
			descCol = strings.Index(row, "Quit")
		}
	}
	if descCol < 0 {
		t.Fatal("could not locate the description column")
	}

	continuations := 0
	for i := 1; i < len(m.filtered); i++ {
		// A continuation shares its binding's search text with the row above.
		row := ansi.Strip(m.filtered[i].rendered)
		if m.filtered[i].isTitle || strings.TrimSpace(row) == "" || m.filtered[i-1].text != m.filtered[i].text {
			continue
		}
		if strings.TrimSpace(row[:descCol-2]) != "" {
			continue // the key itself wraps; its tail sits in the key column
		}
		continuations++
		if lead := len(row) - len(strings.TrimLeft(row, " ")); lead != descCol {
			t.Errorf("continuation row starts at col %d, want description column %d: %q", lead, descCol, row)
		}
	}
	if continuations == 0 {
		t.Fatal("expected some wrapped descriptions at 52 columns")
	}
}

func TestHelpWrappedGoToLineStaysTogether(t *testing.T) {
	m := NewHelpModel(ui.DefaultTheme())
	m.SetSize(52, 40)
	rows := helpPlainRows(m)
	for i, row := range rows {
		if !strings.Contains(row, "Go to line") {
			continue
		}
		descCol := strings.Index(row, "Go to line")
		if i+1 >= len(rows) || !strings.Contains(rows[i+1], "line:col)") {
			t.Fatalf("expected %q to wrap onto the next row:\n%s", "line:col)", strings.Join(rows[i:i+2], "\n"))
		}
		if got := strings.Index(rows[i+1], "line:col)"); got != descCol {
			t.Fatalf("continuation at col %d, want %d:\n%s\n%s", got, descCol, row, rows[i+1])
		}
		return
	}
	t.Fatal("Go to line row not found")
}

func TestHelpScrollCountsWrappedRows(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {48, 24}, {40, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := NewHelpModel(ui.DefaultTheme())
			m.SetSize(size[0], size[1])
			if got, want := m.maxScroll(), len(m.filtered)-m.visibleLines(); got != want {
				t.Fatalf("maxScroll = %d, want %d physical rows", got, want)
			}
			m.scrollY = m.maxScroll()
			last := strings.TrimRight(ansi.Strip(m.filtered[len(m.filtered)-1].rendered), " ")
			if view := ansi.Strip(m.View()); !strings.Contains(view, last) {
				t.Fatalf("last row %q not visible at max scroll:\n%s", last, view)
			}
		})
	}
}

func TestHelpBoxWidensOnLargeTerminals(t *testing.T) {
	for _, tc := range []struct{ w, h, wantMin, wantMax int }{
		{140, 40, 70, 80},
		{80, 24, 60, 76},
		{40, 20, 30, 36},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			m := NewHelpModel(ui.DefaultTheme())
			m.SetSize(tc.w, tc.h)
			view := m.View()
			if w := helpViewWidth(view); w < tc.wantMin || w > tc.wantMax {
				t.Fatalf("box width = %d, want %d..%d", w, tc.wantMin, tc.wantMax)
			}
			if h := strings.Count(view, "\n") + 1; h > tc.h {
				t.Fatalf("box height = %d exceeds terminal height %d", h, tc.h)
			}
		})
	}
}

// TestHelpEveryCellCarriesPanelBackground pins that no cell inside the box
// drops to another background (the editor's, or none): keys, descriptions,
// gaps, titles, wrapped rows, the filter row and the footer all sit on the
// panel.
func TestHelpEveryCellCarriesPanelBackground(t *testing.T) {
	for _, opt := range ui.ThemeOptions() {
		theme := opt.Constructor()
		panelBg := theme.Panel.GetBackground()
		for _, state := range []struct {
			name         string
			w, h, scroll int
			query        string
		}{
			{"wide", 140, 40, 0, ""},
			{"standard", 80, 24, 0, ""},
			{"scrolled", 80, 24, 9999, ""},
			{"narrow", 40, 20, 3, ""},
			{"filtered", 80, 24, 0, "select"},
			{"no_match", 80, 24, 0, "zzqqxx-no-such-shortcut"},
		} {
			t.Run(opt.ID+"/"+state.name, func(t *testing.T) {
				m := NewHelpModel(theme)
				m.SetSize(state.w, state.h)
				m.Focus()
				m.input.SetValue(state.query)
				m.rebuildLines()
				m.scrollY = min(state.scroll, m.maxScroll())
				view := m.View()

				buf := helpCells(view)
				for y, line := range buf.Lines {
					for x, cell := range line {
						if x == 0 || x == len(line)-1 || y == 0 || y == len(buf.Lines)-1 {
							continue // border
						}
						if cell.Style.Attrs&uv.AttrReverse != 0 {
							continue // the filter's virtual cursor block
						}
						if !sameHelpColor(cell.Style.Bg, panelBg) {
							t.Fatalf("cell (%d,%d) %q has background %v, want panel %v\n%s", x, y, cell.Content, cell.Style.Bg, panelBg, ansi.Strip(view))
						}
					}
				}
			})
		}
	}
}

func TestHelpFooterAndEmptyStateUseMutedForeground(t *testing.T) {
	theme := ui.DefaultTheme()
	muted := theme.Muted.GetForeground()

	find := func(t *testing.T, view, needle string) uv.Cell {
		t.Helper()
		buf := helpCells(view)
		rows := strings.Split(view, "\n")
		for y, line := range buf.Lines {
			plain := ansi.Strip(rows[y])
			if idx := strings.Index(plain, needle); idx >= 0 {
				return line[ansi.StringWidth(plain[:idx])]
			}
		}
		t.Fatalf("%q not found in view:\n%s", needle, ansi.Strip(view))
		return uv.Cell{}
	}

	m := NewHelpModel(theme)
	m.SetSize(80, 24)
	cell := find(t, m.View(), "Use arrows or scroll to navigate")
	if !sameHelpColor(cell.Style.Fg, muted) {
		t.Fatalf("footer foreground = %v, want theme.Muted %v", cell.Style.Fg, muted)
	}

	m.input.SetValue("zzqqxx-no-such-shortcut")
	m.rebuildLines()
	cell = find(t, m.View(), "No matching shortcuts")
	if !sameHelpColor(cell.Style.Fg, muted) {
		t.Fatalf("empty-state foreground = %v, want theme.Muted %v", cell.Style.Fg, muted)
	}
}
