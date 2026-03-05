package dap

// DAP event messages

// StoppedEventMsg is sent when the debugger stops.
type StoppedEventMsg struct {
	Reason            string
	Description       string
	ThreadId          int
	AllThreadsStopped bool
}

// ContinuedEventMsg is sent when the debugger continues.
type ContinuedEventMsg struct {
	ThreadId            int
	AllThreadsContinued bool
}

// ExitedEventMsg is sent when the debugged process exits.
type ExitedEventMsg struct {
	ExitCode int
}

// TerminatedEventMsg is sent when the debug session terminates.
type TerminatedEventMsg struct{}

// OutputEventMsg is sent when the debugger outputs text.
type OutputEventMsg struct {
	Category string
	Output   string
}

// BreakpointEventMsg is sent when a breakpoint changes.
type BreakpointEventMsg struct {
	Reason     string
	Breakpoint Breakpoint
}

// DebugState represents the current state of the debugger.
type DebugState int

const (
	StateInactive DebugState = iota
	StateRunning
	StateStopped
	StatePaused
)

// String returns a string representation of the debug state.
func (s DebugState) String() string {
	switch s {
	case StateInactive:
		return "inactive"
	case StateRunning:
		return "running"
	case StateStopped:
		return "stopped"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}
