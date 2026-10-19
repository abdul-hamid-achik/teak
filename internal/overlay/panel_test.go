package overlay

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// rawCells draws a rendered view into a cell grid so tests can assert on the
// foreground/background each terminal cell actually carries.
func rawCells(view string) [][]uv.Cell {
	lines := strings.Split(view, "\n")
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	buf := uv.NewScreenBuffer(width, len(lines))
	uv.NewStyledString(view).Draw(buf, uv.Rect(0, 0, width, len(lines)))
	out := make([][]uv.Cell, len(buf.Lines))
	for i, line := range buf.Lines {
		out[i] = line
	}
	return out
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// rowText returns the plain text of one cell row.
func rowText(row []uv.Cell) string {
	var sb strings.Builder
	for _, c := range row {
		if c.Content == "" {
			continue
		}
		sb.WriteString(c.Content)
	}
	return sb.String()
}

// findText returns the column where text starts in row, or -1.
func findText(row []uv.Cell, text string) int {
	for x := range row {
		if row[x].Content == "" {
			continue
		}
		var sb strings.Builder
		for y := x; y < len(row) && sb.Len() < len(text); y++ {
			sb.WriteString(row[y].Content)
		}
		if strings.HasPrefix(sb.String(), text) {
			return x
		}
	}
	return -1
}

// assertPanelBackground checks that every cell inside the rounded border
// carries one of the allowed backgrounds. Border cells are skipped: they are
// drawn with a foreground only.
func assertPanelBackground(t *testing.T, view string, allowed ...color.Color) {
	t.Helper()
	rows := rawCells(view)
	if len(rows) < 3 {
		t.Fatalf("view too short to contain a bordered box:\n%s", ansi.Strip(view))
	}
	for y := 1; y < len(rows)-1; y++ {
		row := rows[y]
		for x := 1; x < len(row)-1; x++ {
			cell := row[x]
			var bg color.Color
			if cell.Style.Bg != nil {
				bg = cell.Style.Bg
			}
			ok := false
			for _, want := range allowed {
				if sameColor(bg, want) {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("cell (%d,%d) %q has background %v, want one of %v\nrow: %q", x, y, cell.Content, bg, allowed, rowText(row))
			}
		}
	}
}

func TestPickerCellsCarryPanelBackground(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := panelBackground(theme)
	cursorBg := theme.AutocompleteCursor.GetBackground()
	items := []PickerItem{
		{Label: "store.go", Description: "internal/store"},
		{Label: "main.go", Description: "cmd/server"},
		{Label: "README.md"},
	}
	cases := []struct {
		name  string
		setup func(*Picker)
	}{
		{"placeholder", func(p *Picker) {}},
		{"typed", func(p *Picker) {
			p.input.SetValue("stor")
			p.refilter()
		}},
		{"no matches", func(p *Picker) {
			p.input.SetValue("zzzz")
			p.refilter()
		}},
		{"loading", func(p *Picker) { p.itemsPending = true }},
		{"filtering", func(p *Picker) { p.filterPending = true }},
	}
	for _, size := range []struct{ w, h int }{{140, 40}, {80, 24}} {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				p := NewPicker("Open File", items, theme, "bg")
				p.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				tc.setup(p)
				assertPanelBackground(t, p.View(), panel, cursorBg)
			})
		}
	}
}

func TestPickerInputRowMatchesItemRowWidth(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := panelBackground(theme)
	for _, query := range []string{"", "stor"} {
		p := NewPicker("Open File", []PickerItem{{Label: "store.go", Description: "internal/store"}}, theme, "bg")
		p.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
		if query != "" {
			p.input.SetValue(query)
			p.refilter()
		}
		rows := rawCells(p.View())
		inputRow := -1
		for y, row := range rows {
			if strings.Contains(rowText(row), ">") {
				inputRow = y
				break
			}
		}
		if inputRow < 0 {
			t.Fatalf("no input row found for query %q", query)
		}
		itemRow := inputRow + 2
		// The padded interior (between the two padding cells) must be fully on
		// the panel background and the input must span as many cells as an item
		// row does.
		for x := 3; x < len(rows[inputRow])-3; x++ {
			if !sameColor(rows[inputRow][x].Style.Bg, panel) {
				t.Fatalf("query %q: input cell %d has background %v, want panel %v", query, x, rows[inputRow][x].Style.Bg, panel)
			}
		}
		if got, want := lipgloss.Width(strings.TrimRight(rowText(rows[inputRow]), " │")), lipgloss.Width(strings.TrimRight(rowText(rows[itemRow]), " │")); got == 0 || want == 0 {
			t.Fatalf("query %q: empty rows (input %d, item %d)", query, got, want)
		}
	}
}

func TestPickerMutedDescriptionKeepsRowBackground(t *testing.T) {
	theme := ui.DefaultTheme()
	items := []PickerItem{
		{Label: "store.go", Description: "internal/store"},
		{Label: "main.go", Description: "cmd/server"},
	}
	p := NewPicker("Open File", items, theme, "muted")
	p.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	rows := rawCells(p.View())

	find := func(label string) (labelCell, descCell uv.Cell) {
		t.Helper()
		for _, row := range rows {
			x := findText(row, label)
			if x < 0 {
				continue
			}
			descStart := x + len([]rune(label)) + 2
			return row[x], row[descStart]
		}
		t.Fatalf("row with %q not found", label)
		return
	}

	muted := theme.Muted.GetForeground()
	text := theme.AutocompleteItem.GetForeground()
	cursorLabel, cursorDesc := find("store.go")
	if !sameColor(cursorDesc.Style.Fg, muted) {
		t.Errorf("cursor row description fg = %v, want muted %v", cursorDesc.Style.Fg, muted)
	}
	if !sameColor(cursorDesc.Style.Bg, theme.AutocompleteCursor.GetBackground()) {
		t.Errorf("cursor row description bg = %v, want cursor bg", cursorDesc.Style.Bg)
	}
	if sameColor(cursorLabel.Style.Fg, cursorDesc.Style.Fg) {
		t.Errorf("cursor row label and description share fg %v", cursorLabel.Style.Fg)
	}

	normalLabel, normalDesc := find("main.go")
	if !sameColor(normalDesc.Style.Fg, muted) {
		t.Errorf("row description fg = %v, want muted %v", normalDesc.Style.Fg, muted)
	}
	if !sameColor(normalDesc.Style.Bg, theme.AutocompleteItem.GetBackground()) {
		t.Errorf("row description bg = %v, want item bg", normalDesc.Style.Bg)
	}
	if !sameColor(normalLabel.Style.Fg, text) {
		t.Errorf("row label fg = %v, want text %v", normalLabel.Style.Fg, text)
	}
	if sameColor(normalLabel.Style.Fg, normalDesc.Style.Fg) {
		t.Errorf("description is not dimmer than the label: both %v", normalLabel.Style.Fg)
	}
}

func TestPickerStatusLinesUseMutedForeground(t *testing.T) {
	theme := ui.DefaultTheme()
	muted := theme.Muted.GetForeground()
	for _, tc := range []struct {
		text  string
		setup func(*Picker)
	}{
		{"No matches", func(p *Picker) { p.input.SetValue("zzzz"); p.refilter() }},
		{"Loading...", func(p *Picker) { p.itemsPending = true }},
		{"Filtering...", func(p *Picker) { p.filterPending = true }},
	} {
		p := NewPicker("Open File", []PickerItem{{Label: "a"}}, theme, "status")
		p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		tc.setup(p)
		found := false
		for _, row := range rawCells(p.View()) {
			if x := findText(row, tc.text); x >= 0 {
				found = true
				if !sameColor(row[x].Style.Fg, muted) {
					t.Errorf("%q fg = %v, want muted %v", tc.text, row[x].Style.Fg, muted)
				}
			}
		}
		if !found {
			t.Errorf("%q not rendered", tc.text)
		}
	}
}

func TestPickerFooterRightAlignsCount(t *testing.T) {
	theme := ui.DefaultTheme()
	for _, total := range []int{30, 300, 3000} {
		items := make([]PickerItem, total)
		for i := range items {
			items[i] = PickerItem{Label: "item"}
		}
		p := NewPicker("Open File", items, theme, "footer")
		p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		p.SetCursor(total - 1)
		rows := rawCells(p.View())
		want := countStr(total, total)
		found := false
		for _, row := range rows {
			text := rowText(row)
			if !strings.Contains(text, want) {
				continue
			}
			found = true
			// Right edge of the content area: border + 2 padding cells on the
			// right side leave the count ending 3 cells before the last column.
			end := findText(row, want) + len(want)
			if got := len(row) - 3; end != got {
				t.Errorf("total %d: count ends at column %d, want %d (content right edge)", total, end, got)
			}
		}
		if !found {
			t.Errorf("total %d: footer %q not rendered", total, want)
		}
	}
}

func TestInputCellsCarryPanelBackground(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := panelBackground(theme)
	for _, initial := range []string{"", "feature/login"} {
		in := NewInput("Branch name", initial, theme)
		in.Focus()
		in.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		in.input.Placeholder = "type here"
		assertPanelBackground(t, in.View(), panel)
	}
}

func TestFloatCellsCarryPanelBackground(t *testing.T) {
	theme := ui.DefaultTheme()
	f := NewFloat(1, "Status", "line one\nline two", 40, 6)
	f.SetTheme(theme)
	f.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assertPanelBackground(t, f.View(), panelBackground(theme))
}

func TestInputAndFloatHintsUseMutedForeground(t *testing.T) {
	theme := ui.DefaultTheme()
	muted := theme.Muted.GetForeground()
	check := func(name, view, text string) {
		t.Helper()
		for _, row := range rawCells(view) {
			if x := findText(row, text); x >= 0 {
				if !sameColor(row[x].Style.Fg, muted) {
					t.Errorf("%s hint fg = %v, want muted %v", name, row[x].Style.Fg, muted)
				}
				return
			}
		}
		t.Errorf("%s hint %q not rendered", name, text)
	}
	check("input", NewInput("Name", "", theme).View(), "Enter to accept")
	f := NewFloat(1, "T", "body", 40, 4)
	f.SetTheme(theme)
	check("float", f.View(), "Enter or Esc")
}

func TestConfirmCellsCarryPanelBackground(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := panelBackground(theme)
	for _, width := range []int{140, 80, 40} {
		c := NewConfirm("Unsaved Changes", "You have 2 unsaved file(s):", []string{"a.go", "b.go"}, quitButtons(theme), theme)
		c.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := c.View()
		allowed := []color.Color{
			panel,
			theme.GitActionButton.GetBackground(),
			theme.GitPushPullButton.GetBackground(),
			theme.GitCommitButton.GetBackground(),
		}
		assertPanelBackground(t, view, allowed...)
		// Everything outside the button labels must be on the panel proper.
		for y, row := range rawCells(view) {
			if y == 0 || y == len(rawCells(view))-1 {
				continue
			}
			text := rowText(row)
			if strings.Contains(text, "Quit") || strings.Contains(text, "Cancel") || strings.Contains(text, "Save All") {
				continue
			}
			for x := 1; x < len(row)-1; x++ {
				if !sameColor(row[x].Style.Bg, panel) {
					t.Fatalf("width %d: non-button cell (%d,%d) bg %v, want panel", width, x, y, row[x].Style.Bg)
				}
			}
		}
	}
}

func quitButtons(theme ui.Theme) []Button {
	return []Button{
		{Label: "Save All & Quit", Style: theme.GitCommitButton.Padding(0, 2), Action: ButtonAction{Label: "save"}},
		{Label: "Quit Without Saving", Style: theme.PromptDanger.Padding(0, 2), Action: ButtonAction{Label: "quit"}},
		{Label: "Cancel", Action: ButtonAction{Label: "cancel"}},
	}
}

// buttonRows returns, per button label, the row it renders on and the start column.
func buttonRows(t *testing.T, view string, labels ...string) (rows, cols []int) {
	t.Helper()
	cells := rawCells(view)
	for _, label := range labels {
		row, col := -1, -1
		for y, r := range cells {
			if x := findText(r, label); x >= 0 {
				row, col = y, x
				break
			}
		}
		if row < 0 {
			t.Fatalf("button %q not rendered:\n%s", label, ansi.Strip(view))
		}
		rows = append(rows, row)
		cols = append(cols, col)
	}
	return rows, cols
}

func TestConfirmButtonsNeverWrapMidRow(t *testing.T) {
	theme := ui.DefaultTheme()
	labels := []string{"Save All & Quit", "Quit Without Saving", "Cancel"}
	for _, size := range []struct{ w, h int }{{140, 40}, {80, 24}, {60, 24}, {40, 24}} {
		c := NewConfirm("Unsaved Changes", "You have 2 unsaved file(s):", []string{"a.go"}, quitButtons(theme), theme)
		c.SetWidth(min(50, size.w)) // mirrors the app's resize handling
		c.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		view := c.View()
		if got := lipgloss.Width(view); got > size.w {
			t.Errorf("%dx%d: dialog is %d cells wide, wider than the terminal", size.w, size.h, got)
		}
		rows, cols := buttonRows(t, view, labels...)
		sameRow := rows[0] == rows[1] && rows[1] == rows[2]
		stacked := rows[0] < rows[1] && rows[1] < rows[2]
		if !sameRow && !stacked {
			t.Errorf("%dx%d: buttons wrap mid-row (rows %v)", size.w, size.h, rows)
		}
		if size.w >= 80 && !sameRow {
			t.Errorf("%dx%d: buttons fit on one row but were not laid out on one (rows %v)", size.w, size.h, rows)
		}
		if size.w == 40 && !stacked {
			t.Errorf("%dx%d: buttons cannot fit on one row and must stack (rows %v)", size.w, size.h, rows)
		}
		if stacked && (cols[0] != cols[1] || cols[1] != cols[2]) {
			t.Errorf("%dx%d: stacked buttons are not aligned (cols %v)", size.w, size.h, cols)
		}
		// No label may be cut by the box edge.
		plain := ansi.Strip(view)
		for _, label := range labels {
			if !strings.Contains(plain, label) {
				t.Errorf("%dx%d: label %q was cut", size.w, size.h, label)
			}
		}
	}
}

func TestConfirmHorizontalAndStackedKeepFocusAndKeys(t *testing.T) {
	theme := ui.DefaultTheme()
	for _, width := range []int{140, 40} {
		c := NewConfirm("Unsaved Changes", "msg", nil, quitButtons(theme), theme)
		c.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		focused := theme.GitPushPullButton.GetBackground()
		for want := range 3 {
			cells := rawCells(c.View())
			count := 0
			for _, row := range cells {
				for _, cell := range row {
					if sameColor(cell.Style.Bg, focused) && cell.Content != "" {
						count++
					}
				}
			}
			if count == 0 {
				t.Fatalf("width %d: no focused button highlight with cursor %d", width, want)
			}
			if c.cursor != want {
				t.Fatalf("width %d: cursor = %d, want %d", width, c.cursor, want)
			}
			c.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		}
	}
}
