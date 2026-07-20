package git

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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

// branchPickerGeometry is expressed in terminal cells, the same coordinate
// system used by Bubble Tea mouse events. Keeping the modal's layout in one
// place prevents hit testing from drifting from View when terminal dimensions
// or Unicode display widths differ from byte lengths.
type branchPickerGeometry struct {
	x, y           int
	width, height  int
	inputX, inputY int
	inputWidth     int
	listX, listY   int
	listWidth      int
	visible        int
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
			return m, m.switchSelectedCmd()
		case "up":
			m.moveCursor(-1)
			return m, nil
		case "down":
			m.moveCursor(1)
			return m, nil
		}

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		geometry := m.geometry()
		if index, ok := geometry.branchIndexAt(mouse.X, mouse.Y, m.scrollY); ok {
			m.moveCursor(index - m.cursor)
			return m, m.switchSelectedCmd()
		}
		// The modal consumes clicks outside its list. In particular, they
		// must never reach the editor below it. Text input currently has no
		// mouse cursor behavior, but keep its event path explicit so adding
		// one later does not reopen the modal/event-routing bug.
		if geometry.inputContains(mouse.X, mouse.Y) {
			return m.updateInput(msg)
		}
		return m, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		geometry := m.geometry()
		if !geometry.listContains(mouse.X, mouse.Y) {
			return m, nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.moveCursor(-1)
		case tea.MouseWheelDown:
			m.moveCursor(1)
		}
		return m, nil

	case tea.MouseReleaseMsg:
		// Releases are intentionally consumed by the modal. Forwarding a
		// release to text input or the editor is both unnecessary and can
		// complete a drag that began before the picker opened.
		return m, nil
	}

	return m.updateInput(msg)
}

func (m BranchPickerModel) switchSelectedCmd() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	branch := m.filtered[m.cursor]
	// Never run a redundant checkout for the checked-out branch: it can
	// disrupt a repository hook without changing user-visible state.
	if branch == m.current {
		return nil
	}
	return func() tea.Msg { return SwitchBranchMsg{Branch: branch} }
}

func (m BranchPickerModel) updateInput(msg tea.Msg) (BranchPickerModel, tea.Cmd) {
	// Forward keyboard and text-input messages to the focused filter.
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

// moveCursor clamps selection and keeps it within the rendered list. It is
// shared by keyboard and wheel navigation so every input modality has the
// same boundary behavior.
func (m *BranchPickerModel) moveCursor(delta int) {
	if len(m.filtered) == 0 {
		m.cursor = 0
		m.scrollY = 0
		return
	}

	m.cursor = min(max(0, m.cursor+delta), len(m.filtered)-1)
	visible := m.maxVisible()
	if m.cursor < m.scrollY {
		m.scrollY = m.cursor
	}
	if m.cursor >= m.scrollY+visible {
		m.scrollY = m.cursor - visible + 1
	}
	maxScroll := max(0, len(m.filtered)-visible)
	m.scrollY = min(max(0, m.scrollY), maxScroll)
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

func (m BranchPickerModel) boxWidth() int {
	boxWidth := m.width / 2
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 60 {
		boxWidth = 60
	}
	return boxWidth
}

// geometry mirrors View and ui.RenderOverlay. Style.Width specifies the
// complete rendered block width, including the one-cell border and padding;
// all coordinates here are therefore display cells, not string bytes/runes.
func (m BranchPickerModel) geometry() branchPickerGeometry {
	boxWidth := m.boxWidth()
	visible := min(len(m.filtered), m.maxVisible())
	if visible == 0 {
		// View renders the no-match line in place of a list item.
		visible = 1
	}

	// content has input plus visible list rows; the outer style adds a one-cell
	// border and one-cell padding on every side.
	boxHeight := 1 + visible + 4
	x := max(0, (m.width-boxWidth)/2)
	y := max(0, (m.height-boxHeight)/2)
	contentX := x + 2
	contentRight := min(m.width, contentX+boxWidth-4)
	contentWidth := max(0, contentRight-contentX)
	listY := y + 3 // border + top padding + input row
	listEnd := min(m.height, listY+visible)
	visibleOnScreen := max(0, listEnd-listY)
	if listY >= m.height {
		visibleOnScreen = 0
	}

	return branchPickerGeometry{
		x:          x,
		y:          y,
		width:      boxWidth,
		height:     boxHeight,
		inputX:     contentX,
		inputY:     y + 2,
		inputWidth: contentWidth,
		listX:      contentX,
		listY:      listY,
		listWidth:  contentWidth,
		visible:    visibleOnScreen,
	}
}

func (g branchPickerGeometry) inputContains(x, y int) bool {
	return y == g.inputY && x >= g.inputX && x < g.inputX+g.inputWidth
}

func (g branchPickerGeometry) listContains(x, y int) bool {
	return x >= g.listX && x < g.listX+g.listWidth && y >= g.listY && y < g.listY+g.visible
}

func (g branchPickerGeometry) branchIndexAt(x, y, scrollY int) (int, bool) {
	if !g.listContains(x, y) {
		return 0, false
	}
	return scrollY + y - g.listY, true
}

// View renders the branch picker modal.
func (m BranchPickerModel) View() string {
	maxVisible := m.maxVisible()
	boxWidth := m.boxWidth()
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
