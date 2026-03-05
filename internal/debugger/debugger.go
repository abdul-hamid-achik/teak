package debugger

import (
	"fmt"
	"strings"

	"teak/internal/dap"
	"teak/internal/ui"
)

// Model represents the debugger panel state.
type Model struct {
	width           int
	height          int
	theme           ui.Theme
	state           dap.DebugState
	stackFrames     []dap.StackFrame
	variables       []dap.Variable
	breakpoints     []Breakpoint
	currentFrame    int
	scrollY         int
	showBreakpoints bool
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
}

// SetStackFrames sets the stack frames.
func (m *Model) SetStackFrames(frames []dap.StackFrame) {
	m.stackFrames = frames
	m.currentFrame = 0
}

// SetVariables sets the variables.
func (m *Model) SetVariables(vars []dap.Variable) {
	m.variables = vars
}

// SetBreakpoints sets the breakpoints.
func (m *Model) SetBreakpoints(bps []Breakpoint) {
	m.breakpoints = bps
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

	// Controls with clickable buttons
	sb.WriteString(m.renderControls())
	sb.WriteString("\n\n")

	// Stack trace
	sb.WriteString(m.renderStackTrace())
	sb.WriteString("\n")

	// Variables
	sb.WriteString(m.renderVariables())

	return sb.String()
}

// renderInactive renders the panel when no debug session is active.
func (m *Model) renderInactive() string {
	var sb strings.Builder
	
	sb.WriteString(m.theme.Gutter.Render("  No active debug session"))
	sb.WriteString("\n\n")
	
	// Show action buttons
	sb.WriteString(m.theme.GitActionButton.Render(" ▶ Start Debugging (F5) "))
	sb.WriteString("\n\n")
	
	sb.WriteString(m.theme.Gutter.Render("  Press F5 or use Command Palette"))
	sb.WriteString("\n")
	sb.WriteString(m.theme.Gutter.Render("  to start debugging"))
	
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

	maxVars := 10
	for i := 0; i < len(m.variables) && i < maxVars; i++ {
		v := m.variables[i]
		line := fmt.Sprintf("  %s: %s", v.Name, v.Value)
		if v.Type != "" {
			line += fmt.Sprintf(" (%s)", v.Type)
		}
		sb.WriteString(m.theme.TreeEntry.Render(line))
		if i < min(len(m.variables), maxVars)-1 {
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

		line := fmt.Sprintf("  %s %s:%d", status, path, bp.Line)
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
		return "Paused"
	default:
		return ""
	}
}
