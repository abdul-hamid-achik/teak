package search

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// DebounceTickMsg is sent after the debounce delay to trigger a search. It is
// exported because the root Bubble Tea model owns delivery of asynchronous
// messages to the visible overlay.
type DebounceTickMsg struct {
	Generation int
}

// Mode indicates the search type.
type Mode int

const (
	ModeText     Mode = iota // grep/regex
	ModeSemantic             // vecgrep
)

// Result represents a single search result.
type Result struct {
	FilePath   string
	Line       int
	Col        int
	Preview    string
	Score      float64
	SymbolName string // from vecgrep: function/type name containing the match
	ChunkType  string // from vecgrep: function, type, file, etc.
	EndLine    int    // from vecgrep: end line of the matched chunk
}

// OpenResultMsg is sent when a result is selected. Index is the selected
// result's position in the overlay's result list, so result navigation
// (F3/Shift+F3) can continue from the entry the user opened.
type OpenResultMsg struct {
	FilePath string
	Line     int
	Col      int
	Index    int
}

// CloseSearchMsg is sent when the search overlay should close.
type CloseSearchMsg struct{}

// ToggleReplaceMsg is sent to toggle the replace input visibility.
type ToggleReplaceMsg struct{}

// ReplaceOneMsg requests replacing the first match from cursor in the active
// file. Regex and CaseSensitive mirror the overlay's search options so the
// replacement matches the same occurrences the search highlighted.
type ReplaceOneMsg struct {
	Query         string
	Replacement   string
	Regex         bool
	CaseSensitive bool
	WholeWord     bool
}

// ReplaceAllMsg requests replacing all matches across the current project
// result set. When the overlay has no results yet, the app falls back to the
// active file.
type ReplaceAllMsg struct {
	Query         string
	Replacement   string
	Regex         bool
	CaseSensitive bool
	WholeWord     bool
}

// SearchIndexingMsg is sent when semantic search starts indexing.
type SearchIndexingMsg struct {
	Generation int
}

// SearchResultsMsg is sent when results arrive from a search.
type SearchResultsMsg struct {
	Results    []Result
	Err        error
	Generation int
}

// Model is the search overlay model.
type Model struct {
	input         textinput.Model
	mode          Mode
	results       []Result
	cursor        int
	scrollY       int // scroll offset for results
	theme         ui.Theme
	width         int
	height        int
	rootDir       string
	lastQuery     string
	searching     bool
	indexing      bool
	indexed       bool // true after first successful semantic search
	spinner       spinner.Model
	errMsg        string
	debounceGen   int // generation counter for debounce
	searchContext context.Context
	searchCancel  context.CancelFunc

	replaceInput  textinput.Model
	showReplace   bool
	focusedInput  int // 0=search, 1=replace
	regex         bool
	caseSensitive bool
	wholeWord     bool
}

// New creates a new search model.
func New(theme ui.Theme, rootDir string, mode Mode) Model {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 256
	ti.SetWidth(50)
	applyPanelInputTheme(&ti, theme)

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(onPanel(theme, theme.Muted)),
	)

	ri := textinput.New()
	ri.Placeholder = "Replace in project results..."
	ri.CharLimit = 256
	ri.SetWidth(50)
	applyPanelInputTheme(&ri, theme)

	return Model{
		input:        ti,
		replaceInput: ri,
		mode:         mode,
		theme:        theme,
		rootDir:      rootDir,
		spinner:      sp,
	}
}

// SetTheme updates input and spinner styles without clearing a search.
func (m *Model) SetTheme(theme ui.Theme) {
	m.theme = theme
	applyPanelInputTheme(&m.input, theme)
	applyPanelInputTheme(&m.replaceInput, theme)
	m.spinner.Style = onPanel(theme, theme.Muted)
}

// onPanel returns style on the search box background. Lipgloss styles end with
// a reset, so every segment drawn inside the box has to carry the background
// itself or it paints a dark chip into the panel.
func onPanel(theme ui.Theme, style lipgloss.Style) lipgloss.Style {
	return style.Background(theme.SearchBox.GetBackground())
}

// applyPanelInputTheme is ui.ApplyTextInputTheme with the panel's text color:
// the input sits on the box, not on the editor. Its background is painted by
// paintBackground, because Bubbles renders the idle cursor cell unstyled.
func applyPanelInputTheme(input *textinput.Model, theme ui.Theme) {
	ui.ApplyTextInputTheme(input, theme)
	styles := input.Styles()
	styles.Focused.Text = styles.Focused.Text.Foreground(theme.Panel.GetForeground())
	styles.Blurred.Text = styles.Blurred.Text.Foreground(theme.Panel.GetForeground())
	input.SetStyles(styles)
}

// paintBackground sets bg for s and re-applies it after every SGR reset, so
// cells that a component renders without any style (Bubbles draws its idle
// cursor cell that way) still sit on the panel instead of showing the
// terminal default.
func paintBackground(s string, bg color.Color) string {
	r, g, b, _ := bg.RGBA()
	set := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	var sb strings.Builder
	sb.WriteString(set)
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				sb.WriteString(s[i : j+1])
				if params := s[i+2 : j]; s[j] == 'm' && (params == "" || params == "0") {
					sb.WriteString(set)
				}
				i = j + 1
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// Focus focuses the text input and returns the cursor blink command.
func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

// SetShowReplace enables or disables the replace input row.
func (m *Model) SetShowReplace(show bool) {
	m.showReplace = show
}

// ShowReplace reports whether the project-search overlay is in replace mode.
func (m Model) ShowReplace() bool {
	return m.showReplace
}

// Results returns the current search results.
func (m Model) Results() []Result {
	return m.results
}

// Query returns the current search query.
func (m Model) Query() string {
	return m.input.Value()
}

// Replacement returns the current replacement text.
func (m Model) Replacement() string {
	return m.replaceInput.Value()
}

// Regex reports whether regex mode is active in the overlay.
func (m Model) Regex() bool {
	return m.regex
}

// PatternOpts returns the compile options that produced the current results.
func (m Model) PatternOpts() SearchOpts {
	return SearchOpts{Regex: m.regex, CaseSensitive: m.caseSensitive, WholeWord: m.wholeWord}
}

// SetSize sets the overlay dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.input.SetWidth(max(1, m.innerWidth()-lipgloss.Width(m.input.Prompt)-1))
	m.replaceInput.SetWidth(max(1, m.innerWidth()-4-lipgloss.Width(m.replaceInput.Prompt)-1))
	m.ensureCursorVisible()
}

// OverlayOrigin returns the top-left terminal cell used by View when it is
// centered on a canvas of the provided size. App uses this to translate mouse
// events from terminal coordinates to the overlay's local coordinates.
func (m Model) OverlayOrigin(canvasWidth, canvasHeight int) (int, int) {
	view := m.View()
	x := (canvasWidth - lipgloss.Width(view)) / 2
	y := (canvasHeight - len(strings.Split(view, "\n"))) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// headerLines returns the number of lines before results in the search overlay view.
func (m Model) headerLines() int {
	return m.theme.SearchBox.GetBorderTopSize() + m.theme.SearchBox.GetPaddingTop() + strings.Count(m.headerView(false), "\n")
}

// Update handles input messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			m.cancelSearch()
			return m, func() tea.Msg { return CloseSearchMsg{} }
		case "enter":
			if m.showReplace && m.focusedInput == 1 {
				query := m.input.Value()
				replacement := m.replaceInput.Value()
				if query != "" {
					return m, func() tea.Msg {
						return ReplaceOneMsg{Query: query, Replacement: replacement, Regex: m.regex, CaseSensitive: m.caseSensitive, WholeWord: m.wholeWord}
					}
				}
				return m, nil
			}
			if len(m.results) > 0 && m.cursor < len(m.results) {
				r := m.results[m.cursor]
				index := m.cursor
				return m, func() tea.Msg {
					return OpenResultMsg{
						FilePath: r.FilePath,
						Line:     r.Line,
						Col:      r.Col,
						Index:    index,
					}
				}
			}
			return m, nil
		case "ctrl+shift+enter":
			if m.showReplace {
				query := m.input.Value()
				replacement := m.replaceInput.Value()
				if query != "" {
					return m, func() tea.Msg {
						return ReplaceAllMsg{Query: query, Replacement: replacement, Regex: m.regex, CaseSensitive: m.caseSensitive, WholeWord: m.wholeWord}
					}
				}
			}
			return m, nil
		case "tab", "shift+tab":
			// Focus cycling between the find and replace inputs belongs to
			// Tab/Shift+Tab only. Up/down always move the result cursor; when
			// they cycled focus instead, keyboard result navigation became
			// unreachable in replace mode. Tab moves forward (find → replace),
			// Shift+Tab moves back, so the pair stays directional.
			if m.showReplace {
				if m.focusedInput == 0 && msg.String() == "tab" {
					m.focusedInput = 1
					m.input.Blur()
					return m, m.replaceInput.Focus()
				}
				if m.focusedInput == 1 && msg.String() == "shift+tab" {
					m.focusedInput = 0
					m.replaceInput.Blur()
					return m, m.input.Focus()
				}
				return m, nil
			}
			if msg.String() != "tab" {
				return m, nil
			}
			if m.mode == ModeText {
				m.mode = ModeSemantic
			} else {
				m.mode = ModeText
			}
			// Invalidate any pending result for the previous mode before starting
			// a search in the newly selected one.
			m.debounceGen++
			m.replaceSearchContext()
			m.searching = m.input.Value() != ""
			m.indexing = false
			m.errMsg = ""
			// Only re-search if there's a query
			m.results = nil
			m.cursor = 0
			m.scrollY = 0
			if m.input.Value() != "" {
				return m, m.dispatchSearch()
			}
			m.cancelSearch()
			return m, nil
		case "ctrl+r", "alt+c", "alt+w":
			if m.mode == ModeText {
				switch msg.String() {
				case "ctrl+r":
					m.regex = !m.regex
				case "alt+c":
					m.caseSensitive = !m.caseSensitive
				case "alt+w":
					m.wholeWord = !m.wholeWord
				}
				if m.input.Value() != "" {
					m.debounceGen++
					m.replaceSearchContext()
					m.results = nil
					m.cursor = 0
					m.scrollY = 0
					m.errMsg = ""
					return m, m.dispatchSearch()
				}
			}
			return m, nil
		case "up":
			if m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}
			return m, nil
		case "down":
			if m.cursor < len(m.results)-1 {
				m.cursor++
				m.ensureCursorVisible()
			}
			return m, nil
		}

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			row := mouse.Y - m.headerLines()
			clickedIdx := m.scrollY + row
			if row >= 0 && row < m.maxVisibleResults() && clickedIdx < len(m.results) {
				r := m.results[clickedIdx]
				index := clickedIdx
				return m, func() tea.Msg {
					return OpenResultMsg{
						FilePath: r.FilePath,
						Line:     r.Line,
						Col:      r.Col,
						Index:    index,
					}
				}
			}
		}
		return m, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		visible := m.maxVisibleResults()
		maxScroll := len(m.results) - visible
		if maxScroll < 0 {
			maxScroll = 0
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
		case tea.MouseWheelDown:
			m.scrollY += 3
			if m.scrollY > maxScroll {
				m.scrollY = maxScroll
			}
		}
		return m, nil

	case SearchIndexingMsg:
		if msg.Generation != m.debounceGen {
			return m, nil
		}
		m.indexing = true
		return m, m.spinner.Tick

	case spinner.TickMsg:
		if m.indexing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case SearchResultsMsg:
		if msg.Generation != m.debounceGen {
			return m, nil
		}
		m.cancelSearch()
		m.searching = false
		m.indexing = false
		if msg.Err != nil {
			m.errMsg = msg.Err.Error()
			m.results = nil
		} else {
			m.errMsg = ""
			m.results = msg.Results
			if m.mode == ModeSemantic {
				m.indexed = true
			}
		}
		m.cursor = 0
		m.scrollY = 0
		return m, nil

	case DebounceTickMsg:
		if msg.Generation == m.debounceGen {
			return m, m.dispatchSearch()
		}
		return m, nil
	}

	// Forward to the focused input
	var cmd tea.Cmd
	if m.showReplace && m.focusedInput == 1 {
		m.replaceInput, cmd = m.replaceInput.Update(msg)
		return m, cmd
	}

	m.input, cmd = m.input.Update(msg)

	// Check if query changed
	query := m.input.Value()
	if query != m.lastQuery {
		m.lastQuery = query
		// Each edit invalidates both pending debounce timers and any in-flight
		// command. Searches are intentionally not allowed to replace results
		// for newer input when they eventually finish.
		m.debounceGen++
		m.cancelSearch()
		m.searching = query != ""
		m.indexing = false
		m.errMsg = ""
		if query == "" {
			m.results = nil
			m.cursor = 0
			m.scrollY = 0
			return m, cmd
		}
		m.replaceSearchContext()
		// Debounce the current generation.
		gen := m.debounceGen
		debounceCmd := tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
			return DebounceTickMsg{Generation: gen}
		})
		return m, tea.Batch(cmd, debounceCmd)
	}

	return m, cmd
}

// dispatchSearch picks the right search command based on mode.
func (m Model) dispatchSearch() tea.Cmd {
	if m.mode == ModeSemantic {
		return m.doSearchSemantic()
	}
	return m.doSearch()
}

func (m Model) doSearch() tea.Cmd {
	query := m.input.Value()
	if query == "" {
		return nil
	}
	mode := m.mode
	rootDir := m.rootDir
	generation := m.debounceGen
	ctx := m.searchContext
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		var results []Result
		var err error
		if mode == ModeSemantic {
			results, err = SemanticSearchContext(ctx, rootDir, query)
		} else {
			results, err = TextSearchContext(ctx, rootDir, query, SearchOpts{Regex: m.regex, CaseSensitive: m.caseSensitive, WholeWord: m.wholeWord})
		}
		return SearchResultsMsg{Results: results, Err: err, Generation: generation}
	}
}

// doSearchSemantic sends an indexing indicator (first time only) then runs the search.
func (m Model) doSearchSemantic() tea.Cmd {
	query := m.input.Value()
	if query == "" {
		return nil
	}
	rootDir := m.rootDir
	generation := m.debounceGen
	ctx := m.searchContext
	if ctx == nil {
		ctx = context.Background()
	}
	if !m.indexed {
		return tea.Sequence(
			func() tea.Msg { return SearchIndexingMsg{Generation: generation} },
			func() tea.Msg {
				results, err := SemanticSearchContext(ctx, rootDir, query)
				return SearchResultsMsg{Results: results, Err: err, Generation: generation}
			},
		)
	}
	return func() tea.Msg {
		results, err := SemanticSearchContext(ctx, rootDir, query)
		return SearchResultsMsg{Results: results, Err: err, Generation: generation}
	}
}

func (m *Model) cancelSearch() {
	if m.searchCancel != nil {
		m.searchCancel()
	}
	m.searchContext = nil
	m.searchCancel = nil
}

// Cancel stops the active search and invalidates any result already queued for
// delivery. It is idempotent so overlay close and application teardown may
// safely converge on it. In particular, callers must cancel search waiters
// before CancelIndexing so a late semantic-search command cannot start a new
// index build during shutdown.
func (m *Model) Cancel() {
	if m == nil || (m.searchContext == nil && m.searchCancel == nil && !m.searching && !m.indexing) {
		return
	}
	m.debounceGen++
	m.cancelSearch()
	m.searching = false
	m.indexing = false
}

func (m *Model) replaceSearchContext() {
	m.cancelSearch()
	m.searchContext, m.searchCancel = context.WithCancel(context.Background())
}

// maxVisibleResults returns the number of result lines visible in the overlay.
func (m Model) maxVisibleResults() int {
	// Reserve a row for the scroll hint and the box's bottom frame.
	return max(1, min(20, m.height-m.headerLines()-m.theme.SearchBox.GetPaddingBottom()-m.theme.SearchBox.GetBorderBottomSize()-1))
}

// ensureCursorVisible adjusts scrollY so the cursor is in the visible window.
func (m *Model) ensureCursorVisible() {
	visible := m.maxVisibleResults()
	if m.cursor < m.scrollY {
		m.scrollY = m.cursor
	}
	if m.cursor >= m.scrollY+visible {
		m.scrollY = m.cursor - visible + 1
	}
}

const (
	searchBoxMinWidth = 60
	searchBoxMaxWidth = 100
)

// boxWidth grows with the terminal so previews are not cut after a few words,
// but never past what fits.
func (m Model) boxWidth() int {
	if m.width <= 0 {
		return searchBoxMinWidth
	}
	target := min(searchBoxMaxWidth, max(searchBoxMinWidth, m.width*85/100))
	return min(target, max(m.theme.SearchBox.GetHorizontalFrameSize()+1, m.width))
}

func (m Model) innerWidth() int {
	return max(1, m.boxWidth()-m.theme.SearchBox.GetHorizontalFrameSize())
}

// headerView is also the source of the result rows used for mouse hit testing.
func (m Model) headerView(markZones bool) string {
	var sb strings.Builder
	p := onPanel(m.theme, m.theme.Panel)
	muted := onPanel(m.theme, m.theme.Muted)
	accent := onPanel(m.theme, m.theme.PromptAccent)

	// Mode toggle
	textLabel, semLabel := muted.Render("Text"), accent.Render("Semantic")
	if m.mode == ModeText {
		textLabel, semLabel = accent.Render("Text"), muted.Render("Semantic")
	}
	sb.WriteString(onPanel(m.theme, m.theme.HelpTitle).Render("Search") + p.Render("  ") + textLabel + muted.Render("  |  ") + semLabel + muted.Render("  (Tab)"))
	if m.mode == ModeText {
		sb.WriteByte('\n')
		toggle := func(on bool, label string) string {
			if on {
				return accent.Render(label)
			}
			return muted.Render(label)
		}
		sb.WriteString(toggle(m.caseSensitive, "Aa Alt+C"))
		sb.WriteString(p.Render("  ") + toggle(m.wholeWord, "W Alt+W"))
		sb.WriteString(p.Render("  ") + toggle(m.regex, ".* Ctrl+R"))
	}
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Input
	sb.WriteString(paintBackground(m.input.View(), m.theme.SearchBox.GetBackground()))
	sb.WriteByte('\n')
	if m.showReplace {
		sb.WriteString(muted.Render("  ⤷ ") + paintBackground(m.replaceInput.View(), m.theme.SearchBox.GetBackground()) + "\n")
		replace := m.theme.ReplaceButton.Render("Replace")
		all := m.theme.ReplaceButton.Render("All")
		if markZones {
			replace = zone.Mark("search-replace-btn", replace)
			all = zone.Mark("search-replace-all-btn", all)
		}
		sb.WriteString(replace + p.Render(" ") + all)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')

	if m.indexing {
		sb.WriteString(m.spinner.View() + p.Render(" ") + muted.Render("Indexing project..."))
		sb.WriteByte('\n')
	} else if m.searching {
		sb.WriteString(muted.Render("Searching..."))
		sb.WriteByte('\n')
	}

	if m.errMsg != "" {
		sb.WriteString(onPanel(m.theme, m.theme.PromptDanger).Render(strings.ReplaceAll(m.errMsg, "\n", " ")))
		sb.WriteByte('\n')
	}

	lines := strings.Split(sb.String(), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.innerWidth(), "…")
	}
	return strings.Join(lines, "\n")
}

// View renders the search overlay.
func (m Model) View() string {
	var sb strings.Builder
	sb.WriteString(m.headerView(true))
	muted := onPanel(m.theme, m.theme.Muted)

	// Scrollable results
	visible := m.maxVisibleResults()
	endIdx := m.scrollY + visible
	if endIdx > len(m.results) {
		endIdx = len(m.results)
	}

	re := m.matchPattern()
	for i := m.scrollY; i < endIdx; i++ {
		sb.WriteString(m.resultRow(m.results[i], i == m.cursor, re))
		if i < endIdx-1 {
			sb.WriteByte('\n')
		}
	}

	if len(m.results) == 0 && m.input.Value() != "" && !m.searching && !m.indexing {
		sb.WriteString(muted.Render("No results"))
	}

	// Scroll hint, aligned with the result rows above it.
	if len(m.results) > visible {
		sb.WriteByte('\n')
		hint := fmt.Sprintf("%d/%d results", min(m.cursor+1, len(m.results)), len(m.results))
		sb.WriteString(muted.Render(hint))
	}

	// Pad every row to the content width with the panel background so blank
	// rows and short rows are painted too, not left to the terminal default.
	p := onPanel(m.theme, m.theme.Panel)
	lines := strings.Split(sb.String(), "\n")
	for i, line := range lines {
		if gap := m.innerWidth() - ansi.StringWidth(line); gap > 0 {
			lines[i] = line + p.Render(strings.Repeat(" ", gap))
		}
	}
	return m.theme.SearchBox.Width(m.boxWidth()).Render(strings.Join(lines, "\n"))
}

// matchPattern compiles the pattern used to highlight matches inside previews.
// Semantic results are ranked by meaning rather than text, so there is nothing
// to highlight; an invalid pattern simply highlights nothing.
func (m Model) matchPattern() *regexp.Regexp {
	query := m.input.Value()
	if m.mode != ModeText || query == "" {
		return nil
	}
	re, err := CompilePattern(query, SearchOpts{Regex: m.regex, CaseSensitive: m.caseSensitive, WholeWord: m.wholeWord})
	if err != nil {
		return nil
	}
	return re
}

// textSpan is a run of one style inside a result row.
type textSpan struct {
	text  string
	style lipgloss.Style
}

// resultRow renders one result as `path:line  [symbol  ]preview`, with the
// location muted and the matched text of the preview highlighted. The row is
// cut to the content width with a single trailing ellipsis and always filled
// to that width, so the selected row is one even block.
func (m Model) resultRow(r Result, selected bool, re *regexp.Regexp) string {
	width := m.innerWidth()
	base := onPanel(m.theme, m.theme.SearchResult)
	matchStyle := m.theme.FindMatch
	if selected {
		base = m.theme.SearchActive
		matchStyle = m.theme.FindMatchCurrent
	}
	bg := base.GetBackground()
	text := lipgloss.NewStyle().Background(bg).Foreground(base.GetForeground())
	locStyle := text.Foreground(readableForeground(m.theme.Muted.GetForeground(), base.GetForeground(), bg))

	pathMax := min(40, max(25, width/3))
	spans := []textSpan{
		{truncPath(r.FilePath, pathMax) + ":" + strconv.Itoa(r.Line+1), locStyle},
		{"  ", text},
	}
	if r.SymbolName != "" {
		spans = append(spans,
			textSpan{truncStr(r.SymbolName, 20), text.Foreground(m.theme.PromptAccent.GetForeground())},
			textSpan{"  ", text},
		)
	}
	if re == nil {
		spans = append(spans, textSpan{r.Preview, text})
	} else {
		last := 0
		for _, at := range re.FindAllStringIndex(r.Preview, -1) {
			if at[0] == at[1] {
				continue
			}
			spans = append(spans, textSpan{r.Preview[last:at[0]], text}, textSpan{r.Preview[at[0]:at[1]], matchStyle})
			last = at[1]
		}
		spans = append(spans, textSpan{r.Preview[last:], text})
	}

	var full strings.Builder
	for _, span := range spans {
		full.WriteString(span.text)
	}
	cut := ansi.Truncate(full.String(), width, "…")
	body, ellipsis := cut, false
	if cut != full.String() && strings.HasSuffix(cut, "…") {
		body, ellipsis = strings.TrimSuffix(cut, "…"), true
	}

	var sb strings.Builder
	offset := 0
	for _, span := range spans {
		start, end := offset, offset+len(span.text)
		offset = end
		if start >= len(body) || span.text == "" {
			continue
		}
		sb.WriteString(span.style.Render(body[start:min(end, len(body))]))
	}
	if ellipsis {
		sb.WriteString(text.Render("…"))
	}
	if gap := width - ansi.StringWidth(cut); gap > 0 {
		sb.WriteString(text.Render(strings.Repeat(" ", gap)))
	}
	return sb.String()
}

// readableForeground returns preferred when it keeps 4.5:1 contrast on bg and
// fallback otherwise. Muted text is tuned for the editor and panel backgrounds;
// the selected row is lighter and may need the regular text color.
func readableForeground(preferred, fallback, bg color.Color) color.Color {
	if contrastRatio(preferred, bg) >= 4.5 {
		return preferred
	}
	return fallback
}

func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func relativeLuminance(c color.Color) float64 {
	linear := func(v uint32) float64 {
		x := float64(v) / 65535
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b, _ := c.RGBA()
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func truncPath(path string, maxLen int) string {
	width := ansi.StringWidth(path)
	if width <= maxLen {
		return path
	}
	if maxLen <= 3 {
		return ansi.Truncate(path, max(0, maxLen), "")
	}
	return "..." + ansi.Cut(path, width-maxLen+3, width)
}

func truncStr(s string, maxLen int) string {
	tail := "..."
	if maxLen < 3 {
		tail = ""
	}
	return ansi.Truncate(s, max(0, maxLen), tail)
}
