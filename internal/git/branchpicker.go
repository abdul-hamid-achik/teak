package git

import (
	"context"
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

	filterPending    bool
	filterGeneration uint64
	filterCancel     context.CancelFunc
	pendingSwitch    bool
}

// BranchFilterReadyMsg carries a branch projection prepared outside the
// Bubble Tea update loop. Generation and query make slower obsolete results
// harmless after another character is typed or the picker is dismissed.
type BranchFilterReadyMsg struct {
	Generation uint64
	Query      string
	Branches   []string
	Err        error
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
	m.cancelFilter()
	m.branches = branches
	m.filtered = branches
	m.current = current
	m.cursor = 0
	m.scrollY = 0
	m.pendingSwitch = false
	m.input.SetValue("")
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
			m.cancelFilter()
			m.pendingSwitch = false
			return m, func() tea.Msg { return CloseBranchPickerMsg{} }
		case "enter":
			if m.filterPending {
				m.pendingSwitch = true
				return m, nil
			}
			return m, m.switchSelectedCmd()
		case "up":
			m.moveCursor(-1)
			return m, nil
		case "down":
			m.moveCursor(1)
			return m, nil
		}

	case tea.MouseClickMsg:
		if m.filterPending {
			return m, nil
		}
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
		if m.filterPending {
			return m, nil
		}
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

	case BranchFilterReadyMsg:
		return m, m.handleFilterReady(msg)
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
		m.pendingSwitch = false
		return m, tea.Batch(cmd, m.scheduleFilter())
	}
	return m, cmd
}

// CloseBranchPickerMsg requests closing the branch picker.
type CloseBranchPickerMsg struct{}

func (m *BranchPickerModel) scheduleFilter() tea.Cmd {
	m.cancelFilter()
	query := m.input.Value()
	m.cursor = 0
	m.scrollY = 0
	m.filtered = nil
	if query == "" {
		m.filtered = m.branches
		return nil
	}

	m.filterGeneration++
	generation := m.filterGeneration
	branches := m.branches
	ctx, cancel := context.WithCancel(context.Background())
	m.filterCancel = cancel
	m.filterPending = true
	return func() tea.Msg {
		filtered, err := filterBranchesContext(ctx, branches, query)
		return BranchFilterReadyMsg{
			Generation: generation,
			Query:      query,
			Branches:   filtered,
			Err:        err,
		}
	}
}

func (m *BranchPickerModel) handleFilterReady(msg BranchFilterReadyMsg) tea.Cmd {
	if !m.filterPending || msg.Generation != m.filterGeneration || msg.Query != m.input.Value() {
		return nil
	}
	m.filterCancel = nil
	m.filterPending = false
	if msg.Err != nil {
		m.filtered = nil
		m.pendingSwitch = false
		return nil
	}
	m.filtered = msg.Branches
	m.cursor = 0
	m.scrollY = 0
	if !m.pendingSwitch {
		return nil
	}
	m.pendingSwitch = false
	return m.switchSelectedCmd()
}

func (m *BranchPickerModel) cancelFilter() {
	if m.filterCancel != nil {
		m.filterCancel()
		m.filterCancel = nil
	}
	m.filterPending = false
	m.filterGeneration++
}

// CancelFilter stops pending projection work when the picker is hidden.
func (m *BranchPickerModel) CancelFilter() {
	m.cancelFilter()
	m.pendingSwitch = false
}

func filterBranchesContext(ctx context.Context, branches []string, query string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	if query == "" {
		return branches, nil
	}

	matches := make([]string, 0, min(len(branches), 256))
	for i, branch := range branches {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if strings.Contains(strings.ToLower(branch), query) {
			matches = append(matches, branch)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return matches, nil
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

	for i := m.scrollY; !m.filterPending && i < endIdx; i++ {
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

	if m.filterPending {
		sb.WriteString(m.theme.GitEntry.Render("  Filtering branches..."))
	} else if len(m.filtered) == 0 {
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
