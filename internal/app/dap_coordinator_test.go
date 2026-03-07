package app

import (
	"testing"

	"teak/internal/dap"
)

// TestDAPCoordinatorCreation tests that the coordinator can be created
func TestDAPCoordinatorCreation(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}

	if coord.stackFrames == nil {
		t.Error("expected stackFrames slice to be initialized")
	}

	if coord.variables == nil {
		t.Error("expected variables slice to be initialized")
	}
}

// TestDAPCoordinatorHandleStopped tests stopped event handling
func TestDAPCoordinatorHandleStopped(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	msg := dap.StoppedEventMsg{
		Reason: "breakpoint",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}

	if coord.state != dap.StatePaused {
		t.Errorf("expected state Paused, got %v", coord.state)
	}
}

// TestDAPCoordinatorHandleContinued tests continued event handling
func TestDAPCoordinatorHandleContinued(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	coord.state = dap.StatePaused

	msg := dap.ContinuedEventMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}

	if coord.state != dap.StateRunning {
		t.Errorf("expected state Running, got %v", coord.state)
	}
}

// TestDAPCoordinatorHandleTerminated tests terminated event handling
func TestDAPCoordinatorHandleTerminated(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	coord.state = dap.StateRunning

	msg := dap.TerminatedEventMsg{}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}

	if coord.state != dap.StateInactive {
		t.Errorf("expected state Inactive, got %v", coord.state)
	}
}

// TestDAPCoordinatorHandleExited tests exited event handling
func TestDAPCoordinatorHandleExited(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	coord.state = dap.StateRunning

	msg := dap.ExitedEventMsg{
		ExitCode: 0,
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}

	if coord.state != dap.StateInactive {
		t.Errorf("expected state Inactive, got %v", coord.state)
	}
}

// TestDAPCoordinatorHandleOutput tests output event handling
func TestDAPCoordinatorHandleOutput(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	msg := dap.OutputEventMsg{
		Output: "test output\n",
	}

	cmds := coord.HandleMessage(msg)
	if cmds == nil {
		t.Error("expected commands to be returned")
	}

	if len(coord.outputLog) != 1 {
		t.Errorf("expected 1 output line, got %d", len(coord.outputLog))
	}
}

// TestDAPCoordinatorHandleBreakpoint tests breakpoint event handling
func TestDAPCoordinatorHandleBreakpoint(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	msg := dap.BreakpointEventMsg{
		Reason: "changed",
	}

	cmds := coord.HandleMessage(msg)
	// Breakpoint events are acknowledged but don't return commands
	if cmds != nil {
		t.Error("expected nil commands for breakpoint event")
	}
}

// TestDAPCoordinatorSetStackFrames tests setting stack frames
func TestDAPCoordinatorSetStackFrames(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Line: 10},
		{Id: 2, Name: "test", Line: 20},
	}

	coord.SetStackFrames(frames)

	if len(coord.stackFrames) != 2 {
		t.Errorf("expected 2 stack frames, got %d", len(coord.stackFrames))
	}

	if coord.currentFrame != 0 {
		t.Errorf("expected currentFrame 0, got %d", coord.currentFrame)
	}
}

// TestDAPCoordinatorSetVariables tests setting variables
func TestDAPCoordinatorSetVariables(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	vars := []dap.Variable{
		{Name: "x", Value: "1"},
		{Name: "y", Value: "2"},
	}

	coord.SetVariables(vars)

	if len(coord.variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(coord.variables))
	}
}

// TestDAPCoordinatorGetState tests getting debug state
func TestDAPCoordinatorGetState(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	if coord.GetState() != dap.StateInactive {
		t.Errorf("expected state Inactive, got %v", coord.GetState())
	}

	coord.state = dap.StateRunning
	if coord.GetState() != dap.StateRunning {
		t.Errorf("expected state Running, got %v", coord.GetState())
	}
}

// TestDAPCoordinatorSetState tests setting debug state
func TestDAPCoordinatorSetState(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	coord.SetState(dap.StateRunning)
	if coord.state != dap.StateRunning {
		t.Errorf("expected state Running, got %v", coord.state)
	}

	coord.SetState(dap.StatePaused)
	if coord.state != dap.StatePaused {
		t.Errorf("expected state Paused, got %v", coord.state)
	}
}

// TestDAPCoordinatorSelectFrame tests frame selection
func TestDAPCoordinatorSelectFrame(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Line: 10, Source: dap.Source{Path: "/test.go"}},
		{Id: 2, Name: "test", Line: 20, Source: dap.Source{Path: "/test2.go"}},
	}
	coord.SetStackFrames(frames)

	cmd := coord.SelectFrame(1)
	if cmd == nil {
		t.Error("expected command to be returned")
	}

	if coord.currentFrame != 1 {
		t.Errorf("expected currentFrame 1, got %d", coord.currentFrame)
	}
}

// TestDAPCoordinatorSelectFrameOutOfBounds tests frame selection with invalid index
func TestDAPCoordinatorSelectFrameOutOfBounds(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Line: 10},
	}
	coord.SetStackFrames(frames)

	cmd := coord.SelectFrame(100)
	if cmd != nil {
		t.Error("expected nil command for out of bounds frame")
	}
}

// TestDAPCoordinatorGetStackFrames tests getting stack frames
func TestDAPCoordinatorGetStackFrames(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main"},
	}
	coord.SetStackFrames(frames)

	retrieved := coord.GetStackFrames()
	if len(retrieved) != 1 {
		t.Errorf("expected 1 stack frame, got %d", len(retrieved))
	}
}

// TestDAPCoordinatorGetVariables tests getting variables
func TestDAPCoordinatorGetVariables(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	vars := []dap.Variable{
		{Name: "x", Value: "1"},
	}
	coord.SetVariables(vars)

	retrieved := coord.GetVariables()
	if len(retrieved) != 1 {
		t.Errorf("expected 1 variable, got %d", len(retrieved))
	}
}

// TestDAPCoordinatorAppendOutput tests appending output
func TestDAPCoordinatorAppendOutput(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	coord.AppendOutput("line 1")
	coord.AppendOutput("line 2")
	coord.AppendOutput("line 3")

	if len(coord.outputLog) != 3 {
		t.Errorf("expected 3 output lines, got %d", len(coord.outputLog))
	}
}

// TestDAPCoordinatorAppendOutputLimit tests output log limit
func TestDAPCoordinatorAppendOutputLimit(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	// Append 250 lines (limit is 200)
	for i := 0; i < 250; i++ {
		coord.AppendOutput(string(rune('a' + i%26)))
	}

	if len(coord.outputLog) > 200 {
		t.Errorf("expected max 200 output lines, got %d", len(coord.outputLog))
	}
}

// TestDAPCoordinatorClearOutput tests clearing output
func TestDAPCoordinatorClearOutput(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	coord.AppendOutput("line 1")
	coord.AppendOutput("line 2")
	coord.ClearOutput()

	if len(coord.outputLog) != 0 {
		t.Errorf("expected 0 output lines after clear, got %d", len(coord.outputLog))
	}
}

// TestDAPCoordinatorGetCurrentFrame tests getting current frame
func TestDAPCoordinatorGetCurrentFrame(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main"},
		{Id: 2, Name: "test"},
	}
	coord.SetStackFrames(frames)
	coord.currentFrame = 1

	frame := coord.GetCurrentFrame()
	if frame.Name != "test" {
		t.Errorf("expected frame 'test', got '%s'", frame.Name)
	}
}

// TestDAPCoordinatorGetCurrentFrameEmpty tests getting current frame when empty
func TestDAPCoordinatorGetCurrentFrameEmpty(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	frame := coord.GetCurrentFrame()
	if frame.Name != "" {
		t.Errorf("expected empty frame, got '%s'", frame.Name)
	}
}

// TestDAPCoordinatorIsRunning tests is running check
func TestDAPCoordinatorIsRunning(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	if coord.IsRunning() {
		t.Error("expected not running initially")
	}

	coord.state = dap.StateRunning
	if !coord.IsRunning() {
		t.Error("expected running")
	}

	coord.state = dap.StateInactive
	if coord.IsRunning() {
		t.Error("expected not running")
	}
}

// TestDAPCoordinatorIsPaused tests is paused check
func TestDAPCoordinatorIsPaused(t *testing.T) {
	coord := NewDAPCoordinator(nil)

	if coord.IsPaused() {
		t.Error("expected not paused initially")
	}

	coord.state = dap.StatePaused
	if !coord.IsPaused() {
		t.Error("expected paused")
	}
}
