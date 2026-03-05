package problems

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

// Problem represents a single diagnostic problem.
type Problem struct {
	FilePath string
	Line     int
	Col      int
	EndLine  int
	EndCol   int
	Severity int    // 1=error, 2=warning, 3=info, 4=hint
	Message  string
	Source   string
}

// SeverityLabel returns a human-readable label for the severity.
func (p *Problem) SeverityLabel() string {
	switch p.Severity {
	case 1:
		return "Error"
	case 2:
		return "Warning"
	case 3:
		return "Info"
	case 4:
		return "Hint"
	default:
		return "Unknown"
	}
}

// RelativePath returns the file path relative to the given root.
func (p *Problem) RelativePath(root string) string {
	rel, err := filepath.Rel(root, p.FilePath)
	if err != nil {
		return p.FilePath
	}
	return rel
}

// Group represents a group of problems in a file.
type Group struct {
	FilePath string
	Problems []Problem
}

// Model represents the Problems panel state.
type Model struct {
	problems      []Problem
	groups        []Group
	selectedIndex int // index into problems
	scrollY       int
	width         int
	height        int
	theme         ui.Theme
	rootDir       string
}

// New creates a new Problems panel model.
func New(theme ui.Theme, rootDir string) Model {
	return Model{
		theme:   theme,
		rootDir: rootDir,
	}
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetProblems updates the problems list and rebuilds groups.
func (m *Model) SetProblems(problems []Problem) {
	m.problems = problems
	m.groups = m.buildGroups()
	// Keep selection in bounds
	if m.selectedIndex >= len(m.problems) {
		m.selectedIndex = max(0, len(m.problems)-1)
	}
}

// buildGroups groups problems by file.
func (m *Model) buildGroups() []Group {
	fileMap := make(map[string][]Problem)
	for _, p := range m.problems {
		fileMap[p.FilePath] = append(fileMap[p.FilePath], p)
	}

	var groups []Group
	for path, probs := range fileMap {
		groups = append(groups, Group{
			FilePath: path,
			Problems: probs,
		})
	}

	// Sort groups by path for consistent ordering
	sortGroups(groups)
	return groups
}

// sortGroups sorts groups by file path.
func sortGroups(groups []Group) {
	for i := 0; i < len(groups)-1; i++ {
		for j := i + 1; j < len(groups); j++ {
			if groups[i].FilePath > groups[j].FilePath {
				groups[i], groups[j] = groups[j], groups[i]
			}
		}
	}
}

// ProblemCount returns the total number of problems.
func (m *Model) ProblemCount() int {
	return len(m.problems)
}

// ErrorCount returns the number of errors.
func (m *Model) ErrorCount() int {
	count := 0
	for _, p := range m.problems {
		if p.Severity == 1 {
			count++
		}
	}
	return count
}

// WarningCount returns the number of warnings.
func (m *Model) WarningCount() int {
	count := 0
	for _, p := range m.problems {
		if p.Severity == 2 {
			count++
		}
	}
	return count
}

// SelectedProblem returns the currently selected problem, or nil if none.
func (m *Model) SelectedProblem() *Problem {
	if len(m.problems) == 0 {
		return nil
	}
	return &m.problems[m.selectedIndex]
}

// SelectNext moves selection to the next problem.
func (m *Model) SelectNext() {
	if len(m.problems) > 0 {
		m.selectedIndex = (m.selectedIndex + 1) % len(m.problems)
		m.ensureVisible()
	}
}

// SelectPrev moves selection to the previous problem.
func (m *Model) SelectPrev() {
	if len(m.problems) > 0 {
		m.selectedIndex = (m.selectedIndex - 1 + len(m.problems)) % len(m.problems)
		m.ensureVisible()
	}
}

// ensureVisible scrolls to keep the selection visible.
func (m *Model) ensureVisible() {
	if m.selectedIndex < m.scrollY {
		m.scrollY = m.selectedIndex
	}
	if m.selectedIndex >= m.scrollY+m.height {
		m.scrollY = m.selectedIndex - m.height + 1
	}
}

// ScrollUp scrolls up by n items.
func (m *Model) ScrollUp(n int) {
	m.scrollY -= n
	if m.scrollY < 0 {
		m.scrollY = 0
	}
}

// ScrollDown scrolls down by n items.
func (m *Model) ScrollDown(n int) {
	maxScroll := max(0, len(m.problems)-m.height)
	m.scrollY += n
	if m.scrollY > maxScroll {
		m.scrollY = maxScroll
	}
}

// Height returns the visible height of the panel.
func (m *Model) Height() int {
	return m.height
}

// ScrollY returns the current scroll position.
func (m *Model) ScrollY() int {
	return m.scrollY
}

// SelectedIndex returns the current selection index.
func (m *Model) SelectedIndex() int {
	return m.selectedIndex
}

// View renders the Problems panel.
func (m *Model) View() string {
	if len(m.problems) == 0 {
		var sb strings.Builder
		sb.WriteString(m.theme.Gutter.Render("  No problems found"))
		sb.WriteString("\n\n")
		sb.WriteString(m.theme.Gutter.Render("  LSP diagnostics will appear here"))
		sb.WriteString("\n")
		sb.WriteString(m.theme.Gutter.Render("  when you open files with errors"))
		return sb.String()
	}

	var sb strings.Builder
	maxItems := m.height

	startIdx := m.scrollY
	endIdx := min(startIdx+maxItems, len(m.problems))

	for i := startIdx; i < endIdx; i++ {
		if i > startIdx {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderProblem(i))
	}

	return sb.String()
}

// renderProblem renders a single problem line.
func (m *Model) renderProblem(index int) string {
	if index >= len(m.problems) {
		return ""
	}

	p := m.problems[index]
	isSelected := index == m.selectedIndex

	// Severity icon
	icon := "•"
	var severityStyle lipgloss.Style
	switch p.Severity {
	case 1:
		icon = "✗"
		severityStyle = m.theme.DiagError
	case 2:
		icon = "⚠"
		severityStyle = m.theme.DiagWarning
	case 3:
		icon = "ℹ"
		severityStyle = m.theme.DiagInfo
	case 4:
		icon = "ℎ"
		severityStyle = m.theme.DiagHint
	}

	// File path (relative)
	relPath := p.RelativePath(m.rootDir)

	// Line and column
	location := fmt.Sprintf("%d:%d", p.Line+1, p.Col+1)

	// Message (truncated if needed)
	message := p.Message
	maxMessageWidth := m.width - 20 // Reserve space for icon, path, location
	if len(message) > maxMessageWidth {
		message = message[:maxMessageWidth-3] + "..."
	}

	// Build the line
	var parts []string

	// Icon
	parts = append(parts, severityStyle.Render(icon))

	// Space
	parts = append(parts, " ")

	// File path
	pathStyle := m.theme.TreeEntry
	if isSelected {
		pathStyle = m.theme.TreeCursor
	}
	parts = append(parts, pathStyle.Render(relPath))

	// Location
	parts = append(parts, m.theme.Gutter.Render(":"+location))

	// Message
	msgStyle := m.theme.TreeEntry
	if isSelected {
		msgStyle = m.theme.TreeCursor
	}
	parts = append(parts, msgStyle.Render(" "+message))

	line := strings.Join(parts, "")

	// Apply selection background
	if isSelected {
		line = lipgloss.NewStyle().Background(ui.Nord2).Render(line)
	}

	return line
}

// Summary returns a summary string for the status bar.
func (m *Model) Summary() string {
	errors := m.ErrorCount()
	warnings := m.WarningCount()
	total := m.ProblemCount()

	if total == 0 {
		return "No problems"
	}

	parts := []string{}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s)", warnings))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d info/hint(s)", total))
	}

	return strings.Join(parts, ", ")
}

// SelectedPosition returns the position of the selected problem for navigation.
func (m *Model) SelectedPosition() (filePath string, pos text.Position) {
	if len(m.problems) == 0 {
		return "", text.Position{}
	}
	p := m.problems[m.selectedIndex]
	return p.FilePath, text.Position{Line: p.Line, Col: p.Col}
}
