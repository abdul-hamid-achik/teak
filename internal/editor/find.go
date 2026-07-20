package editor

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/search"
	"teak/internal/text"
	"teak/internal/ui"
)

// FindMatch is a single match within the buffer.
type FindMatch struct {
	Start text.Position
	End   text.Position
}

// FindModel manages in-buffer find state.
type FindModel struct {
	input     textinput.Model
	visible   bool
	matches   []FindMatch
	current   int
	regex     bool
	theme     ui.Theme
	query     string
}

// NewFindModel creates a new in-buffer find widget.
func NewFindModel(theme ui.Theme) FindModel {
	ti := textinput.New()
	ti.Placeholder = "Find..."
	ti.CharLimit = 256
	ti.SetWidth(40)
	return FindModel{
		input: ti,
		theme: theme,
	}
}

// Show opens the find widget.
func (f *FindModel) Show() {
	f.visible = true
	f.input.Focus()
}

// Hide closes the find widget and clears matches.
func (f *FindModel) Hide() {
	f.visible = false
	f.matches = nil
	f.current = 0
	f.input.Blur()
}

// Visible returns whether the find widget is open.
func (f FindModel) Visible() bool {
	return f.visible
}

// Matches returns the current matches.
func (f FindModel) Matches() []FindMatch {
	return f.matches
}

// CurrentMatch returns the current match index (0-based), or -1 if none.
func (f FindModel) CurrentMatch() int {
	if len(f.matches) == 0 {
		return -1
	}
	return f.current
}

// MatchCount returns the total number of matches.
func (f FindModel) MatchCount() int {
	return len(f.matches)
}

// Update handles keyboard input for the find widget.
func (f FindModel) Update(msg tea.Msg, buf *text.Buffer) (FindModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			f.Hide()
			return f, nil
		case "enter", "f3":
			if len(f.matches) > 0 {
				f.current = (f.current + 1) % len(f.matches)
			}
			return f, nil
		case "shift+f3", "shift+enter":
			if len(f.matches) > 0 {
				f.current = (f.current - 1 + len(f.matches)) % len(f.matches)
			}
			return f, nil
		case "ctrl+r":
			f.regex = !f.regex
			f.updateMatches(buf)
			return f, nil
		}
	}

	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	newQuery := f.input.Value()
	if newQuery != f.query {
		f.query = newQuery
		f.updateMatches(buf)
	}
	return f, cmd
}

// updateMatches recomputes matches from the buffer content.
func (f *FindModel) updateMatches(buf *text.Buffer) {
	f.matches = nil
	f.current = 0
	query := f.input.Value()
	if query == "" || buf == nil {
		return
	}

	re, err := search.CompilePattern(query, search.SearchOpts{Regex: f.regex})
	if err != nil {
		return
	}

	rope := buf.Rope()
	lineCount := rope.LineCount()
	const maxMatches = 10000

	for line := 0; line < lineCount && len(f.matches) < maxMatches; line++ {
		lineBytes := rope.Line(line)
		lineStr := string(lineBytes)
		locs := re.FindAllStringIndex(lineStr, -1)
		for _, loc := range locs {
			if len(f.matches) >= maxMatches {
				break
			}
			f.matches = append(f.matches, FindMatch{
				Start: text.Position{Line: line, Col: loc[0]},
				End:   text.Position{Line: line, Col: loc[1]},
			})
		}
	}

	// Position current match near the cursor
	if len(f.matches) > 0 {
		cursor := buf.Cursor
		for i, m := range f.matches {
			if m.Start.Line > cursor.Line || (m.Start.Line == cursor.Line && m.Start.Col >= cursor.Col) {
				f.current = i
				break
			}
		}
	}
}

// CurrentMatchPosition returns the position of the current match, or nil.
func (f FindModel) CurrentMatchPosition() *FindMatch {
	if len(f.matches) == 0 || f.current >= len(f.matches) {
		return nil
	}
	return &f.matches[f.current]
}

// View renders the find widget as a single line.
func (f FindModel) View() string {
	if !f.visible {
		return ""
	}

	var s string
	s += lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true).Render("Find")
	s += "  "
	s += f.input.View()

	if f.regex {
		s += "  " + lipgloss.NewStyle().Foreground(ui.Nord14).Bold(true).Render(".*")
	}

	if len(f.matches) > 0 {
		s += "  " + lipgloss.NewStyle().Foreground(ui.Nord4).Render(
			formatMatchCount(f.current+1, len(f.matches)))
	} else if f.query != "" {
		s += "  " + lipgloss.NewStyle().Foreground(ui.Nord11).Render("No matches")
	}

	return lipgloss.NewStyle().
		Background(ui.Nord1).
		Padding(0, 1).
		Render(s)
}

func formatMatchCount(current, total int) string {
	if total > 999 {
		return "999+ matches"
	}
	return itoa(current) + "/" + itoa(total)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// FindMatchRangesForLine returns the byte ranges of find matches on the given line.
func (f FindModel) FindMatchRangesForLine(line int) []selectionByteRange {
	if len(f.matches) == 0 {
		return nil
	}
	var ranges []selectionByteRange
	for i, m := range f.matches {
		if m.Start.Line > line {
			break
		}
		if m.Start.Line == line {
			ranges = append(ranges, selectionByteRange{start: m.Start.Col, end: m.End.Col})
			_ = i
		}
	}
	return ranges
}

// CurrentMatchRangesForLine returns the byte range of the current match if on the given line.
func (f FindModel) CurrentMatchRangesForLine(line int) []selectionByteRange {
	m := f.CurrentMatchPosition()
	if m == nil || m.Start.Line != line {
		return nil
	}
	return []selectionByteRange{{start: m.Start.Col, end: m.End.Col}}
}
