package app

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/dap"
)

// DAPCoordinator manages DAP debug session lifecycle and event handling.
type DAPCoordinator struct {
	mu            sync.RWMutex
	mgr           *dap.Manager
	state         dap.DebugState
	stackFrames   []dap.StackFrame
	variables     []dap.Variable
	outputLog     []string
	currentFrame  int
	maxOutputLines int
}

// NewDAPCoordinator creates a new DAP coordinator.
func NewDAPCoordinator(mgr *dap.Manager) *DAPCoordinator {
	return &DAPCoordinator{
		mgr:           mgr,
		state:         dap.StateInactive,
		stackFrames:   make([]dap.StackFrame, 0),
		variables:     make([]dap.Variable, 0),
		outputLog:     make([]string, 0),
		maxOutputLines: 200,
	}
}

// HandleMessage routes DAP messages to appropriate handlers.
func (c *DAPCoordinator) HandleMessage(msg tea.Msg) []tea.Cmd {
	switch m := msg.(type) {
	case dap.StoppedEventMsg:
		return c.handleStopped(m)
	case dap.ContinuedEventMsg:
		return c.handleContinued(m)
	case dap.TerminatedEventMsg:
		return c.handleTerminated(m)
	case dap.ExitedEventMsg:
		return c.handleExited(m)
	case dap.OutputEventMsg:
		return c.handleOutput(m)
	case dap.BreakpointEventMsg:
		return c.handleBreakpoint(m)
	default:
		return nil
	}
}

// handleStopped handles debug session stopped event.
func (c *DAPCoordinator) handleStopped(msg dap.StoppedEventMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = dap.StatePaused
	// Will fetch stack trace and variables via separate command
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleContinued handles debug session continued event.
func (c *DAPCoordinator) handleContinued(msg dap.ContinuedEventMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = dap.StateRunning
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleTerminated handles debug session terminated event.
func (c *DAPCoordinator) handleTerminated(msg dap.TerminatedEventMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = dap.StateInactive
	c.outputLog = nil
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleExited handles process exited event.
func (c *DAPCoordinator) handleExited(msg dap.ExitedEventMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = dap.StateInactive
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleOutput handles output event (stdout/stderr from debuggee).
func (c *DAPCoordinator) handleOutput(msg dap.OutputEventMsg) []tea.Cmd {
	c.AppendOutput(strings.TrimRight(msg.Output, "\n"))
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleBreakpoint handles breakpoint changed event.
func (c *DAPCoordinator) handleBreakpoint(msg dap.BreakpointEventMsg) []tea.Cmd {
	// Acknowledge but don't return commands - breakpoint state is tracked separately
	return nil
}

// SetStackFrames sets the current stack frames.
func (c *DAPCoordinator) SetStackFrames(frames []dap.StackFrame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stackFrames = frames
	c.currentFrame = 0
}

// SetVariables sets the current variables.
func (c *DAPCoordinator) SetVariables(vars []dap.Variable) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.variables = vars
}

// SetState sets the debug state.
func (c *DAPCoordinator) SetState(state dap.DebugState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
	if state == dap.StateInactive {
		c.stackFrames = nil
		c.variables = nil
	}
}

// GetState returns the current debug state.
func (c *DAPCoordinator) GetState() dap.DebugState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// IsRunning returns true if debug session is running.
func (c *DAPCoordinator) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state == dap.StateRunning || c.state == dap.StatePaused
}

// IsPaused returns true if debug session is paused.
func (c *DAPCoordinator) IsPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state == dap.StatePaused
}

// SelectFrame selects a stack frame and returns a command to jump to it.
func (c *DAPCoordinator) SelectFrame(idx int) tea.Cmd {
	c.mu.RLock()
	if idx < 0 || idx >= len(c.stackFrames) {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()
	
	c.mu.Lock()
	c.currentFrame = idx
	frame := c.stackFrames[idx]
	c.mu.Unlock()
	
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

// GetCurrentFrame returns the currently selected stack frame.
func (c *DAPCoordinator) GetCurrentFrame() dap.StackFrame {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.stackFrames) == 0 {
		return dap.StackFrame{}
	}
	return c.stackFrames[c.currentFrame]
}

// GetStackFrames returns all stack frames.
func (c *DAPCoordinator) GetStackFrames() []dap.StackFrame {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stackFrames
}

// GetVariables returns all variables.
func (c *DAPCoordinator) GetVariables() []dap.Variable {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.variables
}

// AppendOutput adds a line to the output log.
func (c *DAPCoordinator) AppendOutput(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outputLog = append(c.outputLog, line)
	// Keep only last maxOutputLines
	if len(c.outputLog) > c.maxOutputLines {
		c.outputLog = c.outputLog[len(c.outputLog)-c.maxOutputLines:]
	}
}

// ClearOutput clears the output log.
func (c *DAPCoordinator) ClearOutput() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outputLog = nil
}

// GetOutputLog returns the output log.
func (c *DAPCoordinator) GetOutputLog() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outputLog
}

// GetCurrentFrameIndex returns the current frame index.
func (c *DAPCoordinator) GetCurrentFrameIndex() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentFrame
}

// GetStackFrameCount returns the number of stack frames.
func (c *DAPCoordinator) GetStackFrameCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.stackFrames)
}

// Shutdown stops the debug session.
func (c *DAPCoordinator) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mgr != nil {
		c.mgr.Stop()
	}
	c.state = dap.StateInactive
}
