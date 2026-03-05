package diff

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
	"teak/internal/highlight"
	"teak/internal/ui"
)

// Model is a read-only side-by-side diff viewer.
type Model struct {
	FilePath string
	Lines    []DiffLine
	ScrollY  int
	Width    int
	Height   int
	theme    ui.Theme
	leftHL   *highlight.Highlighter
	rightHL  *highlight.Highlighter
	// Maps from DiffLine index → highlighter line index (-1 if no content)
	leftLineMap  []int
	rightLineMap []int
}

// New creates a new diff view model.
func New(filePath string, lines []DiffLine, theme ui.Theme) Model {
	m := Model{
		FilePath: filePath,
		Lines:    lines,
		theme:    theme,
	}
	m.buildHighlighting()
	return m
}

// buildHighlighting tokenizes the left and right sides for syntax coloring.
// Only actual content lines are sent to the highlighter; separators and
// KindEmpty sides are mapped to -1 so View() can look up the correct
// highlighter line index per DiffLine.
func (m *Model) buildHighlighting() {
	if len(m.Lines) == 0 {
		return
	}

	var leftLines, rightLines []string
	m.leftLineMap = make([]int, len(m.Lines))
	m.rightLineMap = make([]int, len(m.Lines))

	leftIdx, rightIdx := 0, 0
	for i, dl := range m.Lines {
		if dl.IsSeparator {
			m.leftLineMap[i] = -1
			m.rightLineMap[i] = -1
			continue
		}
		if dl.LeftKind != KindEmpty {
			leftLines = append(leftLines, dl.Left)
			m.leftLineMap[i] = leftIdx
			leftIdx++
		} else {
			m.leftLineMap[i] = -1
		}
		if dl.RightKind != KindEmpty {
			rightLines = append(rightLines, dl.Right)
			m.rightLineMap[i] = rightIdx
			rightIdx++
		} else {
			m.rightLineMap[i] = -1
		}
	}

	leftContent := strings.Join(leftLines, "\n")
	rightContent := strings.Join(rightLines, "\n")

	m.leftHL = highlight.New(m.FilePath, m.theme)
	m.leftHL.Tokenize([]byte(leftContent))

	m.rightHL = highlight.New(m.FilePath, m.theme)
	m.rightHL.Tokenize([]byte(rightContent))
}

// SetSize sets the viewport dimensions.
func (m *Model) SetSize(w, h int) {
	m.Width = w
	m.Height = h
}

// Update handles scroll input.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			if m.ScrollY > 0 {
				m.ScrollY--
			}
		case "down":
			maxScroll := len(m.Lines) - 1
			if maxScroll < 0 {
				maxScroll = 0
			}
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
			maxScroll := len(m.Lines) - 1
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.ScrollY > maxScroll {
				m.ScrollY = maxScroll
			}
		case "home":
			m.ScrollY = 0
		case "end":
			m.ScrollY = len(m.Lines) - 1
			if m.ScrollY < 0 {
				m.ScrollY = 0
			}
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			m.ScrollY -= 3
			if m.ScrollY < 0 {
				m.ScrollY = 0
			}
		} else if mouse.Button == tea.MouseWheelDown {
			m.ScrollY += 3
			maxScroll := len(m.Lines) - 1
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.ScrollY > maxScroll {
				m.ScrollY = maxScroll
			}
		}
	}
	return m, nil
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
	maxNum := 0
	for _, dl := range m.Lines {
		if dl.LeftNum > maxNum {
			maxNum = dl.LeftNum
		}
		if dl.RightNum > maxNum {
			maxNum = dl.RightNum
		}
	}
	digits := 1
	for n := maxNum; n >= 10; n /= 10 {
		digits++
	}
	return digits + 1 // +1 for padding
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
	switch kind {
	case KindAdded:
		return lipgloss.Color("#2E3B2E")
	case KindRemoved:
		return lipgloss.Color("#3B2C2E")
	case KindEmpty:
		return ui.Nord1
	default:
		return ui.Nord0
	}
}

// fgForKind returns the default foreground color for a diff line kind.
func (m Model) fgForKind(kind LineKind) color.Color {
	switch kind {
	case KindEmpty:
		return ui.Nord3
	default:
		return ui.Nord4
	}
}

func (m Model) renderContentHighlighted(text string, kind LineKind, width int, tokens []highlight.StyledToken) string {
	bg := m.bgForKind(kind)

	if len(tokens) > 0 && kind != KindEmpty {
		// Render with syntax highlighting, overriding background to match diff kind
		var sb strings.Builder
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
			// Use the token's foreground but override background for diff coloring
			style := tok.Style.Background(bg)
			sb.WriteString(style.Render(t))
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
	return lipgloss.NewStyle().Background(ui.Nord0).Width(width).Render("")
}

func (m Model) renderSeparator(width int) string {
	style := m.theme.DiffHunkHeader.Width(width)
	label := " ..."
	return style.Render(label)
}

func (m Model) borderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ui.Nord3)
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
