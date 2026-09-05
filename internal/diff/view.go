package diff

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
	"teak/internal/highlight"
	"teak/internal/ui"
)

// Model is a read-only side-by-side diff viewer.
type Model struct {
	id          uint64
	FilePath    string
	Lines       []DiffLine
	ScrollY     int
	Width       int
	Height      int
	theme       ui.Theme
	leftHL      *highlight.Highlighter
	rightHL     *highlight.Highlighter
	leftSource  []string
	rightSource []string
	leftKinds   []LineKind
	rightKinds  []LineKind
	gutter      int
	// Maps from DiffLine index → highlighter line index (-1 if no content)
	leftLineMap  []int
	rightLineMap []int
	highlighting *highlightScheduler
}

type highlightLane struct {
	generation uint64
	cancel     context.CancelFunc
}

type highlightScheduler struct {
	viewport highlightLane
}

var nextDiffModelID atomic.Uint64

// HighlightReadyMsg contains viewport-scoped tokens built outside Update.
// Model identity and generation reject results from closed views and obsolete
// scroll positions.
type HighlightReadyMsg struct {
	modelID    uint64
	generation uint64
	leftBatch  highlight.TokenBatch
	rightBatch highlight.TokenBatch
	leftReady  bool
	rightReady bool
	canceled   bool
}

func (m Model) maxScroll() int {
	visible := max(m.Height, 1)
	return max(len(m.Lines)-visible, 0)
}

// New creates a new diff view model.
func New(filePath string, lines []DiffLine, theme ui.Theme) Model {
	m := Model{
		id:           nextDiffModelID.Add(1),
		FilePath:     filePath,
		Lines:        lines,
		theme:        theme,
		highlighting: &highlightScheduler{},
	}
	m.buildHighlighting()
	return m
}

// SetTheme replaces syntax highlighters and returns asynchronous work for the
// visible viewport. It never tokenizes in the Bubble Tea update loop.
func (m *Model) SetTheme(theme ui.Theme) tea.Cmd {
	if theme == m.theme {
		return nil
	}
	if m.highlighting != nil && m.highlighting.viewport.cancel != nil {
		m.highlighting.viewport.cancel()
	}
	m.theme = theme
	m.leftHL = highlight.New(m.FilePath, theme)
	m.rightHL = highlight.New(m.FilePath, theme)
	if m.highlighting != nil {
		m.highlighting.viewport.generation++
	}
	return m.scheduleViewportHighlight()
}

// buildHighlighting prepares immutable per-side indexes for syntax coloring.
// Tokenization is deliberately deferred to PrepareViewport: a large diff must
// not allocate styled tokens for lines that have never been visible.
func (m *Model) buildHighlighting() {
	m.leftLineMap = make([]int, len(m.Lines))
	m.rightLineMap = make([]int, len(m.Lines))
	maxLineNumber := 0

	for i, dl := range m.Lines {
		if dl.LeftNum > maxLineNumber {
			maxLineNumber = dl.LeftNum
		}
		if dl.RightNum > maxLineNumber {
			maxLineNumber = dl.RightNum
		}
		if dl.IsSeparator {
			m.leftLineMap[i] = -1
			m.rightLineMap[i] = -1
			continue
		}
		if dl.LeftKind != KindEmpty {
			m.leftLineMap[i] = len(m.leftSource)
			m.leftSource = append(m.leftSource, dl.Left)
			m.leftKinds = append(m.leftKinds, dl.LeftKind)
		} else {
			m.leftLineMap[i] = -1
		}
		if dl.RightKind != KindEmpty {
			m.rightLineMap[i] = len(m.rightSource)
			m.rightSource = append(m.rightSource, dl.Right)
			m.rightKinds = append(m.rightKinds, dl.RightKind)
		} else {
			m.rightLineMap[i] = -1
		}
	}

	m.leftHL = highlight.New(m.FilePath, m.theme)
	m.rightHL = highlight.New(m.FilePath, m.theme)
	digits := 1
	for n := maxLineNumber; n >= 10; n /= 10 {
		digits++
	}
	m.gutter = digits + 1
}

// PrepareViewport tokenizes and installs only the syntax context around the
// requested diff rows. It is intended for a background command; the source
// slices are immutable after New returns.
func (m *Model) PrepareViewport(ctx context.Context, viewStart, viewEnd int) bool {
	result := prepareViewportHighlightWithTheme(
		ctx,
		m.theme,
		m.leftHL,
		m.rightHL,
		m.leftSource,
		m.rightSource,
		m.leftKinds,
		m.rightKinds,
		m.leftLineMap,
		m.rightLineMap,
		len(m.Lines),
		viewStart,
		viewEnd,
	)
	if !result.complete {
		return false
	}
	if result.leftReady {
		m.leftHL.MergeBatch(result.leftBatch)
	}
	if result.rightReady {
		m.rightHL.MergeBatch(result.rightBatch)
	}
	return true
}

type viewportHighlightResult struct {
	leftBatch  highlight.TokenBatch
	rightBatch highlight.TokenBatch
	leftReady  bool
	rightReady bool
	complete   bool
}

func prepareViewportHighlight(ctx context.Context, leftHL, rightHL *highlight.Highlighter, leftSource, rightSource []string, leftKinds, rightKinds []LineKind, leftMap, rightMap []int, totalLines, viewStart, viewEnd int) viewportHighlightResult {
	return prepareViewportHighlightWithTheme(ctx, ui.DefaultTheme(), leftHL, rightHL, leftSource, rightSource, leftKinds, rightKinds, leftMap, rightMap, totalLines, viewStart, viewEnd)
}

func prepareViewportHighlightWithTheme(ctx context.Context, theme ui.Theme, leftHL, rightHL *highlight.Highlighter, leftSource, rightSource []string, leftKinds, rightKinds []LineKind, leftMap, rightMap []int, totalLines, viewStart, viewEnd int) viewportHighlightResult {
	if ctx == nil {
		ctx = context.Background()
	}
	leftSnapshot, leftOK := captureSideViewport(leftSource, leftMap, totalLines, viewStart, viewEnd)
	rightSnapshot, rightOK := captureSideViewport(rightSource, rightMap, totalLines, viewStart, viewEnd)
	result := viewportHighlightResult{leftReady: leftOK, rightReady: rightOK}
	if leftOK {
		var complete bool
		result.leftBatch, complete = leftHL.TokenizeViewportSnapshotBatch(ctx, leftSnapshot)
		if !complete {
			return result
		}
	}
	if rightOK {
		var complete bool
		result.rightBatch, complete = rightHL.TokenizeViewportSnapshotBatch(ctx, rightSnapshot)
		if !complete {
			return result
		}
	}
	styles := make(map[diffTokenStyleKey]highlight.StyledToken)
	if leftOK {
		applyDiffBackgrounds(&result.leftBatch, leftKinds, styles, theme)
	}
	if rightOK {
		applyDiffBackgrounds(&result.rightBatch, rightKinds, styles, theme)
	}
	result.complete = ctx.Err() == nil
	return result
}

type diffTokenStyleKey struct {
	kind           LineKind
	prefix, suffix string
	fast           bool
}

func applyDiffBackgrounds(batch *highlight.TokenBatch, kinds []LineKind, styles map[diffTokenStyleKey]highlight.StyledToken, theme ui.Theme) {
	for lineOffset := range batch.Lines {
		lineIndex := batch.StartLine + lineOffset
		if lineIndex < 0 || lineIndex >= len(kinds) {
			continue
		}
		for tokenIndex, token := range batch.Lines[lineOffset] {
			key := diffTokenStyleKey{kind: kinds[lineIndex], prefix: token.Prefix, suffix: token.Suffix, fast: token.FastSGR}
			styled, ok := styles[key]
			if !ok || !token.FastSGR {
				styled = token.WithBackground(backgroundForKindWithTheme(kinds[lineIndex], theme))
				if token.FastSGR {
					styles[key] = styled
				}
			}
			styled.Text = token.Text
			batch.Lines[lineOffset][tokenIndex] = styled
		}
	}
}

const diffHighlightContextRows = 200

func captureSideViewport(source []string, lineMap []int, totalDiffLines, viewStart, viewEnd int) (highlight.ViewportSnapshot, bool) {
	if len(source) == 0 || totalDiffLines == 0 {
		return highlight.ViewportSnapshot{}, false
	}
	viewStart = max(0, min(viewStart, totalDiffLines))
	viewEnd = max(viewStart, min(viewEnd, totalDiffLines))
	if viewEnd == viewStart {
		viewEnd = min(totalDiffLines, viewStart+1)
	}
	rangeStart := max(0, viewStart-diffHighlightContextRows)
	rangeEnd := min(totalDiffLines, viewEnd+diffHighlightContextRows)
	sourceStart, sourceEnd, ok := mappedSourceRange(lineMap, rangeStart, rangeEnd)
	if !ok {
		return highlight.ViewportSnapshot{}, false
	}
	sideViewStart, sideViewEnd, visible := mappedSourceRange(lineMap, viewStart, viewEnd)
	if !visible {
		sideViewStart = sourceStart
		sideViewEnd = min(sourceEnd, sourceStart+1)
	}

	var content bytes.Buffer
	for _, line := range source[sourceStart:sourceEnd] {
		content.WriteString(line)
		content.WriteByte('\n')
	}
	return highlight.ViewportSnapshot{
		Content:   content.Bytes(),
		LineCount: len(source),
		StartLine: sourceStart,
		ViewStart: sideViewStart,
		ViewEnd:   sideViewEnd,
	}, true
}

func mappedSourceRange(lineMap []int, start, end int) (int, int, bool) {
	start = max(0, min(start, len(lineMap)))
	end = max(start, min(end, len(lineMap)))
	first, last := -1, -1
	for _, mapped := range lineMap[start:end] {
		if mapped < 0 {
			continue
		}
		if first < 0 {
			first = mapped
		}
		last = mapped
	}
	if first < 0 {
		return 0, 0, false
	}
	return first, last + 1, true
}

// SetSize sets the viewport dimensions.
func (m *Model) SetSize(w, h int) {
	m.Width = w
	m.Height = h
}

// Update handles scroll input.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if prepared, ok := msg.(HighlightReadyMsg); ok {
		m.ApplyHighlight(prepared)
		return m, nil
	}
	previousScroll := m.ScrollY
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			if m.ScrollY > 0 {
				m.ScrollY--
			}
		case "down":
			maxScroll := m.maxScroll()
			if m.ScrollY < maxScroll {
				m.ScrollY++
			}
		case "pgup", "page_up":
			m.ScrollY -= m.Height
			if m.ScrollY < 0 {
				m.ScrollY = 0
			}
		case "pgdown", "page_down":
			m.ScrollY += m.Height
			maxScroll := m.maxScroll()
			if m.ScrollY > maxScroll {
				m.ScrollY = maxScroll
			}
		case "home":
			m.ScrollY = 0
		case "end":
			m.ScrollY = m.maxScroll()
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.ScrollY -= 3
			if m.ScrollY < 0 {
				m.ScrollY = 0
			}
		case tea.MouseWheelDown:
			m.ScrollY += 3
			maxScroll := m.maxScroll()
			if m.ScrollY > maxScroll {
				m.ScrollY = maxScroll
			}
		}
	}
	if m.ScrollY == previousScroll {
		return m, nil
	}
	return m, m.scheduleViewportHighlight()
}

func (m *Model) scheduleViewportHighlight() tea.Cmd {
	if len(m.Lines) == 0 || m.leftHL == nil || m.rightHL == nil {
		return nil
	}
	ctx, generation := m.beginViewportHighlight()
	modelID := m.id
	leftHL, rightHL := m.leftHL, m.rightHL
	leftSource, rightSource := m.leftSource, m.rightSource
	leftKinds, rightKinds := m.leftKinds, m.rightKinds
	leftMap, rightMap := m.leftLineMap, m.rightLineMap
	theme := m.theme
	totalLines := len(m.Lines)
	viewStart := m.ScrollY
	viewEnd := min(totalLines, viewStart+max(1, m.Height))
	return func() tea.Msg {
		result := prepareViewportHighlightWithTheme(ctx, theme, leftHL, rightHL, leftSource, rightSource, leftKinds, rightKinds, leftMap, rightMap, totalLines, viewStart, viewEnd)
		return HighlightReadyMsg{
			modelID: modelID, generation: generation,
			leftBatch: result.leftBatch, rightBatch: result.rightBatch,
			leftReady: result.leftReady, rightReady: result.rightReady,
			canceled: !result.complete,
		}
	}
}

func (m *Model) beginViewportHighlight() (context.Context, uint64) {
	if m.highlighting == nil {
		m.highlighting = &highlightScheduler{}
	}
	lane := &m.highlighting.viewport
	if lane.cancel != nil {
		lane.cancel()
	}
	lane.generation++
	ctx, cancel := context.WithCancel(context.Background())
	lane.cancel = cancel
	return ctx, lane.generation
}

// ApplyHighlight installs a current viewport projection in bounded time.
func (m *Model) ApplyHighlight(msg HighlightReadyMsg) bool {
	if msg.canceled || msg.modelID != m.id || m.highlighting == nil || msg.generation != m.highlighting.viewport.generation {
		return false
	}
	if msg.leftReady {
		m.leftHL.MergeBatch(msg.leftBatch)
	}
	if msg.rightReady {
		m.rightHL.MergeBatch(msg.rightBatch)
	}
	m.highlighting.viewport.cancel = nil
	return true
}

// CancelHighlight stops viewport work when the view is closed.
func (m *Model) CancelHighlight() {
	if m.highlighting == nil {
		return
	}
	lane := &m.highlighting.viewport
	if lane.cancel != nil {
		lane.cancel()
		lane.cancel = nil
	}
	lane.generation++
}

// View renders the side-by-side diff.
func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 || len(m.Lines) == 0 {
		return ""
	}

	// Calculate layout
	panelWidth := (m.Width - 1) / 2 // -1 for center border
	if panelWidth < 4 {
		panelWidth = 4
	}
	gutterWidth := m.gutterWidth()
	contentWidth := panelWidth - gutterWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	var sb strings.Builder
	sb.Grow(max(0, m.Height*m.Width*4))
	for i := range m.Height {
		lineIdx := m.ScrollY + i
		if i > 0 {
			sb.WriteByte('\n')
		}

		if lineIdx < 0 || lineIdx >= len(m.Lines) {
			// Empty row
			emptyLeft := m.renderEmptyPanel(panelWidth)
			emptyRight := m.renderEmptyPanel(m.Width - panelWidth - 1)
			sb.WriteString(emptyLeft)
			sb.WriteString(m.borderStyle().Render("│"))
			sb.WriteString(emptyRight)
			continue
		}

		dl := m.Lines[lineIdx]

		if dl.IsSeparator {
			sep := m.renderSeparator(panelWidth)
			sepRight := m.renderSeparator(m.Width - panelWidth - 1)
			sb.WriteString(sep)
			sb.WriteString(m.borderStyle().Render("│"))
			sb.WriteString(sepRight)
			continue
		}

		// Left panel
		sb.WriteString(m.renderGutter(dl.LeftNum, dl.LeftKind, gutterWidth))
		leftHLIdx := -1
		if lineIdx < len(m.leftLineMap) {
			leftHLIdx = m.leftLineMap[lineIdx]
		}
		leftTokens := m.getTokens(m.leftHL, leftHLIdx)
		sb.WriteString(m.renderContentHighlighted(dl.Left, dl.LeftKind, contentWidth, leftTokens))
		// Center border
		sb.WriteString(m.borderStyle().Render("│"))
		// Right panel
		rightContentWidth := m.Width - panelWidth - 1 - gutterWidth
		if rightContentWidth < 1 {
			rightContentWidth = 1
		}
		sb.WriteString(m.renderGutter(dl.RightNum, dl.RightKind, gutterWidth))
		rightHLIdx := -1
		if lineIdx < len(m.rightLineMap) {
			rightHLIdx = m.rightLineMap[lineIdx]
		}
		rightTokens := m.getTokens(m.rightHL, rightHLIdx)
		sb.WriteString(m.renderContentHighlighted(dl.Right, dl.RightKind, rightContentWidth, rightTokens))
	}
	return sb.String()
}

func (m Model) getTokens(hl *highlight.Highlighter, lineIdx int) []highlight.StyledToken {
	if hl == nil || lineIdx < 0 || lineIdx >= hl.LineCount() {
		return nil
	}
	return hl.Line(lineIdx)
}

func (m Model) gutterWidth() int {
	if m.gutter < 2 {
		return 2
	}
	return m.gutter
}

func (m Model) renderGutter(num int, kind LineKind, width int) string {
	style := m.theme.DiffGutter.Width(width).MaxWidth(width)
	if num == 0 {
		return style.Render(strings.Repeat(" ", width))
	}
	numStr := fmt.Sprintf("%*d ", width-1, num)
	// Clamp to width in case the number is wider than expected
	numStr = truncateToWidth(numStr, width)
	return style.Render(numStr)
}

// bgForKind returns the background color for a diff line kind.
func (m Model) bgForKind(kind LineKind) color.Color {
	return backgroundForKindWithTheme(kind, m.theme)
}

// backgroundForKind preserves the package helper used by older tests.
func backgroundForKind(kind LineKind) color.Color {
	return backgroundForKindWithTheme(kind, ui.DefaultTheme())
}

func backgroundForKindWithTheme(kind LineKind, theme ui.Theme) color.Color {
	switch kind {
	case KindAdded:
		return theme.DiffAdded.GetBackground()
	case KindRemoved:
		return theme.DiffRemoved.GetBackground()
	case KindEmpty:
		return theme.DiffEmpty.GetBackground()
	default:
		return theme.Editor.GetBackground()
	}
}

// fgForKind returns the default foreground color for a diff line kind.
func (m Model) fgForKind(kind LineKind) color.Color {
	switch kind {
	case KindEmpty:
		return m.theme.DiffEmpty.GetForeground()
	default:
		return m.theme.Editor.GetForeground()
	}
}

func (m Model) renderContentHighlighted(text string, kind LineKind, width int, tokens []highlight.StyledToken) string {
	bg := m.bgForKind(kind)

	if len(tokens) > 0 && kind != KindEmpty {
		// Render with syntax highlighting, overriding background to match diff kind
		var sb strings.Builder
		sb.Grow(max(0, width*4))
		widthLeft := width
		for _, tok := range tokens {
			if widthLeft <= 0 {
				break
			}
			// Strip newlines/carriage returns from token text
			t := strings.TrimRight(tok.Text, "\n\r")
			// Expand tabs to spaces
			t = strings.ReplaceAll(t, "\t", "    ")
			tw := runewidth.StringWidth(t)
			if tw > widthLeft {
				t = truncateToWidth(t, widthLeft)
				tw = runewidth.StringWidth(t)
			}
			// Background-adjusted SGR is prepared with the viewport tokens, so
			// rendering avoids lipgloss's generic per-token layout pipeline.
			tok.WriteTo(&sb, t)
			widthLeft -= tw
		}
		// Pad remaining width
		if widthLeft > 0 {
			pad := lipgloss.NewStyle().Background(bg).Foreground(m.fgForKind(kind))
			sb.WriteString(pad.Render(strings.Repeat(" ", widthLeft)))
		}
		return sb.String()
	}

	// Fallback: plain text with diff coloring
	// Expand tabs and strip newlines
	cleanText := strings.ReplaceAll(strings.TrimRight(text, "\n\r"), "\t", "    ")
	truncated := truncateToWidth(cleanText, width)
	style := lipgloss.NewStyle().Background(bg).Foreground(m.fgForKind(kind)).Width(width).MaxWidth(width)
	return style.Render(truncated)
}

func (m Model) renderEmptyPanel(width int) string {
	return lipgloss.NewStyle().Background(m.theme.Editor.GetBackground()).Width(width).Render("")
}

func (m Model) renderSeparator(width int) string {
	style := m.theme.DiffHunkHeader.Width(width)
	label := " ..."
	return style.Render(label)
}

func (m Model) borderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme.DiffBorder.GetForeground())
}

// truncateToWidth truncates s to at most width display columns.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width {
			return s[:i]
		}
		w += rw
	}
	return s
}
