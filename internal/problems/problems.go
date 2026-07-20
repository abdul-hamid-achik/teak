package problems

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	Severity int // 1=error, 2=warning, 3=info, 4=hint
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
	errorCount    int
	warningCount  int
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
	m.clampScroll()
}

// SetProblems updates the problems list and rebuilds groups.
func (m *Model) SetProblems(problems []Problem) {
	m.problems = append(m.problems[:0], problems...)
	slices.SortStableFunc(m.problems, Compare)
	m.groups = m.buildGroups()
	m.errorCount, m.warningCount = problemSeverityCounts(m.problems)
	// Keep selection in bounds
	if m.selectedIndex >= len(m.problems) {
		m.selectedIndex = max(0, len(m.problems)-1)
	}
	m.clampScroll()
}

// ReplaceFileProblems replaces diagnostics for one file without rebuilding the
// complete per-file index. The global list remains ordered for keyboard and
// mouse navigation; replacing a file is linear in the visible problem count,
// rather than a full map rebuild plus an O(P log P) re-sort.
func (m *Model) ReplaceFileProblems(filePath string, fileProblems []Problem) {
	oldErrors, oldWarnings := problemSeverityCountsForFile(m.problems, filePath)
	newProblems := append([]Problem(nil), fileProblems...)
	slices.SortStableFunc(newProblems, Compare)
	newErrors, newWarnings := problemSeverityCounts(newProblems)

	remaining := m.problems[:0]
	for _, p := range m.problems {
		if p.FilePath != filePath {
			remaining = append(remaining, p)
		}
	}
	m.problems = mergeProblemsInPlace(remaining, newProblems)
	m.errorCount += newErrors - oldErrors
	m.warningCount += newWarnings - oldWarnings
	m.replaceGroup(filePath, newProblems)
	if m.selectedIndex >= len(m.problems) {
		m.selectedIndex = max(0, len(m.problems)-1)
	}
	m.clampScroll()
}

// Compare orders problems exactly as the panel presents them. The final
// comparison leaves truly identical diagnostics stable in their source order.
func Compare(a, b Problem) int {
	if a.Severity != b.Severity {
		if a.Severity < b.Severity {
			return -1
		}
		return 1
	}
	if result := strings.Compare(a.FilePath, b.FilePath); result != 0 {
		return result
	}
	if a.Line != b.Line {
		if a.Line < b.Line {
			return -1
		}
		return 1
	}
	if a.Col != b.Col {
		if a.Col < b.Col {
			return -1
		}
		return 1
	}
	if a.EndLine != b.EndLine {
		if a.EndLine < b.EndLine {
			return -1
		}
		return 1
	}
	if a.EndCol != b.EndCol {
		if a.EndCol < b.EndCol {
			return -1
		}
		return 1
	}
	if result := strings.Compare(a.Message, b.Message); result != 0 {
		return result
	}
	return strings.Compare(a.Source, b.Source)
}

func mergeProblemsInPlace(left, right []Problem) []Problem {
	total := len(left) + len(right)
	if cap(left) < total {
		capacity := max(16, cap(left)*2)
		if capacity < total {
			capacity = total
		}
		grown := make([]Problem, len(left), capacity)
		copy(grown, left)
		left = grown
	}
	left = left[:total]
	for leftIndex, rightIndex, outputIndex := total-len(right)-1, len(right)-1, total-1; outputIndex >= 0; outputIndex-- {
		if rightIndex < 0 || (leftIndex >= 0 && Compare(left[leftIndex], right[rightIndex]) > 0) {
			left[outputIndex] = left[leftIndex]
			leftIndex--
			continue
		}
		left[outputIndex] = right[rightIndex]
		rightIndex--
	}
	return left
}

func (m *Model) replaceGroup(filePath string, fileProblems []Problem) {
	index, found := slices.BinarySearchFunc(m.groups, filePath, func(group Group, path string) int {
		return strings.Compare(group.FilePath, path)
	})
	if len(fileProblems) == 0 {
		if found {
			m.groups = append(m.groups[:index], m.groups[index+1:]...)
		}
		return
	}
	group := Group{FilePath: filePath, Problems: append([]Problem(nil), fileProblems...)}
	if found {
		m.groups[index] = group
		return
	}
	m.groups = append(m.groups, Group{})
	copy(m.groups[index+1:], m.groups[index:])
	m.groups[index] = group
}

func problemSeverityCounts(problems []Problem) (errors, warnings int) {
	for _, p := range problems {
		switch p.Severity {
		case 1:
			errors++
		case 2:
			warnings++
		}
	}
	return errors, warnings
}

func problemSeverityCountsForFile(problems []Problem, filePath string) (errors, warnings int) {
	for _, p := range problems {
		if p.FilePath != filePath {
			continue
		}
		switch p.Severity {
		case 1:
			errors++
		case 2:
			warnings++
		}
	}
	return errors, warnings
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
	slices.SortFunc(groups, func(a, b Group) int {
		return strings.Compare(a.FilePath, b.FilePath)
	})
}

// ProblemCount returns the total number of problems.
func (m *Model) ProblemCount() int {
	return len(m.problems)
}

// ErrorCount returns the number of errors.
func (m *Model) ErrorCount() int {
	return m.errorCount
}

// WarningCount returns the number of warnings.
func (m *Model) WarningCount() int {
	return m.warningCount
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
	if m.height <= 0 {
		m.scrollY = 0
		return
	}
	if m.selectedIndex < m.scrollY {
		m.scrollY = m.selectedIndex
	}
	if m.selectedIndex >= m.scrollY+m.height {
		m.scrollY = m.selectedIndex - m.height + 1
	}
	m.clampScroll()
}

// ScrollUp scrolls up by n items.
func (m *Model) ScrollUp(n int) {
	if m.height <= 0 {
		m.scrollY = 0
		return
	}
	m.scrollY -= n
	m.clampScroll()
}

// ScrollDown scrolls down by n items.
func (m *Model) ScrollDown(n int) {
	if m.height <= 0 {
		m.scrollY = 0
		return
	}
	m.scrollY += n
	m.clampScroll()
}

func (m *Model) clampScroll() {
	if m.height <= 0 {
		m.scrollY = 0
		return
	}
	maxScroll := max(0, len(m.problems)-m.height)
	if m.scrollY < 0 {
		m.scrollY = 0
	} else if m.scrollY > maxScroll {
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

// SelectAt selects a problem by its absolute index.
func (m *Model) SelectAt(index int) bool {
	if index < 0 || index >= len(m.problems) {
		return false
	}
	m.selectedIndex = index
	m.ensureVisible()
	return true
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
	maxMessageWidth := max(0, m.width-20) // Reserve space for icon, path, location.
	message := ansi.Truncate(p.Message, maxMessageWidth, "...")

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
