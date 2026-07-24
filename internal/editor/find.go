package editor

import (
	"time"

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

// findDebounce is how long typing must pause before the buffer is scanned.
// Matching the search panel's cadence keeps the two find experiences aligned.
const findDebounce = 120 * time.Millisecond

// maxFindMatches bounds how many matches are retained.
const maxFindMatches = 10000

// maxFindScanBytes bounds how much of the document a single scan reads. A query
// with no matches used to walk the whole buffer regardless of the match cap, so
// the cost was unbounded on large files even when nothing was found.
const maxFindScanBytes = 8 << 20

// FindDebounceMsg fires after typing pauses and asks for a scan.
type FindDebounceMsg struct {
	EditorID   uint64
	Generation int
}

// FindResultsMsg carries the outcome of an asynchronous buffer scan.
type FindResultsMsg struct {
	EditorID   uint64
	Generation int
	Matches    []FindMatch
	Current    int
}

// FindModel manages in-buffer find state.
type FindModel struct {
	input   textinput.Model
	visible bool
	matches []FindMatch
	current int
	regex   bool
	theme   ui.Theme
	query   string

	// generation increments on every query change so results from a superseded
	// scan can be discarded rather than overwriting newer ones.
	generation int
	// editorID lets a scan's result find its way back to the right editor.
	editorID uint64
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
			return f, f.scheduleScan()
		}
	}

	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	newQuery := f.input.Value()
	if newQuery != f.query {
		f.query = newQuery
		// Scanning here used to run synchronously inside Update, recompiling the
		// pattern and walking every line of the document on each keystroke —
		// 64ms of blocked event loop per character on a 50k-line file.
		return f, tea.Batch(cmd, f.scheduleScan())
	}
	return f, cmd
}

// scheduleScan invalidates any in-flight scan and asks for a fresh one after a
// short pause. Results carry the generation so a slow scan that lands after the
// query moved on is dropped instead of overwriting newer matches.
func (f *FindModel) scheduleScan() tea.Cmd {
	f.generation++
	generation := f.generation
	editorID := f.editorID
	if f.input.Value() == "" {
		// An empty query has no matches; clear immediately rather than making
		// the user wait for a scan that would find nothing.
		f.matches = nil
		f.current = 0
		return nil
	}
	return tea.Tick(findDebounce, func(time.Time) tea.Msg {
		return FindDebounceMsg{EditorID: editorID, Generation: generation}
	})
}

// ScanCmd returns a command that scans rope off the UI goroutine. The rope is
// immutable, so the command can safely outlive the edit that produced it.
func (f FindModel) ScanCmd(rope *text.Rope, cursor text.Position) tea.Cmd {
	query := f.input.Value()
	regex := f.regex
	generation := f.generation
	editorID := f.editorID
	if query == "" || rope == nil {
		return nil
	}
	return func() tea.Msg {
		matches, current := findMatches(rope, query, regex, cursor)
		return FindResultsMsg{
			EditorID:   editorID,
			Generation: generation,
			Matches:    matches,
			Current:    current,
		}
	}
}

// ApplyResults installs a scan result, ignoring one that has been superseded.
func (f *FindModel) ApplyResults(msg FindResultsMsg) bool {
	if msg.Generation != f.generation {
		return false
	}
	f.matches = msg.Matches
	f.current = msg.Current
	return true
}

// Generation returns the current query generation.
func (f FindModel) Generation() int { return f.generation }

// SetEditorID binds this widget to its owning editor so results can be routed.
func (f *FindModel) SetEditorID(id uint64) { f.editorID = id }

// findMatches scans rope for query. It is a pure function so it can run on a
// background goroutine.
func findMatches(rope *text.Rope, query string, regex bool, cursor text.Position) ([]FindMatch, int) {
	re, err := search.CompilePattern(query, search.SearchOpts{Regex: regex})
	if err != nil {
		return nil, 0
	}

	var matches []FindMatch
	lineCount := rope.LineCount()
	scanned := 0

	for line := 0; line < lineCount && len(matches) < maxFindMatches; line++ {
		lineBytes := rope.Line(line)
		scanned += len(lineBytes) + 1
		if scanned > maxFindScanBytes {
			break
		}
		lineStr := string(lineBytes)
		for _, loc := range re.FindAllStringIndex(lineStr, -1) {
			if len(matches) >= maxFindMatches {
				break
			}
			matches = append(matches, FindMatch{
				Start: text.Position{Line: line, Col: loc[0]},
				End:   text.Position{Line: line, Col: loc[1]},
			})
		}
	}

	// Position the current match near the cursor.
	current := 0
	for i, m := range matches {
		if m.Start.Line > cursor.Line || (m.Start.Line == cursor.Line && m.Start.Col >= cursor.Col) {
			current = i
			break
		}
	}
	return matches, current
}

// updateMatches scans immediately. Reserved for callers that must have
// matches before returning (opening the widget over a selection); typing goes
// through the debounced path instead.
func (f *FindModel) updateMatches(buf *text.Buffer) {
	if buf == nil || f.input.Value() == "" {
		f.matches = nil
		f.current = 0
		return
	}
	f.matches, f.current = findMatches(buf.Rope(), f.input.Value(), f.regex, buf.Cursor)
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
