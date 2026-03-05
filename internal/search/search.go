package search

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// debounceTickMsg is sent after the debounce delay to trigger a search.
type debounceTickMsg struct {
	generation int
}

// Mode indicates the search type.
type Mode int

const (
	ModeText     Mode = iota // grep/regex
	ModeSemantic             // vecgrep
)

// Result represents a single search result.
type Result struct {
	FilePath string
	Line     int
	Col      int
	Preview  string
	Score    float64
}

// OpenResultMsg is sent when a result is selected.
type OpenResultMsg struct {
	FilePath string
	Line     int
	Col      int
}

// CloseSearchMsg is sent when the search overlay should close.
type CloseSearchMsg struct{}

// SearchIndexingMsg is sent when semantic search starts indexing.
type SearchIndexingMsg struct{}

// SearchResultsMsg is sent when results arrive from a search.
type SearchResultsMsg struct {
	Results []Result
	Err     error
}

// Model is the search overlay model.
type Model struct {
	input       textinput.Model
	mode        Mode
	results     []Result
	cursor      int
	scrollY     int // scroll offset for results
	theme       ui.Theme
	width       int
	height      int
	rootDir     string
	lastQuery   string
	searching   bool
	indexing    bool
	indexed    bool // true after first successful semantic search
	spinner     spinner.Model
	errMsg      string
	debounceGen int // generation counter for debounce
}

// New creates a new search model.
func New(theme ui.Theme, rootDir string, mode Mode) Model {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 256
	ti.SetWidth(50)

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(ui.Nord13)),
	)

	return Model{
		input:   ti,
		mode:    mode,
		theme:   theme,
		rootDir: rootDir,
		spinner: sp,
	}
}

// Focus focuses the text input and returns the cursor blink command.
func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

// SetSize sets the overlay dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.input.SetWidth(50)
}

// headerLines is the number of lines before results in the search overlay view.
// mode toggle + blank + input + blank = 4 lines, plus border padding.
const headerLines = 6

// Update handles input messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return m, func() tea.Msg { return CloseSearchMsg{} }
		case "enter":
			if len(m.results) > 0 && m.cursor < len(m.results) {
				r := m.results[m.cursor]
				return m, func() tea.Msg {
					return OpenResultMsg{
						FilePath: r.FilePath,
						Line:     r.Line,
						Col:      r.Col,
					}
				}
			}
			return m, nil
		case "tab":
			if m.mode == ModeText {
				m.mode = ModeSemantic
			} else {
				m.mode = ModeText
			}
			// Only re-search if there's a query
			m.results = nil
			m.cursor = 0
			m.scrollY = 0
			if m.input.Value() != "" {
				return m, m.dispatchSearch()
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
			clickedIdx := m.scrollY + mouse.Y - headerLines
			if clickedIdx >= 0 && clickedIdx < len(m.results) {
				r := m.results[clickedIdx]
				return m, func() tea.Msg {
					return OpenResultMsg{
						FilePath: r.FilePath,
						Line:     r.Line,
						Col:      r.Col,
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
		if mouse.Button == tea.MouseWheelUp {
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
		} else if mouse.Button == tea.MouseWheelDown {
			m.scrollY += 3
			if m.scrollY > maxScroll {
				m.scrollY = maxScroll
			}
		}
		return m, nil

	case SearchIndexingMsg:
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

	case debounceTickMsg:
		if msg.generation == m.debounceGen {
			return m, m.dispatchSearch()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Check if query changed
	query := m.input.Value()
	if query != m.lastQuery {
		m.lastQuery = query
		if query == "" {
			m.results = nil
			m.cursor = 0
			return m, cmd
		}
		// Debounce: increment generation and schedule a tick
		m.debounceGen++
		gen := m.debounceGen
		debounceCmd := tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
			return debounceTickMsg{generation: gen}
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
	return func() tea.Msg {
		var results []Result
		var err error
		if mode == ModeSemantic {
			results, err = SemanticSearch(rootDir, query)
		} else {
			results, err = TextSearch(rootDir, query)
		}
		return SearchResultsMsg{Results: results, Err: err}
	}
}

// doSearchSemantic sends an indexing indicator (first time only) then runs the search.
func (m Model) doSearchSemantic() tea.Cmd {
	query := m.input.Value()
	if query == "" {
		return nil
	}
	rootDir := m.rootDir
	if !m.indexed {
		return tea.Sequence(
			func() tea.Msg { return SearchIndexingMsg{} },
			func() tea.Msg {
				results, err := SemanticSearch(rootDir, query)
				return SearchResultsMsg{Results: results, Err: err}
			},
		)
	}
	return func() tea.Msg {
		results, err := SemanticSearch(rootDir, query)
		return SearchResultsMsg{Results: results, Err: err}
	}
}

// maxVisibleResults returns the number of result lines visible in the overlay.
func (m Model) maxVisibleResults() int {
	// header (title+mode) + blank + input + blank + possible status = ~5-6 lines, plus border padding ~4
	v := m.height - 12
	if v < 3 {
		v = 3
	}
	if v > 20 {
		v = 20
	}
	return v
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

// View renders the search overlay.
func (m Model) View() string {
	const boxWidth = 60

	var sb strings.Builder

	// Mode toggle
	textLabel := "Text"
	semLabel := "Semantic"
	if m.mode == ModeText {
		textLabel = "[Text]"
	} else {
		semLabel = "[Semantic]"
	}
	modeStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
	activeMode := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	if m.mode == ModeText {
		textLabel = activeMode.Render("Text")
		semLabel = modeStyle.Render("Semantic")
	} else {
		textLabel = modeStyle.Render("Text")
		semLabel = activeMode.Render("Semantic")
	}
	sb.WriteString(m.theme.HelpTitle.Render("Search") + "  " + textLabel + modeStyle.Render("  |  ") + semLabel + modeStyle.Render("  (Tab)"))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Input
	sb.WriteString(m.input.View())
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	if m.indexing {
		sb.WriteString(m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ui.Nord13).Render("Indexing project..."))
		sb.WriteByte('\n')
	}

	if m.errMsg != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(ui.Nord11).Render(m.errMsg))
		sb.WriteByte('\n')
	}

	// Scrollable results
	visible := m.maxVisibleResults()
	endIdx := m.scrollY + visible
	if endIdx > len(m.results) {
		endIdx = len(m.results)
	}

	for i := m.scrollY; i < endIdx; i++ {
		r := m.results[i]
		line := fmt.Sprintf("%s:%d  %s", truncPath(r.FilePath, 25), r.Line+1, truncStr(r.Preview, boxWidth-30))
		if i == m.cursor {
			sb.WriteString(m.theme.SearchActive.Render(line))
		} else {
			sb.WriteString(m.theme.SearchResult.Render(line))
		}
		if i < endIdx-1 {
			sb.WriteByte('\n')
		}
	}

	if len(m.results) == 0 && m.input.Value() != "" && !m.searching && !m.indexing {
		sb.WriteString(lipgloss.NewStyle().Foreground(ui.Nord3).Render("No results"))
	}

	// Scroll hint
	if len(m.results) > visible {
		sb.WriteByte('\n')
		hint := fmt.Sprintf("  %d/%d results", min(m.cursor+1, len(m.results)), len(m.results))
		sb.WriteString(lipgloss.NewStyle().Foreground(ui.Nord3).Render(hint))
	}

	content := sb.String()
	return m.theme.SearchBox.Width(boxWidth).Render(content)
}

func truncPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

func truncStr(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
