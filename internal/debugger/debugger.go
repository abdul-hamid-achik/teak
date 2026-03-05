package debugger

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/dap"
	"teak/internal/ui"
)

// JumpToFrameMsg is emitted when the user clicks a stack frame.
type JumpToFrameMsg struct {
	FilePath string
	Line     int // 0-based
}

// ExpandVariableMsg is emitted when the user wants to expand a variable.
type ExpandVariableMsg struct {
	VariablesReference int
}

// Model represents the debugger panel state.
type Model struct {
	width           int
	height          int
	theme           ui.Theme
	state           dap.DebugState
	stackFrames     []dap.StackFrame
	variables       []dap.Variable
	breakpoints     []Breakpoint
	outputLog       []string
	currentFrame    int
	scrollY         int
	showBreakpoints bool
	expandedVars    map[int][]dap.Variable // variablesReference → children
}

// Breakpoint represents a breakpoint in the UI.
type Breakpoint struct {
	FilePath string
	Line     int
	Enabled  bool
	Verified bool
}

// New creates a new debugger panel model.
func New(theme ui.Theme) Model {
	return Model{
		theme:           theme,
		state:           dap.StateInactive,
		showBreakpoints: true,
		expandedVars:    make(map[int][]dap.Variable),
	}
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetState sets the debug state.
func (m *Model) SetState(state dap.DebugState) {
	m.state = state
	if state == dap.StateInactive {
		m.stackFrames = nil
		m.variables = nil
		m.expandedVars = make(map[int][]dap.Variable)
	}
}

// SetStackFrames sets the stack frames.
func (m *Model) SetStackFrames(frames []dap.StackFrame) {
	m.stackFrames = frames
	m.currentFrame = 0
}

// SetVariables sets the variables.
func (m *Model) SetVariables(vars []dap.Variable) {
	m.variables = vars
	m.expandedVars = make(map[int][]dap.Variable)
}

// SetExpandedVariables stores child variables for a parent reference.
func (m *Model) SetExpandedVariables(ref int, vars []dap.Variable) {
	m.expandedVars[ref] = vars
}

// IsExpanded returns whether a variable reference has been expanded.
func (m *Model) IsExpanded(ref int) bool {
	_, ok := m.expandedVars[ref]
	return ok
}

// ToggleExpand toggles expansion state. Returns (needsFetch, variablesReference).
func (m *Model) ToggleExpand(ref int) bool {
	if _, ok := m.expandedVars[ref]; ok {
		delete(m.expandedVars, ref)
		return false
	}
	return true // needs fetch
}

// SetBreakpoints sets the breakpoints.
func (m *Model) SetBreakpoints(bps []Breakpoint) {
	m.breakpoints = bps
}

// AppendOutput adds a line to the output log.
func (m *Model) AppendOutput(line string) {
	m.outputLog = append(m.outputLog, line)
	if len(m.outputLog) > 200 {
		m.outputLog = m.outputLog[len(m.outputLog)-200:]
	}
}

// ClearOutput clears the output log.
func (m *Model) ClearOutput() {
	m.outputLog = nil
}

// State returns the current debug state.
func (m *Model) State() dap.DebugState {
	return m.state
}

// SelectFrame selects a stack frame and returns a JumpToFrameMsg cmd.
func (m *Model) SelectFrame(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.stackFrames) {
		return nil
	}
	m.currentFrame = idx
	frame := m.stackFrames[idx]
	if frame.Source.Path == "" {
		return nil
	}
	return func() tea.Msg {
		return JumpToFrameMsg{
			FilePath: frame.Source.Path,
			Line:     frame.Line - 1, // DAP is 1-based, we use 0-based
		}
	}
}

// CurrentFrame returns the currently selected frame index.
func (m *Model) CurrentFrame() int {
	return m.currentFrame
}

// View renders the debugger panel.
func (m *Model) View() string {
	if m.state == dap.StateInactive {
		return m.renderInactive()
	}

	var sb strings.Builder

	// State indicator
	stateStr := m.state.String()
	var stateRendered string
	switch m.state {
	case dap.StateRunning:
		stateRendered = m.theme.Gutter.Render(fmt.Sprintf(" ● %s", stateStr))
	case dap.StateStopped:
		stateRendered = m.theme.DiagWarning.Render(fmt.Sprintf(" ● %s", stateStr))
	case dap.StatePaused:
		stateRendered = m.theme.DiagInfo.Render(fmt.Sprintf(" ● %s", stateStr))
	}
	sb.WriteString(stateRendered)
	sb.WriteString("\n\n")

	// Controls
	sb.WriteString(m.renderControls())
	sb.WriteString("\n\n")

	// Stack trace
	sb.WriteString(m.renderStackTrace())
	sb.WriteString("\n")

	// Variables
	sb.WriteString(m.renderVariables())

	// Breakpoints
	if m.showBreakpoints && len(m.breakpoints) > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.BreakpointView())
	}

	// Output log
	sb.WriteString(m.renderOutput())

	return sb.String()
}

// renderInactive renders the panel when no debug session is active.
func (m *Model) renderInactive() string {
	var sb strings.Builder

	sb.WriteString(m.theme.Gutter.Render("  No active debug session"))
	sb.WriteString("\n\n")

	sb.WriteString(m.theme.GitActionButton.Render(" ▶ Start Debugging (F5) "))
	sb.WriteString("\n\n")

	sb.WriteString(m.theme.Gutter.Render("  Press F5 or use Command Palette"))
	sb.WriteString("\n")
	sb.WriteString(m.theme.Gutter.Render("  to start debugging"))

	if len(m.breakpoints) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(m.BreakpointView())
	}

	return sb.String()
}

// renderControls renders the debug control buttons.
func (m *Model) renderControls() string {
	if m.state == dap.StateInactive {
		return ""
	}

	controls := []string{
		m.renderControlButton("▶", "Continue", "c"),
		m.renderControlButton("⏭", "Next", "n"),
		m.renderControlButton("⤵", "Step In", "i"),
		m.renderControlButton("⤴", "Step Out", "o"),
		m.renderControlButton("⏹", "Stop", "q"),
	}

	return strings.Join(controls, " ")
}

// renderControlButton renders a single control button.
func (m *Model) renderControlButton(icon, label, shortcut string) string {
	style := m.theme.GitActionButton
	return style.Render(fmt.Sprintf("%s %s", icon, shortcut))
}

// renderStackTrace renders the call stack.
func (m *Model) renderStackTrace() string {
	if len(m.stackFrames) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(m.theme.GitSectionHeader.Render("Call Stack"))
	sb.WriteString("\n")

	maxFrames := 8
	startIdx := m.scrollY
	endIdx := min(startIdx+maxFrames, len(m.stackFrames))

	for i := startIdx; i < endIdx; i++ {
		frame := m.stackFrames[i]
		isCurrent := i == m.currentFrame

		line := fmt.Sprintf("  %d: %s", i, frame.Name)
		if frame.Source.Path != "" {
			line += fmt.Sprintf(" (%s:%d)", frame.Source.Name, frame.Line)
		}

		if isCurrent {
			line = m.theme.TreeCursor.Render(line)
		} else {
			line = m.theme.TreeEntry.Render(line)
		}

		sb.WriteString(line)
		if i < endIdx-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderVariables renders the variables panel.
func (m *Model) renderVariables() string {
	if len(m.variables) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(m.theme.GitSectionHeader.Render("Variables"))
	sb.WriteString("\n")

	m.renderVarList(&sb, m.variables, 0, 10)

	return sb.String()
}

// renderVarList renders a list of variables with indentation and expansion.
func (m *Model) renderVarList(sb *strings.Builder, vars []dap.Variable, depth int, remaining int) int {
	indent := strings.Repeat("  ", depth+1)
	for _, v := range vars {
		if remaining <= 0 {
			break
		}

		// Expansion indicator for composite types
		prefix := " "
		if v.VariablesReference > 0 {
			if m.IsExpanded(v.VariablesReference) {
				prefix = "▼"
			} else {
				prefix = "▶"
			}
		}

		line := fmt.Sprintf("%s%s %s: %s", indent, prefix, v.Name, v.Value)
		if v.Type != "" {
			line += fmt.Sprintf(" (%s)", v.Type)
		}
		sb.WriteString(m.theme.TreeEntry.Render(line))
		sb.WriteString("\n")
		remaining--

		// Render expanded children
		if v.VariablesReference > 0 {
			if children, ok := m.expandedVars[v.VariablesReference]; ok {
				remaining = m.renderVarList(sb, children, depth+1, remaining)
			}
		}
	}
	return remaining
}

// renderOutput renders the debug output log.
func (m *Model) renderOutput() string {
	if len(m.outputLog) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(m.theme.GitSectionHeader.Render("Output"))
	sb.WriteString("\n")

	start := len(m.outputLog) - 8
	if start < 0 {
		start = 0
	}
	for i := start; i < len(m.outputLog); i++ {
		sb.WriteString(m.theme.TreeEntry.Render("  " + m.outputLog[i]))
		if i < len(m.outputLog)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// BreakpointView renders the breakpoints list.
func (m *Model) BreakpointView() string {
	if len(m.breakpoints) == 0 {
		return m.theme.Gutter.Render("  No breakpoints")
	}

	var sb strings.Builder
	sb.WriteString(m.theme.GitSectionHeader.Render("Breakpoints"))
	sb.WriteString("\n")

	for _, bp := range m.breakpoints {
		icon := "○"
		if bp.Verified {
			icon = "●"
		}
		if !bp.Enabled {
			icon = "◌"
		}

		status := m.theme.Gutter.Render(icon)
		path := bp.FilePath
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			path = path[idx+1:]
		}

		line := fmt.Sprintf("  %s %s:%d", status, path, bp.Line+1) // display as 1-based
		sb.WriteString(m.theme.TreeEntry.Render(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

// Status returns a status string for the status bar.
func (m *Model) Status() string {
	switch m.state {
	case dap.StateRunning:
		return "Debugging"
	case dap.StateStopped:
		if len(m.stackFrames) > 0 {
			frame := m.stackFrames[m.currentFrame]
			return fmt.Sprintf("Stopped at %s:%d", frame.Source.Name, frame.Line)
		}
		return "Stopped"
	case dap.StatePaused:
		if len(m.stackFrames) > 0 {
			frame := m.stackFrames[m.currentFrame]
			return fmt.Sprintf("Paused at %s:%d", frame.Source.Name, frame.Line)
		}
		return "Paused"
	default:
		return ""
	}
}
