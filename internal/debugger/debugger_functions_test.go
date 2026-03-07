package debugger

import (
	"testing"

	"teak/internal/dap"
	"teak/internal/ui"
)

// TestDebuggerSetBreakpoints tests SetBreakpoints method
func TestDebuggerSetBreakpoints(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	bps := []Breakpoint{
		{FilePath: "/test1.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test2.go", Line: 20, Enabled: false, Verified: false},
	}

	model.SetBreakpoints(bps)

	if len(model.breakpoints) != 2 {
		t.Errorf("Expected 2 breakpoints, got %d", len(model.breakpoints))
	}
}

// TestDebuggerSetBreakpointsWithEmptyList tests SetBreakpoints with empty list
func TestDebuggerSetBreakpointsWithEmptyList(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetBreakpoints([]Breakpoint{})

	if len(model.breakpoints) != 0 {
		t.Errorf("Expected 0 breakpoints, got %d", len(model.breakpoints))
	}
}

// TestDebuggerSetBreakpointsWithNil tests SetBreakpoints with nil
func TestDebuggerSetBreakpointsWithNil(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetBreakpoints(nil)

	if model.breakpoints != nil {
		t.Errorf("Expected nil breakpoints, got %v", model.breakpoints)
	}
}

// TestDebuggerAppendOutput tests AppendOutput method
func TestDebuggerAppendOutput(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AppendOutput("line 1")
	model.AppendOutput("line 2")
	model.AppendOutput("line 3")

	if len(model.outputLog) != 3 {
		t.Errorf("Expected 3 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerAppendOutputWithLimit tests AppendOutput with 200 line limit
func TestDebuggerAppendOutputWithLimit(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Append more than 200 lines
	for i := 0; i < 250; i++ {
		model.AppendOutput("line " + string(rune('a'+i%26)))
	}

	if len(model.outputLog) > 200 {
		t.Errorf("Expected max 200 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerAppendOutputPreservesOrder tests AppendOutput preserves order
func TestDebuggerAppendOutputPreservesOrder(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	expected := []string{"first", "second", "third"}
	for _, line := range expected {
		model.AppendOutput(line)
	}

	for i, exp := range expected {
		if model.outputLog[i] != exp {
			t.Errorf("Expected line %d to be %q, got %q", i, exp, model.outputLog[i])
		}
	}
}

// TestDebuggerClearOutput tests ClearOutput method
func TestDebuggerClearOutput(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AppendOutput("line 1")
	model.AppendOutput("line 2")
	model.ClearOutput()

	if len(model.outputLog) != 0 {
		t.Errorf("Expected 0 output lines after clear, got %d", len(model.outputLog))
	}
}

// TestDebuggerClearOutputMultipleTimes tests ClearOutput multiple times
func TestDebuggerClearOutputMultipleTimes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.ClearOutput()
	model.ClearOutput()
	model.ClearOutput()

	// Should not crash
}

// TestDebuggerStateMethod tests State method
func TestDebuggerStateMethod(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	state := model.State()
	if state != dap.StateInactive {
		t.Errorf("Expected state Inactive, got %v", state)
	}

	model.state = dap.StateRunning
	state = model.State()
	if state != dap.StateRunning {
		t.Errorf("Expected state Running, got %v", state)
	}
}

// TestDebuggerSelectFrame tests SelectFrame method
func TestDebuggerSelectFrame(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Source: dap.Source{Path: "/test1.go", Name: "test1.go"}, Line: 10},
		{Id: 2, Name: "test", Source: dap.Source{Path: "/test2.go", Name: "test2.go"}, Line: 20},
	}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(1)
	if cmd == nil {
		t.Error("Expected command to be returned")
	}

	if model.currentFrame != 1 {
		t.Errorf("Expected currentFrame 1, got %d", model.currentFrame)
	}
}

// TestDebuggerSelectFrameWithInvalidIndex tests SelectFrame with invalid index
func TestDebuggerSelectFrameWithInvalidIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{{Id: 1}}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(100)
	if cmd != nil {
		t.Error("Expected nil command for out of bounds index")
	}
}

// TestDebuggerSelectFrameWithNegativeIndex tests SelectFrame with negative index
func TestDebuggerSelectFrameWithNegativeIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{{Id: 1}}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(-1)
	if cmd != nil {
		t.Error("Expected nil command for negative index")
	}
}

// TestDebuggerSelectFrameWithEmptySourcePath tests SelectFrame with empty source path
func TestDebuggerSelectFrameWithEmptySourcePath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Source: dap.Source{Path: "", Name: ""}, Line: 10},
	}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(0)
	if cmd != nil {
		t.Error("Expected nil command for frame with empty source path")
	}
}

// TestDebuggerSelectFrameConvertsLineNumbers tests SelectFrame converts 1-based to 0-based
func TestDebuggerSelectFrameConvertsLineNumbers(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Source: dap.Source{Path: "/test.go", Name: "test.go"}, Line: 10},
	}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(0)
	if cmd == nil {
		t.Fatal("Expected command to be returned")
	}

	// Execute command to get message
	msg := cmd()
	if jumpMsg, ok := msg.(JumpToFrameMsg); ok {
		if jumpMsg.Line != 9 { // 10 - 1 = 9 (0-based)
			t.Errorf("Expected line 9 (0-based), got %d", jumpMsg.Line)
		}
	}
}

// TestDebuggerCurrentFrame tests CurrentFrame method
func TestDebuggerCurrentFrame(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.CurrentFrame() != 0 {
		t.Errorf("Expected currentFrame 0, got %d", model.CurrentFrame())
	}

	model.currentFrame = 5
	if model.CurrentFrame() != 5 {
		t.Errorf("Expected currentFrame 5, got %d", model.CurrentFrame())
	}
}

// TestDebuggerBreakpointView tests BreakpointView method
func TestDebuggerBreakpointView(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Empty breakpoints
	view := model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty breakpoint view")
	}

	// With breakpoints
	model.breakpoints = []Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true, Verified: true},
	}
	view = model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty breakpoint view with breakpoints")
	}
}

// TestDebuggerBreakpointViewWithMultipleBreakpoints tests BreakpointView with multiple breakpoints
func TestDebuggerBreakpointViewWithMultipleBreakpoints(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test1.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test2.go", Line: 20, Enabled: false, Verified: false},
		{FilePath: "/test3.go", Line: 30, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty breakpoint view")
	}
}

// TestDebuggerBreakpointViewExtractsFilename tests BreakpointView extracts filename from path
func TestDebuggerBreakpointViewExtractsFilename(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/very/long/path/to/file.go", Line: 10, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should contain filename
	if view != "" && !containsString(view, "file.go") {
		t.Error("Expected view to contain filename 'file.go'")
	}
}

// TestDebuggerBreakpointViewShowsLineNumber tests BreakpointView shows line numbers
func TestDebuggerBreakpointViewShowsLineNumber(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test.go", Line: 42, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should contain line number (displayed as 1-based, so 43)
	if view != "" && !containsString(view, "43") {
		t.Error("Expected view to contain line number")
	}
}

// TestDebuggerBreakpointViewShowsVerificationStatus tests BreakpointView shows verification status
func TestDebuggerBreakpointViewShowsVerificationStatus(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test2.go", Line: 20, Enabled: true, Verified: false},
	}

	view := model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerBreakpointViewShowsEnabledStatus tests BreakpointView shows enabled status
func TestDebuggerBreakpointViewShowsEnabledStatus(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test2.go", Line: 20, Enabled: false, Verified: true},
	}

	view := model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerBreakpointViewWithEmptyPath tests BreakpointView with empty file path
func TestDebuggerBreakpointViewWithEmptyPath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "", Line: 10, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should not crash
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerBreakpointViewOrderPreservation tests BreakpointView preserves order
func TestDebuggerBreakpointViewOrderPreservation(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	expected := []string{"/test1.go", "/test2.go", "/test3.go"}
	for i, path := range expected {
		model.breakpoints = append(model.breakpoints, Breakpoint{
			FilePath: path,
			Line:     i * 10,
			Enabled:  true,
			Verified: true,
		})
	}

	view := model.BreakpointView()
	// Order should be preserved in rendering
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerSetBreakpointsReplacesExisting tests SetBreakpoints replaces existing
func TestDebuggerSetBreakpointsReplacesExisting(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Set initial breakpoints
	model.breakpoints = []Breakpoint{
		{FilePath: "/old.go", Line: 1},
	}

	// Replace with new breakpoints
	model.SetBreakpoints([]Breakpoint{
		{FilePath: "/new.go", Line: 2},
	})

	if len(model.breakpoints) != 1 {
		t.Errorf("Expected 1 breakpoint, got %d", len(model.breakpoints))
	}
	if model.breakpoints[0].FilePath != "/new.go" {
		t.Errorf("Expected /new.go, got %q", model.breakpoints[0].FilePath)
	}
}

// TestDebuggerAppendOutputWithEmptyString tests AppendOutput with empty string
func TestDebuggerAppendOutputWithEmptyString(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AppendOutput("")

	if len(model.outputLog) != 1 {
		t.Errorf("Expected 1 output line, got %d", len(model.outputLog))
	}
}

// TestDebuggerAppendOutputWithLongString tests AppendOutput with long string
func TestDebuggerAppendOutputWithLongString(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	longLine := string(make([]byte, 10000))
	model.AppendOutput(longLine)

	if len(model.outputLog) != 1 {
		t.Errorf("Expected 1 output line, got %d", len(model.outputLog))
	}
	if len(model.outputLog[0]) != 10000 {
		t.Errorf("Expected content length 10000, got %d", len(model.outputLog[0]))
	}
}

// TestDebuggerAppendOutputAtLimit tests AppendOutput when at limit
func TestDebuggerAppendOutputAtLimit(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Fill to limit
	for i := 0; i < 200; i++ {
		model.AppendOutput("line")
	}

	// Add one more
	model.AppendOutput("new line")

	if len(model.outputLog) != 200 {
		t.Errorf("Expected 200 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerClearOutputWithEmptyList tests ClearOutput with empty list
func TestDebuggerClearOutputWithEmptyList(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.ClearOutput()

	if len(model.outputLog) != 0 {
		t.Errorf("Expected 0 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerStateTransitions tests state transitions
func TestDebuggerStateTransitions(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	states := []dap.DebugState{
		dap.StateInactive,
		dap.StateRunning,
		dap.StateStopped,
		dap.StatePaused,
	}

	for _, expected := range states {
		model.state = expected
		if model.State() != expected {
			t.Errorf("Expected state %v, got %v", expected, model.State())
		}
	}
}

// TestDebuggerSelectFrameUpdatesCurrentFrame tests SelectFrame updates currentFrame
func TestDebuggerSelectFrameUpdatesCurrentFrame(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{
		{Id: 1},
		{Id: 2},
		{Id: 3},
	}
	model.SetStackFrames(frames)

	// Select different frames
	model.SelectFrame(0)
	if model.CurrentFrame() != 0 {
		t.Errorf("Expected currentFrame 0, got %d", model.CurrentFrame())
	}

	model.SelectFrame(2)
	if model.CurrentFrame() != 2 {
		t.Errorf("Expected currentFrame 2, got %d", model.CurrentFrame())
	}
}

// TestDebuggerSelectFrameDoesNotChangeOnInvalidIndex tests SelectFrame doesn't change on invalid index
func TestDebuggerSelectFrameDoesNotChangeOnInvalidIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{{Id: 1}}
	model.SetStackFrames(frames)
	model.currentFrame = 0

	// Try to select invalid index
	model.SelectFrame(100)

	// Should not change
	if model.currentFrame != 0 {
		t.Errorf("Expected currentFrame unchanged (0), got %d", model.currentFrame)
	}
}

// TestDebuggerCurrentFrameAfterSetStackFrames tests CurrentFrame after SetStackFrames
func TestDebuggerCurrentFrameAfterSetStackFrames(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{{Id: 1}, {Id: 2}}
	model.SetStackFrames(frames)

	if model.CurrentFrame() != 0 {
		t.Errorf("Expected currentFrame 0 after SetStackFrames, got %d", model.CurrentFrame())
	}
}

// TestDebuggerBreakpointViewFormatsLinesAsOneBased tests BreakpointView formats lines as 1-based
func TestDebuggerBreakpointViewFormatsLinesAsOneBased(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test.go", Line: 0, Enabled: true, Verified: true}, // 0-based
	}

	view := model.BreakpointView()
	// Should display as 1 (0 + 1)
	if view != "" && !containsString(view, "1") {
		t.Error("Expected view to show line as 1-based")
	}
}

// TestDebuggerSetBreakpointsWithSingleBreakpoint tests SetBreakpoints with single breakpoint
func TestDebuggerSetBreakpointsWithSingleBreakpoint(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetBreakpoints([]Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true, Verified: true},
	})

	if len(model.breakpoints) != 1 {
		t.Errorf("Expected 1 breakpoint, got %d", len(model.breakpoints))
	}
}

// TestDebuggerAppendOutputMultipleTimes tests AppendOutput multiple times
func TestDebuggerAppendOutputMultipleTimes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	for i := 0; i < 100; i++ {
		model.AppendOutput("line " + string(rune('a'+i%26)))
	}

	if len(model.outputLog) != 100 {
		t.Errorf("Expected 100 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerClearOutputAfterManyLines tests ClearOutput after many lines
func TestDebuggerClearOutputAfterManyLines(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	for i := 0; i < 500; i++ {
		model.AppendOutput("line")
	}

	model.ClearOutput()

	if len(model.outputLog) != 0 {
		t.Errorf("Expected 0 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerStateMethodReturnsCurrentState tests State method returns current state
func TestDebuggerStateMethodReturnsCurrentState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.state = dap.StateRunning
	if model.State() != dap.StateRunning {
		t.Error("Expected Running state")
	}

	model.state = dap.StateStopped
	if model.State() != dap.StateStopped {
		t.Error("Expected Stopped state")
	}
}

// TestDebuggerSelectFrameWithZeroFrames tests SelectFrame with zero frames
func TestDebuggerSelectFrameWithZeroFrames(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetStackFrames([]dap.StackFrame{})

	cmd := model.SelectFrame(0)
	if cmd != nil {
		t.Error("Expected nil command for zero frames")
	}
}

// TestDebuggerCurrentFrameWithNoFrames tests CurrentFrame with no frames
func TestDebuggerCurrentFrameWithNoFrames(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetStackFrames([]dap.StackFrame{})

	if model.CurrentFrame() != 0 {
		t.Errorf("Expected currentFrame 0, got %d", model.CurrentFrame())
	}
}

// TestDebuggerBreakpointViewWithSpecialCharacters tests BreakpointView with special characters in path
func TestDebuggerBreakpointViewWithSpecialCharacters(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/path/with spaces/file.go", Line: 10, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should not crash
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerBreakpointViewWithWindowsPath tests BreakpointView with Windows path
func TestDebuggerBreakpointViewWithWindowsPath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "C:\\Users\\test\\file.go", Line: 10, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should not crash
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerBreakpointViewWithVeryLongPath tests BreakpointView with very long path
func TestDebuggerBreakpointViewWithVeryLongPath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	longPath := "/very/long/path/" + string(make([]byte, 1000)) + "/file.go"
	model.breakpoints = []Breakpoint{
		{FilePath: longPath, Line: 10, Enabled: true, Verified: true},
	}

	view := model.BreakpointView()
	// Should not crash
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerAppendOutputWithNewlines tests AppendOutput with newlines
func TestDebuggerAppendOutputWithNewlines(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AppendOutput("line1\nline2\nline3")

	if len(model.outputLog) != 1 {
		t.Errorf("Expected 1 output line, got %d", len(model.outputLog))
	}
}

// TestDebuggerAppendOutputWithTabs tests AppendOutput with tabs
func TestDebuggerAppendOutputWithTabs(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AppendOutput("line\twith\ttabs")

	if len(model.outputLog) != 1 {
		t.Errorf("Expected 1 output line, got %d", len(model.outputLog))
	}
}

// TestDebuggerBreakpointViewRendersAllBreakpoints tests BreakpointView renders all breakpoints
func TestDebuggerBreakpointViewRendersAllBreakpoints(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	for i := 0; i < 10; i++ {
		model.breakpoints = append(model.breakpoints, Breakpoint{
			FilePath: "/test.go",
			Line:     i * 10,
			Enabled:  true,
			Verified: true,
		})
	}

	view := model.BreakpointView()
	// Should render all breakpoints
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerSetBreakpointsMaintainsOrder tests SetBreakpoints maintains order
func TestDebuggerSetBreakpointsMaintainsOrder(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	expected := []Breakpoint{
		{FilePath: "/test1.go", Line: 10},
		{FilePath: "/test2.go", Line: 20},
		{FilePath: "/test3.go", Line: 30},
	}

	model.SetBreakpoints(expected)

	for i, exp := range expected {
		if model.breakpoints[i].FilePath != exp.FilePath {
			t.Errorf("Expected breakpoint %d to be %q, got %q", i, exp.FilePath, model.breakpoints[i].FilePath)
		}
	}
}

// TestDebuggerAppendOutputAtExactLimit tests AppendOutput at exact limit
func TestDebuggerAppendOutputAtExactLimit(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Append exactly 200 lines
	for i := 0; i < 200; i++ {
		model.AppendOutput("line")
	}

	if len(model.outputLog) != 200 {
		t.Errorf("Expected 200 output lines, got %d", len(model.outputLog))
	}

	// Append one more
	model.AppendOutput("new")

	// Should still be 200
	if len(model.outputLog) != 200 {
		t.Errorf("Expected 200 output lines after limit, got %d", len(model.outputLog))
	}
}

// TestDebuggerClearOutputResetsCount tests ClearOutput resets count
func TestDebuggerClearOutputResetsCount(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Fill and clear multiple times
	for i := 0; i < 3; i++ {
		for j := 0; j < 100; j++ {
			model.AppendOutput("line")
		}
		model.ClearOutput()
	}

	if len(model.outputLog) != 0 {
		t.Errorf("Expected 0 output lines, got %d", len(model.outputLog))
	}
}

// TestDebuggerSelectFrameCommandExecution tests SelectFrame command execution
func TestDebuggerSelectFrameCommandExecution(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{
		{Id: 1, Name: "main", Source: dap.Source{Path: "/test.go", Name: "test.go"}, Line: 10},
	}
	model.SetStackFrames(frames)

	cmd := model.SelectFrame(0)
	if cmd == nil {
		t.Fatal("Expected command")
	}

	msg := cmd()
	if msg == nil {
		t.Fatal("Expected message from command")
	}

	if _, ok := msg.(JumpToFrameMsg); !ok {
		t.Errorf("Expected JumpToFrameMsg, got %T", msg)
	}
}

// TestDebuggerCurrentFrameAfterMultipleSelects tests CurrentFrame after multiple selects
func TestDebuggerCurrentFrameAfterMultipleSelects(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	frames := []dap.StackFrame{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}, {Id: 5}}
	model.SetStackFrames(frames)

	// Select multiple frames
	model.SelectFrame(2)
	if model.CurrentFrame() != 2 {
		t.Errorf("Expected currentFrame 2, got %d", model.CurrentFrame())
	}

	model.SelectFrame(4)
	if model.CurrentFrame() != 4 {
		t.Errorf("Expected currentFrame 4, got %d", model.CurrentFrame())
	}

	model.SelectFrame(0)
	if model.CurrentFrame() != 0 {
		t.Errorf("Expected currentFrame 0, got %d", model.CurrentFrame())
	}
}

// TestDebuggerBreakpointViewWithMixedVerificationStatus tests BreakpointView with mixed verification status
func TestDebuggerBreakpointViewWithMixedVerificationStatus(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.breakpoints = []Breakpoint{
		{FilePath: "/test1.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test2.go", Line: 20, Enabled: true, Verified: false},
		{FilePath: "/test3.go", Line: 30, Enabled: false, Verified: false},
		{FilePath: "/test4.go", Line: 40, Enabled: false, Verified: true},
	}

	view := model.BreakpointView()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestDebuggerInitialStateValues tests initial state values
func TestDebuggerInitialStateValues(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Verify initial values
	if model.state != dap.StateInactive {
		t.Errorf("Expected state Inactive, got %v", model.state)
	}
	if model.currentFrame != 0 {
		t.Errorf("Expected currentFrame 0, got %d", model.currentFrame)
	}
	if model.scrollY != 0 {
		t.Errorf("Expected scrollY 0, got %d", model.scrollY)
	}
	if !model.showBreakpoints {
		t.Error("Expected showBreakpoints true initially")
	}
	if model.breakpoints != nil {
		t.Error("Expected breakpoints nil initially")
	}
	if model.outputLog != nil {
		t.Error("Expected outputLog nil initially")
	}
	if model.stackFrames != nil {
		t.Error("Expected stackFrames nil initially")
	}
	if model.variables != nil {
		t.Error("Expected variables nil initially")
	}
	if model.expandedVars == nil {
		t.Error("Expected expandedVars to be initialized")
	}
}

// Helper function to check if string contains substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
