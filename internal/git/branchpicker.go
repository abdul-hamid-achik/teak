package git

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// BranchPickerModel is a modal for selecting git branches.
type BranchPickerModel struct {
	input    textinput.Model
	branches []string
	filtered []string
	current  string
	cursor   int
	scrollY  int
	theme    ui.Theme
	width    int
	height   int
}

// NewBranchPicker creates a new branch picker model.
func NewBranchPicker(theme ui.Theme) BranchPickerModel {
	ti := textinput.New()
	ti.Placeholder = "Switch branch..."
	ti.CharLimit = 128
	return BranchPickerModel{
		input: ti,
		theme: theme,
	}
}

// SetBranches populates the branch list and resets the filter.
func (m *BranchPickerModel) SetBranches(branches []string, current string) {
	m.branches = branches
	m.current = current
	m.cursor = 0
	m.scrollY = 0
	m.input.SetValue("")
	m.filter()
}

// Focus gives focus to the text input.
func (m *BranchPickerModel) Focus() tea.Cmd {
	return m.input.Focus()
}

// SetSize sets the available space for the picker.
func (m *BranchPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles input for the branch picker.
func (m BranchPickerModel) Update(msg tea.Msg) (BranchPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return m, func() tea.Msg { return CloseBranchPickerMsg{} }
		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				branch := m.filtered[m.cursor]
				return m, func() tea.Msg { return SwitchBranchMsg{Branch: branch} }
			}
			return m, nil
		case "up":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollY {
					m.scrollY = m.cursor
				}
			}
			return m, nil
		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				maxVisible := m.maxVisible()
				if m.cursor >= m.scrollY+maxVisible {
					m.scrollY = m.cursor - maxVisible + 1
				}
			}
			return m, nil
		}
	}

	// Forward to text input
	prevVal := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prevVal {
		m.filter()
	}
	return m, cmd
}

// CloseBranchPickerMsg requests closing the branch picker.
type CloseBranchPickerMsg struct{}

func (m *BranchPickerModel) filter() {
	query := strings.ToLower(m.input.Value())
	m.filtered = nil
	for _, b := range m.branches {
		if query == "" || strings.Contains(strings.ToLower(b), query) {
			m.filtered = append(m.filtered, b)
		}
	}
	m.cursor = 0
	m.scrollY = 0
}

func (m BranchPickerModel) maxVisible() int {
	// Reserve lines for input + border
	v := m.height/2 - 4
	if v < 5 {
		v = 5
	}
	if v > 15 {
		v = 15
	}
	return v
}

// View renders the branch picker modal.
func (m BranchPickerModel) View() string {
	maxVisible := m.maxVisible()
	boxWidth := m.width / 2
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 60 {
		boxWidth = 60
	}
	contentWidth := boxWidth - 4 // border + padding

	var sb strings.Builder
	sb.WriteString(m.input.View())
	sb.WriteByte('\n')

	endIdx := m.scrollY + maxVisible
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
	}

	for i := m.scrollY; i < endIdx; i++ {
		b := m.filtered[i]
		prefix := "  "
		if b == m.current {
			prefix = "* "
		}
		label := prefix + truncPath(b, contentWidth-2)

		if i == m.cursor {
			sb.WriteString(m.theme.GitCursor.Width(contentWidth).Render(label))
		} else {
			sb.WriteString(m.theme.GitEntry.Width(contentWidth).Render(label))
		}
		if i < endIdx-1 {
			sb.WriteByte('\n')
		}
	}

	if len(m.filtered) == 0 {
		sb.WriteString(m.theme.GitEntry.Render("  No matching branches"))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 1).
		Width(boxWidth).
		Render(sb.String())
}
