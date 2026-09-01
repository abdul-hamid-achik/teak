package editor

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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

// maxFindScanBytes matches the editor's supported file limit. Scans run off the
// UI goroutine and are canceled when the query changes, so every buffer Teak can
// open remains searchable without turning abandoned queries into background
// CPU work.
const maxFindScanBytes = int(text.MaxBufferFileBytes)

var errFindScanLimit = errors.New("find scan exceeds the 64 MiB editor limit")

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
	Err        error
}

// findOriginState snapshots where the editor was when the find widget opened.
// Escaping without navigating to a match rewinds to it, so opening the widget
// by accident never strands the cursor on the first match.
type findOriginState struct {
	cursor     text.Position
	selections []text.Selection
	primary    int
	valid      bool
}

// FindModel manages in-buffer find state.
type FindModel struct {
	input         textinput.Model
	visible       bool
	matches       []FindMatch
	current       int
	regex         bool
	caseSensitive bool
	wholeWord     bool
	theme         ui.Theme
	query         string
	errMsg        string
	searching     bool
	scanContext   context.Context
	scanCancel    context.CancelFunc

	// origin is the pre-find editor state restored on a bail-out.
	origin findOriginState
	// visited records whether the user navigated to a match (Enter/F3). Once
	// they have, Esc leaves the cursor on the current match like VS Code and
	// Helix instead of rewinding to the origin.
	visited bool

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

// SetInputWidth sizes the query field to the editor width so long searches
// are not clipped at the historical 40-column default.
func (f *FindModel) SetInputWidth(width int) {
	if width < 12 {
		width = 12
	}
	f.input.SetWidth(width)
}

// Show opens the find widget.
func (f *FindModel) Show() {
	f.visible = true
	f.input.Focus()
}

// CaptureOrigin snapshots the editor position so a later bail-out can rewind.
// The editor calls it when opening the widget, before any seeded query moves
// the cursor. Selections are copied, not aliased: the live set keeps changing
// while the widget is open.
func (f *FindModel) CaptureOrigin(buf *text.Buffer) {
	if buf == nil {
		return
	}
	origin := findOriginState{cursor: buf.Cursor, valid: true}
	if buf.Selections != nil && buf.Selections.Count() > 0 {
		origin.selections = append([]text.Selection(nil), buf.Selections.All()...)
		origin.primary = buf.Selections.PrimaryIndex()
	} else {
		origin.selections = []text.Selection{{Anchor: buf.Cursor, Head: buf.Cursor}}
	}
	f.origin = origin
	f.visited = false
}

// Hide closes the find widget and clears matches.
func (f *FindModel) Hide() {
	// Invalidate both a pending debounce tick and an in-flight scan. A result
	// that lands after Escape must not repopulate hidden matches or move the
	// editor cursor.
	f.generation++
	f.cancelScan()
	f.visible = false
	f.errMsg = ""
	f.searching = false
	f.origin = findOriginState{}
	f.visited = false
	f.input.Blur()
	// Keep query and matches so F3/Shift+F3 after Esc continue the last find
	// instead of jumping to unrelated project-search results.
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

// Query returns the last find query, including after the widget is hidden.
func (f FindModel) Query() string {
	return f.query
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
				f.visited = true
			}
			return f, nil
		case "shift+f3", "shift+enter":
			if len(f.matches) > 0 {
				f.current = (f.current - 1 + len(f.matches)) % len(f.matches)
				f.visited = true
			}
			return f, nil
		case "ctrl+r":
			f.regex = !f.regex
			return f, f.scheduleScan()
		case "alt+c":
			f.caseSensitive = !f.caseSensitive
			return f, f.scheduleScan()
		case "alt+w":
			f.wholeWord = !f.wholeWord
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
	f.cancelScan()
	f.generation++
	f.errMsg = ""
	// Results describe the previous query or regex mode until the replacement
	// scan completes. Clear them now so UpdateFind cannot move the cursor back
	// to a stale match while the user is still typing.
	f.matches = nil
	f.current = 0
	generation := f.generation
	editorID := f.editorID
	if f.input.Value() == "" {
		// An empty query has no matches; clear immediately rather than making
		// the user wait for a scan that would find nothing.
		f.matches = nil
		f.current = 0
		f.searching = false
		return nil
	}
	f.searching = true
	f.scanContext, f.scanCancel = context.WithCancel(context.Background())
	return tea.Tick(findDebounce, func(time.Time) tea.Msg {
		return FindDebounceMsg{EditorID: editorID, Generation: generation}
	})
}

// ScanCmd returns a command that scans rope off the UI goroutine. The rope is
// immutable, so the command can safely outlive the edit that produced it.
func (f FindModel) ScanCmd(rope *text.Rope, cursor text.Position) tea.Cmd {
	query := f.input.Value()
	opts := search.SearchOpts{Regex: f.regex, CaseSensitive: f.caseSensitive, WholeWord: f.wholeWord}
	generation := f.generation
	editorID := f.editorID
	ctx := f.scanContext
	if ctx == nil {
		ctx = context.Background()
	}
	if query == "" || rope == nil {
		return nil
	}
	return func() tea.Msg {
		matches, current, err := findMatchesContext(ctx, rope, query, opts, cursor)
		return FindResultsMsg{
			EditorID:   editorID,
			Generation: generation,
			Matches:    matches,
			Current:    current,
			Err:        err,
		}
	}
}

// ApplyResults installs a scan result, ignoring one that has been superseded.
func (f *FindModel) ApplyResults(msg FindResultsMsg) bool {
	if !f.visible || msg.Generation != f.generation {
		return false
	}
	f.cancelScan()
	if msg.Err != nil {
		f.matches = nil
		f.current = 0
		f.errMsg = msg.Err.Error()
		f.searching = false
		return true
	}
	f.errMsg = ""
	f.searching = false
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
func findMatches(rope *text.Rope, query string, regex bool, cursor text.Position) ([]FindMatch, int, error) {
	return findMatchesContext(context.Background(), rope, query, search.SearchOpts{Regex: regex}, cursor)
}

func findMatchesContext(ctx context.Context, rope *text.Rope, query string, opts search.SearchOpts, cursor text.Position) ([]FindMatch, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if rope == nil {
		return nil, 0, nil
	}
	if rope.Len() > maxFindScanBytes {
		return nil, 0, errFindScanLimit
	}
	re, err := search.CompilePattern(query, opts)
	if err != nil {
		return nil, 0, err
	}

	var matches []FindMatch
	lineCount := rope.LineCount()

	for line := 0; line < lineCount && len(matches) < maxFindMatches; line++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		lineBytes := rope.Line(line)
		lineStr := string(lineBytes)
		remaining := maxFindMatches - len(matches)
		for _, loc := range re.FindAllStringIndex(lineStr, remaining) {
			matches = append(matches, FindMatch{
				Start: text.Position{Line: line, Col: loc[0]},
				End:   text.Position{Line: line, Col: loc[1]},
			})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	// Position the current match near the cursor.
	current := 0
	for i, m := range matches {
		if m.Start.Line > cursor.Line || (m.Start.Line == cursor.Line && m.Start.Col >= cursor.Col) {
			current = i
			break
		}
	}
	return matches, current, nil
}

func (f *FindModel) cancelScan() {
	if f.scanCancel != nil {
		f.scanCancel()
	}
	f.scanContext = nil
	f.scanCancel = nil
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
	matches, current, err := findMatchesContext(context.Background(), buf.Rope(), f.input.Value(), search.SearchOpts{
		Regex:         f.regex,
		CaseSensitive: f.caseSensitive,
		WholeWord:     f.wholeWord,
	}, buf.Cursor)
	f.matches = matches
	f.current = current
	f.searching = false
	if err != nil {
		f.errMsg = err.Error()
	} else {
		f.errMsg = ""
	}
}

// SeedFromSelection preloads the query with the buffer's primary selection and
// scans immediately, so selecting a word and opening find already shows its
// matches. It reports whether seeding happened. Empty selections are skipped,
// and multiline selections are deliberately not seeded: the scan is per-line,
// so a query spanning lines could never match. In regex mode the literal
// selection is escaped, matching how CompilePattern treats plain queries.
func (f *FindModel) SeedFromSelection(buf *text.Buffer) bool {
	if buf == nil || buf.Selections == nil {
		return false
	}
	sel := buf.Selections.Primary()
	if sel.IsEmpty() {
		return false
	}
	start, end := sel.Ordered()
	if start.Line != end.Line {
		return false
	}
	line := buf.Line(start.Line)
	if end.Col > len(line) {
		return false
	}
	query := string(line[start.Col:end.Col])
	if query == "" {
		return false
	}
	if f.regex {
		query = regexp.QuoteMeta(query)
	}
	// Seed the query only. Scanning a large selection-backed find on the UI
	// goroutine blocked typing; ShowFind schedules the same async path as
	// ordinary typing.
	f.cancelScan()
	f.generation++
	f.input.SetValue(query)
	f.query = query
	return true
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
	s += f.theme.PromptAccent.Render("Find")
	s += "  "
	s += f.input.View()

	flagStyle := f.theme.PromptAccent
	if f.caseSensitive {
		s += "  " + flagStyle.Render("Aa")
	}
	if f.wholeWord {
		s += "  " + flagStyle.Render("W")
	}
	if f.regex {
		s += "  " + flagStyle.Render(".*")
	}

	if f.searching {
		s += "  " + f.theme.PromptMuted.Render("Searching…")
	} else if f.errMsg != "" {
		s += "  " + f.theme.PromptDanger.Render(truncateToWidth(f.errMsg, 40))
	} else if len(f.matches) > 0 {
		s += "  " + f.theme.PromptMuted.Render(formatMatchCount(f.current+1, len(f.matches)))
	} else if f.query != "" {
		s += "  " + f.theme.PromptDanger.Render("No matches")
	}

	return f.theme.StatusBar.Padding(0, 1).Render(s)
}

func formatMatchCount(current, total int) string {
	if total > 999 {
		// Keep the current-of-total position; the capped total is marked with
		// "+" instead of replacing the indicator and losing where the user is.
		return itoa(current) + "/999+"
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
	first := sort.Search(len(f.matches), func(i int) bool {
		return f.matches[i].Start.Line >= line
	})
	last := sort.Search(len(f.matches), func(i int) bool {
		return f.matches[i].Start.Line > line
	})
	if first == last {
		return nil
	}
	ranges := make([]selectionByteRange, 0, last-first)
	for _, m := range f.matches[first:last] {
		ranges = append(ranges, selectionByteRange{start: m.Start.Col, end: m.End.Col})
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
