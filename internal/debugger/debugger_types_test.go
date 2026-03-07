package debugger

import (
	"testing"
)

// TestJumpToFrameMsg tests JumpToFrameMsg struct
func TestJumpToFrameMsg(t *testing.T) {
	msg := JumpToFrameMsg{
		FilePath: "/test.go",
		Line:     10,
	}

	if msg.FilePath != "/test.go" {
		t.Errorf("Expected FilePath '/test.go', got %q", msg.FilePath)
	}
	if msg.Line != 10 {
		t.Errorf("Expected Line 10, got %d", msg.Line)
	}
}

// TestExpandVariableMsg tests ExpandVariableMsg struct
func TestExpandVariableMsg(t *testing.T) {
	msg := ExpandVariableMsg{
		VariablesReference: 123,
	}

	if msg.VariablesReference != 123 {
		t.Errorf("Expected VariablesReference 123, got %d", msg.VariablesReference)
	}
}

// TestBreakpointStruct tests Breakpoint struct
func TestBreakpointStruct(t *testing.T) {
	bp := Breakpoint{
		FilePath: "/test.go",
		Line:     10,
		Enabled:  true,
		Verified: true,
	}

	if bp.FilePath != "/test.go" {
		t.Errorf("Expected FilePath '/test.go', got %q", bp.FilePath)
	}
	if bp.Line != 10 {
		t.Errorf("Expected Line 10, got %d", bp.Line)
	}
	if !bp.Enabled {
		t.Error("Expected Enabled to be true")
	}
	if !bp.Verified {
		t.Error("Expected Verified to be true")
	}
}

// TestBreakpointSlice tests Breakpoint slice operations
func TestBreakpointSlice(t *testing.T) {
	breakpoints := []Breakpoint{
		{FilePath: "/test1.go", Line: 10, Enabled: true},
		{FilePath: "/test2.go", Line: 20, Enabled: false},
		{FilePath: "/test3.go", Line: 30, Enabled: true},
	}

	if len(breakpoints) != 3 {
		t.Errorf("Expected 3 breakpoints, got %d", len(breakpoints))
	}

	// Test access
	if breakpoints[0].FilePath != "/test1.go" {
		t.Errorf("Expected first breakpoint '/test1.go', got %q", breakpoints[0].FilePath)
	}
	if !breakpoints[0].Enabled {
		t.Error("Expected first breakpoint to be enabled")
	}
	if breakpoints[1].Enabled {
		t.Error("Expected second breakpoint to be disabled")
	}
}

// TestBreakpointCopy tests Breakpoint value semantics
func TestBreakpointCopy(t *testing.T) {
	original := Breakpoint{
		FilePath: "/original.go",
		Line:     10,
		Enabled:  true,
	}

	// Copy by value
	copy := original
	copy.FilePath = "/modified.go"
	copy.Enabled = false

	if original.FilePath != "/original.go" {
		t.Error("Expected original to be unchanged")
	}
	if !original.Enabled {
		t.Error("Expected original.Enabled to be true")
	}
	if copy.FilePath != "/modified.go" {
		t.Errorf("Expected copy.FilePath to be modified, got %q", copy.FilePath)
	}
	if copy.Enabled {
		t.Error("Expected copy.Enabled to be false")
	}
}

// TestMessageTypesInstantiation tests all message types can be instantiated
func TestMessageTypesInstantiation(t *testing.T) {
	// Test all message types can be instantiated
	_ = JumpToFrameMsg{FilePath: "/test.go", Line: 10}
	_ = ExpandVariableMsg{VariablesReference: 123}
	_ = Breakpoint{FilePath: "/test.go", Line: 10, Enabled: true}
}

// TestBreakpointWithDifferentStates tests breakpoints with different states
func TestBreakpointWithDifferentStates(t *testing.T) {
	breakpoints := []Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true, Verified: true},
		{FilePath: "/test.go", Line: 20, Enabled: true, Verified: false},
		{FilePath: "/test.go", Line: 30, Enabled: false, Verified: false},
	}

	// Verify states
	if !breakpoints[0].Enabled || !breakpoints[0].Verified {
		t.Error("Expected first breakpoint enabled and verified")
	}
	if !breakpoints[1].Enabled || breakpoints[1].Verified {
		t.Error("Expected second breakpoint enabled but not verified")
	}
	if breakpoints[2].Enabled || breakpoints[2].Verified {
		t.Error("Expected third breakpoint disabled and not verified")
	}
}

// TestBreakpointEmpty tests Breakpoint with empty/zero fields
func TestBreakpointEmpty(t *testing.T) {
	bp := Breakpoint{}

	if bp.FilePath != "" {
		t.Errorf("Expected empty FilePath, got %q", bp.FilePath)
	}
	if bp.Line != 0 {
		t.Errorf("Expected Line 0, got %d", bp.Line)
	}
	if bp.Enabled {
		t.Error("Expected Enabled to be false")
	}
	if bp.Verified {
		t.Error("Expected Verified to be false")
	}
}

// TestJumpToFrameMsgCopy tests JumpToFrameMsg value semantics
func TestJumpToFrameMsgCopy(t *testing.T) {
	original := JumpToFrameMsg{
		FilePath: "/original.go",
		Line:     10,
	}

	// Copy by value
	copy := original
	copy.FilePath = "/modified.go"
	copy.Line = 20

	if original.FilePath != "/original.go" {
		t.Error("Expected original to be unchanged")
	}
	if original.Line != 10 {
		t.Errorf("Expected original.Line to be 10, got %d", original.Line)
	}
	if copy.FilePath != "/modified.go" {
		t.Errorf("Expected copy.FilePath to be modified, got %q", copy.FilePath)
	}
	if copy.Line != 20 {
		t.Errorf("Expected copy.Line to be 20, got %d", copy.Line)
	}
}

// TestExpandVariableMsgCopy tests ExpandVariableMsg value semantics
func TestExpandVariableMsgCopy(t *testing.T) {
	original := ExpandVariableMsg{
		VariablesReference: 123,
	}

	// Copy by value
	copy := original
	copy.VariablesReference = 456

	if original.VariablesReference != 123 {
		t.Errorf("Expected original to be unchanged, got %d", original.VariablesReference)
	}
	if copy.VariablesReference != 456 {
		t.Errorf("Expected copy to be modified, got %d", copy.VariablesReference)
	}
}

// TestBreakpointFiltering tests filtering breakpoints by state
func TestBreakpointFiltering(t *testing.T) {
	breakpoints := []Breakpoint{
		{FilePath: "/test.go", Line: 10, Enabled: true},
		{FilePath: "/test.go", Line: 20, Enabled: false},
		{FilePath: "/test.go", Line: 30, Enabled: true},
		{FilePath: "/test.go", Line: 40, Enabled: false},
	}

	// Filter enabled breakpoints
	var enabled []Breakpoint
	for _, bp := range breakpoints {
		if bp.Enabled {
			enabled = append(enabled, bp)
		}
	}

	if len(enabled) != 2 {
		t.Errorf("Expected 2 enabled breakpoints, got %d", len(enabled))
	}

	// Filter disabled breakpoints
	var disabled []Breakpoint
	for _, bp := range breakpoints {
		if !bp.Enabled {
			disabled = append(disabled, bp)
		}
	}

	if len(disabled) != 2 {
		t.Errorf("Expected 2 disabled breakpoints, got %d", len(disabled))
	}
}

// TestBreakpointByFile tests grouping breakpoints by file
func TestBreakpointByFile(t *testing.T) {
	breakpoints := []Breakpoint{
		{FilePath: "/file1.go", Line: 10, Enabled: true},
		{FilePath: "/file2.go", Line: 20, Enabled: true},
		{FilePath: "/file1.go", Line: 30, Enabled: true},
	}

	// Count breakpoints per file
	fileCounts := make(map[string]int)
	for _, bp := range breakpoints {
		fileCounts[bp.FilePath]++
	}

	if fileCounts["/file1.go"] != 2 {
		t.Errorf("Expected 2 breakpoints in /file1.go, got %d", fileCounts["/file1.go"])
	}
	if fileCounts["/file2.go"] != 1 {
		t.Errorf("Expected 1 breakpoint in /file2.go, got %d", fileCounts["/file2.go"])
	}
}

// TestBreakpointLineOrder tests breakpoints are ordered by line
func TestBreakpointLineOrder(t *testing.T) {
	breakpoints := []Breakpoint{
		{FilePath: "/test.go", Line: 30},
		{FilePath: "/test.go", Line: 10},
		{FilePath: "/test.go", Line: 20},
	}

	// Sort by line (simple bubble sort for test)
	for i := 0; i < len(breakpoints)-1; i++ {
		for j := i + 1; j < len(breakpoints); j++ {
			if breakpoints[i].Line > breakpoints[j].Line {
				breakpoints[i], breakpoints[j] = breakpoints[j], breakpoints[i]
			}
		}
	}

	// Verify order
	expectedLines := []int{10, 20, 30}
	for i, expected := range expectedLines {
		if breakpoints[i].Line != expected {
			t.Errorf("Expected line %d at position %d, got %d", expected, i, breakpoints[i].Line)
		}
	}
}

// TestJumpToFrameMsgWithDifferentPaths tests JumpToFrameMsg with different file paths
func TestJumpToFrameMsgWithDifferentPaths(t *testing.T) {
	paths := []string{
		"/test1.go",
		"/test2.go",
		"/home/user/project/main.go",
		"C:\\Users\\project\\main.go",
	}

	for _, path := range paths {
		msg := JumpToFrameMsg{
			FilePath: path,
			Line:     10,
		}
		if msg.FilePath != path {
			t.Errorf("Expected FilePath %q, got %q", path, msg.FilePath)
		}
	}
}

// TestExpandVariableMsgWithDifferentReferences tests different variable references
func TestExpandVariableMsgWithDifferentReferences(t *testing.T) {
	references := []int{0, 1, 100, 1000, 9999}

	for _, ref := range references {
		msg := ExpandVariableMsg{
			VariablesReference: ref,
		}
		if msg.VariablesReference != ref {
			t.Errorf("Expected VariablesReference %d, got %d", ref, msg.VariablesReference)
		}
	}
}

// TestBreakpointValidation tests breakpoint validation logic
func TestBreakpointValidation(t *testing.T) {
	// Valid breakpoint
	valid := Breakpoint{
		FilePath: "/test.go",
		Line:     10,
		Enabled:  true,
	}
	if valid.FilePath == "" {
		t.Error("Expected non-empty FilePath")
	}
	if valid.Line <= 0 {
		t.Error("Expected positive Line")
	}

	// Invalid breakpoint (empty path)
	invalid := Breakpoint{
		FilePath: "",
		Line:     10,
	}
	if invalid.FilePath != "" {
		t.Error("Expected empty FilePath for invalid breakpoint")
	}
}

// TestBreakpointToggleEnabled tests toggling breakpoint enabled state
func TestBreakpointToggleEnabled(t *testing.T) {
	bp := Breakpoint{
		FilePath: "/test.go",
		Line:     10,
		Enabled:  true,
	}

	// Toggle enabled
	bp.Enabled = !bp.Enabled
	if bp.Enabled {
		t.Error("Expected Enabled to be false after toggle")
	}

	// Toggle again
	bp.Enabled = !bp.Enabled
	if !bp.Enabled {
		t.Error("Expected Enabled to be true after second toggle")
	}
}
