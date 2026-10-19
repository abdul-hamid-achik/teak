package search

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

func colorKey(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d,%d,%d", r>>8, g>>8, b>>8)
}

type cell struct {
	r      rune
	fg, bg string
	rev    bool // reverse video: the input cursor block
}

// styledCells returns every printed rune of line with the truecolor foreground
// and background in effect. "" means the terminal default (nothing was set, or
// an inner style's reset cleared it).
func styledCells(line string) []cell {
	var cells []cell
	var fg, bg string
	var rev bool
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] != 0x1b || i+1 >= len(runes) || runes[i+1] != '[' {
			cells = append(cells, cell{runes[i], fg, bg, rev})
			continue
		}
		j := i + 2
		for j < len(runes) && (runes[j] < 0x40 || runes[j] > 0x7e) {
			j++
		}
		if j >= len(runes) {
			break
		}
		if runes[j] == 'm' {
			params := strings.Split(string(runes[i+2:j]), ";")
			for k := 0; k < len(params); k++ {
				switch p := params[k]; {
				case p == "" || p == "0":
					fg, bg, rev = "", "", false
				case p == "7":
					rev = true
				case p == "27":
					rev = false
				case p == "39":
					fg = ""
				case p == "49":
					bg = ""
				case (p == "38" || p == "48") && k+4 < len(params) && params[k+1] == "2":
					key := strings.Join(params[k+2:k+5], ",")
					if p == "38" {
						fg = key
					} else {
						bg = key
					}
					k += 4
				case (p == "38" || p == "48") && k+2 < len(params) && params[k+1] == "5":
					key := "idx" + params[k+2]
					if p == "38" {
						fg = key
					} else {
						bg = key
					}
					k += 2
				default:
					if n, err := strconv.Atoi(p); err == nil && ((n >= 40 && n <= 47) || (n >= 100 && n <= 107)) {
						bg = "ansi" + p
					}
				}
			}
		}
		i = j
	}
	return cells
}

// runeIndex is strings.Index in cells (runes), not bytes: rows start with a
// multi-byte border rune.
func runeIndex(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return len([]rune(s[:i]))
}

func plainOf(cells []cell) string {
	var sb strings.Builder
	for _, c := range cells {
		sb.WriteRune(c.r)
	}
	return sb.String()
}

func newZones(t *testing.T) {
	t.Helper()
	previous := zone.DefaultManager
	zone.NewGlobal()
	t.Cleanup(func() { zone.Close(); zone.DefaultManager = previous })
}

func sampleResults(n int) []Result {
	results := make([]Result, n)
	for i := range results {
		results[i] = Result{FilePath: "README.md", Line: 10 + i, Col: 4, Preview: "- `GET /items` lists every Item in the store"}
	}
	return results
}

// stateModel builds the overlay in one of the visual states the box can show.
func stateModel(theme ui.Theme, state string, width, height int) Model {
	m := New(theme, "/fixture", ModeText)
	m.SetSize(width, height)
	_ = m.Focus()
	switch state {
	case "empty":
	case "searching":
		m.input.SetValue("item")
		m.searching = true
	case "error":
		m.input.SetValue("item")
		m.errMsg = "Search tool unavailable"
	case "no-results":
		m.input.SetValue("item")
	case "results":
		m.input.SetValue("item")
		m.results = sampleResults(3)
		m.cursor = 1
	case "scrolling":
		m.input.SetValue("item")
		m.results = sampleResults(40)
		m.cursor = 2
	case "replace":
		m.input.SetValue("item")
		m.replaceInput.SetValue("thing")
		m.showReplace = true
		m.results = sampleResults(3)
	case "semantic":
		m.mode = ModeSemantic
		m.input.SetValue("how are items listed")
		m.indexing = true
		m.results = []Result{{FilePath: "api.go", Line: 3, Preview: "func listItems()", SymbolName: "listItems"}}
	}
	return m
}

func TestSearchViewCellsCarryPanelBackground(t *testing.T) {
	newZones(t)
	states := []string{"empty", "searching", "error", "no-results", "results", "scrolling", "replace", "semantic"}
	for _, option := range ui.ThemeOptions() {
		theme := option.Constructor()
		panel := colorKey(theme.Panel.GetBackground())
		active := colorKey(theme.SearchActive.GetBackground())
		match := colorKey(theme.FindMatch.GetBackground())
		current := colorKey(theme.FindMatchCurrent.GetBackground())
		button := colorKey(theme.ReplaceButton.GetBackground())
		for _, state := range states {
			t.Run(option.ID+"/"+state, func(t *testing.T) {
				m := stateModel(theme, state, 100, 30)
				lines := strings.Split(zone.Scan(m.View()), "\n")
				for n, line := range lines {
					if n == 0 || n == len(lines)-1 {
						continue // border rows
					}
					cells := styledCells(line)
					// Drop the side border cells; the inside must be the panel.
					inside := cells[1 : len(cells)-1]
					for col, c := range inside {
						ok := c.bg == panel || c.rev
						switch {
						case strings.Contains(plainOf(cells), "README.md:") || strings.Contains(plainOf(cells), "api.go:"):
							ok = ok || (c.bg == active && n > 0) || c.bg == match || c.bg == current
						case state == "replace" && strings.Contains(plainOf(cells), "Replace"):
							ok = ok || c.bg == button
						}
						if !ok {
							t.Fatalf("line %d col %d (%q) of %q has background %q, want panel %q", n, col, c.r, ansi.Strip(line), c.bg, panel)
						}
					}
				}
			})
		}
	}
}

// resultRow returns the styled cells of the first rendered result row.
func resultRow(t *testing.T, m Model, marker string) []cell {
	t.Helper()
	for _, line := range strings.Split(zone.Scan(m.View()), "\n") {
		cells := styledCells(line)
		if strings.Contains(plainOf(cells), marker) {
			return cells
		}
	}
	t.Fatalf("no row containing %q in:\n%s", marker, ansi.Strip(m.View()))
	return nil
}

func TestSearchResultLocationIsMutedAndPreviewIsNot(t *testing.T) {
	newZones(t)
	theme := ui.DefaultTheme()
	muted := colorKey(theme.Muted.GetForeground())
	m := stateModel(theme, "results", 140, 40)
	m.cursor = 2 // keep row 0 unselected
	cells := resultRow(t, m, "README.md:11")
	text := plainOf(cells)
	locStart := runeIndex(text, "README.md:11")
	for i := locStart; i < locStart+len("README.md:11"); i++ {
		if cells[i].fg != muted {
			t.Fatalf("location cell %d (%q) has fg %q, want muted %q", i, cells[i].r, cells[i].fg, muted)
		}
	}
	previewStart := runeIndex(text, "lists")
	for i := previewStart; i < previewStart+len("lists"); i++ {
		if cells[i].fg == muted {
			t.Fatalf("preview cell %d (%q) has the muted foreground", i, cells[i].r)
		}
	}
}

func TestSearchHighlightsMatchedTextRespectingOptions(t *testing.T) {
	newZones(t)
	theme := ui.DefaultTheme()
	match := colorKey(theme.FindMatch.GetBackground())
	current := colorKey(theme.FindMatchCurrent.GetBackground())
	panel := colorKey(theme.Panel.GetBackground())

	tests := []struct {
		name  string
		query string
		opts  SearchOpts
		want  []string // substrings of the preview that must be highlighted
	}{
		{"case-insensitive literal", "item", SearchOpts{}, []string{"item", "item", "Item"}},
		{"case-sensitive", "item", SearchOpts{CaseSensitive: true}, []string{"item", "item"}},
		{"whole word", "item", SearchOpts{WholeWord: true}, []string{"item", "Item"}},
		{"regex", `it.m`, SearchOpts{Regex: true}, []string{"item", "item", "Item"}},
		{"invalid regex highlights nothing", `it(`, SearchOpts{Regex: true}, nil},
		{"empty-width regex highlights nothing", `x*`, SearchOpts{Regex: true}, nil},
	}
	for _, tt := range tests {
		for _, selected := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/selected=%t", tt.name, selected), func(t *testing.T) {
				m := New(theme, "/fixture", ModeText)
				m.SetSize(140, 40)
				m.input.SetValue(tt.query)
				m.regex, m.caseSensitive, m.wholeWord = tt.opts.Regex, tt.opts.CaseSensitive, tt.opts.WholeWord
				m.results = []Result{{FilePath: "README.md", Line: 10, Preview: "- `GET /items` lists every item or Item"}}
				if !selected {
					m.results = append(m.results, m.results[0])
					m.cursor = 1
				}
				want := match
				if selected {
					want = current
				}
				cells := resultRow(t, m, "README.md:11")
				text := plainOf(cells)
				highlighted := map[int]bool{}
				for i, c := range cells {
					if c.bg == match || c.bg == current {
						if c.bg != want {
							t.Fatalf("cell %d highlighted with %q, want %q", i, c.bg, want)
						}
						highlighted[i] = true
					}
				}
				// Collect highlighted runs and compare with the expected substrings.
				var runs []string
				for i := 0; i < len(cells); {
					if !highlighted[i] {
						i++
						continue
					}
					j := i
					for highlighted[j] {
						j++
					}
					runs = append(runs, string([]rune(text)[i:j]))
					i = j
				}
				if strings.Join(runs, "|") != strings.Join(tt.want, "|") {
					t.Fatalf("highlighted runs = %q, want %q (row %q)", runs, tt.want, text)
				}
				_ = panel
			})
		}
	}
}

func TestSearchHighlightSurvivesTruncation(t *testing.T) {
	newZones(t)
	theme := ui.DefaultTheme()
	m := New(theme, "/fixture", ModeText)
	m.SetSize(80, 30)
	m.input.SetValue("needle")
	m.results = []Result{
		{FilePath: "a.go", Line: 0, Preview: "needle " + strings.Repeat("x", 200) + " needle"},
		{FilePath: "b.go", Line: 0, Preview: "x"},
	}
	m.cursor = 1
	cells := resultRow(t, m, "a.go:1")
	if got := len(cells); got != m.boxWidth() {
		t.Fatalf("row is %d cells wide, want %d", got, m.boxWidth())
	}
	inner := cells[3 : len(cells)-3]
	if last := inner[len(inner)-1]; last.r == ' ' {
		// The padding ends the content box; the ellipsis must sit just before
		// the right edge of the text area, not be lost.
		t.Fatalf("row %q does not fill the content width", plainOf(inner))
	}
	if !strings.HasSuffix(plainOf(inner), "…") {
		t.Fatalf("truncated row %q does not end with an ellipsis", plainOf(inner))
	}
	if strings.Count(plainOf(inner), "…") != 1 {
		t.Fatalf("truncated row %q has more than one ellipsis", plainOf(inner))
	}
	if last := inner[len(inner)-1]; last.bg != colorKey(theme.Panel.GetBackground()) {
		t.Fatalf("ellipsis carries background %q, want the panel", last.bg)
	}
}

func TestSearchOptionTogglesShowWhichAreOn(t *testing.T) {
	newZones(t)
	theme := ui.DefaultTheme()
	accent := colorKey(theme.PromptAccent.GetForeground())
	muted := colorKey(theme.Muted.GetForeground())
	tests := []struct {
		name         string
		caseOn, word bool
		regexOn      bool
	}{
		{"all off", false, false, false},
		{"case", true, false, false},
		{"word", false, true, false},
		{"regex", false, false, true},
		{"all on", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(theme, "/fixture", ModeText)
			m.SetSize(100, 30)
			m.caseSensitive, m.wholeWord, m.regex = tt.caseOn, tt.word, tt.regexOn
			cells := resultRow(t, m, "Aa Alt+C")
			text := plainOf(cells)
			for label, on := range map[string]bool{"Aa Alt+C": tt.caseOn, "W Alt+W": tt.word, ".* Ctrl+R": tt.regexOn} {
				want := muted
				if on {
					want = accent
				}
				start := runeIndex(text, label)
				for i := start; i < start+len(label); i++ {
					if cells[i].fg != want {
						t.Fatalf("%q cell %d fg = %q, want %q (on=%t)", label, i, cells[i].fg, want, on)
					}
				}
			}
		})
	}
}

func TestSearchScrollHintAlignsWithResultRows(t *testing.T) {
	newZones(t)
	m := stateModel(ui.DefaultTheme(), "scrolling", 100, 20)
	var rowCol, hintCol = -1, -1
	for _, line := range strings.Split(ansi.Strip(zone.Scan(m.View())), "\n") {
		if strings.Contains(line, "README.md:") && rowCol < 0 {
			rowCol = strings.Index(line, "README.md")
		}
		if strings.Contains(line, "results") {
			hintCol = strings.Index(line, "3/40")
		}
	}
	if rowCol < 0 || hintCol < 0 {
		t.Fatalf("row/hint not found:\n%s", ansi.Strip(m.View()))
	}
	if rowCol != hintCol {
		t.Fatalf("hint starts at column %d, result rows at %d", hintCol, rowCol)
	}
}

func TestSearchBoxWidthIsResponsive(t *testing.T) {
	tests := []struct {
		termWidth int
		min, max  int
	}{
		{0, 60, 60},
		{40, 40, 40},
		{80, 60, 80},
		{100, 60, 100},
		{140, 90, 100},
		{300, 100, 100},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.termWidth), func(t *testing.T) {
			m := New(ui.DefaultTheme(), "/fixture", ModeText)
			m.SetSize(tt.termWidth, 24)
			if got := m.boxWidth(); got < tt.min || got > tt.max {
				t.Fatalf("boxWidth(%d) = %d, want %d..%d", tt.termWidth, got, tt.min, tt.max)
			}
		})
	}
}

func TestSearchWideTerminalShowsLongPreviews(t *testing.T) {
	newZones(t)
	preview := "func (s *Store) ListItems(ctx context.Context, f Filter) error"
	m := New(ui.DefaultTheme(), "/fixture", ModeText)
	m.SetSize(140, 40)
	m.input.SetValue("items")
	m.results = []Result{{FilePath: "internal/store/store.go", Line: 41, Preview: preview}}
	view := ansi.Strip(zone.Scan(m.View()))
	if !strings.Contains(view, preview) {
		t.Fatalf("a %d-column preview is still truncated at 140 columns:\n%s", len(preview), view)
	}
	m.SetSize(80, 24)
	if width := ansi.StringWidth(strings.Split(m.View(), "\n")[0]); width > 80 {
		t.Fatalf("box is %d columns wide at 80", width)
	}
}

func TestSearchInputRowKeepsPanelBackgroundWhileTyping(t *testing.T) {
	newZones(t)
	theme := ui.DefaultTheme()
	m := New(theme, "/fixture", ModeText)
	m.SetSize(100, 30)
	_ = m.Focus()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "abc"})
	panel := colorKey(theme.Panel.GetBackground())
	for _, c := range resultRow(t, m, "abc")[1 : m.boxWidth()-1] {
		if c.bg != panel && !c.rev {
			t.Fatalf("input cell %q has background %q, want %q", c.r, c.bg, panel)
		}
	}
}
