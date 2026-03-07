package app

import (
	"testing"

	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/lsp"
)

// TestLSPCoordinatorHandlesNilMessage tests that nil messages don't crash
func TestLSPCoordinatorHandlesNilMessage(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	
	// Should not panic
	cmds := coord.HandleMessage(nil)
	if cmds != nil {
		t.Error("Expected nil commands for nil message")
	}
}

// TestLSPCoordinatorHandlesEmptyDiagnostics tests empty diagnostic arrays
func TestLSPCoordinatorHandlesEmptyDiagnostics(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	
	cmds := coord.HandleMessage(lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{},
	})
	
	// Should store empty diagnostics without crashing
	diags := coord.GetDiagnostics("/test.go")
	if diags == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(diags) != 0 {
		t.Errorf("Expected 0 diagnostics, got %d", len(diags))
	}
	
	_ = cmds
}

// TestDAPCoordinatorHandlesNilMessage tests that nil messages don't crash
func TestDAPCoordinatorHandlesNilMessage(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	
	// Should not panic
	cmds := coord.HandleMessage(nil)
	if cmds != nil {
		t.Error("Expected nil commands for nil message")
	}
}

// TestDAPCoordinatorHandlesInvalidFrameSelection tests out-of-bounds frame selection
func TestDAPCoordinatorHandlesInvalidFrameSelection(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	
	// Set up some frames
	coord.SetStackFrames([]dap.StackFrame{
		{Id: 1, Name: "frame1"},
		{Id: 2, Name: "frame2"},
	})
	
	// Try to select invalid frames
	cmd := coord.SelectFrame(-1)
	if cmd != nil {
		t.Error("Expected nil command for negative index")
	}
	
	cmd = coord.SelectFrame(100)
	if cmd != nil {
		t.Error("Expected nil command for out-of-bounds index")
	}
}

// TestDAPCoordinatorHandlesEmptyStackFrames tests empty stack frame operations
func TestDAPCoordinatorHandlesEmptyStackFrames(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	
	// Should not crash on empty frames
	frame := coord.GetCurrentFrame()
	if frame.Name != "" {
		t.Errorf("Expected empty frame, got %q", frame.Name)
	}
	
	frames := coord.GetStackFrames()
	if len(frames) != 0 {
		t.Errorf("Expected 0 frames, got %d", len(frames))
	}
	
	count := coord.GetStackFrameCount()
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}
}

// TestACPCoordinatorHandlesNilMessage tests that nil messages don't crash
func TestACPCoordinatorHandlesNilMessage(t *testing.T) {
	coord := NewACPCoordinator(nil)
	
	// Should not panic
	cmds := coord.HandleMessage(nil)
	if cmds != nil {
		t.Error("Expected nil commands for nil message")
	}
}

// TestACPCoordinatorHandlesEmptyHistory tests empty history operations
func TestACPCoordinatorHandlesEmptyHistory(t *testing.T) {
	coord := NewACPCoordinator(nil)
	
	// Should not crash on empty history
	history := coord.GetHistory()
	if history == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(history) != 0 {
		t.Errorf("Expected 0 history entries, got %d", len(history))
	}
	
	// Clear should not crash
	coord.ClearHistory()
}

// TestACPCoordinatorHandlesEmptyStrings tests empty string inputs
func TestACPCoordinatorHandlesEmptyStrings(t *testing.T) {
	coord := NewACPCoordinator(nil)
	
	// Should not crash on empty strings
	coord.AddToHistory("", "")
	coord.SetSessionInfo("", "", "")
	
	// Verify state
	history := coord.GetHistory()
	if len(history) != 1 {
		t.Errorf("Expected 1 history entry, got %d", len(history))
	}
	
	sid, mid, mode := coord.GetSessionInfo()
	if sid != "" || mid != "" || mode != "" {
		t.Error("Expected empty session info")
	}
}

// TestCoordinatorHandlesNilManagers tests coordinator creation with nil managers
func TestCoordinatorHandlesNilManagers(t *testing.T) {
	// Should not panic with nil managers
	coord := NewCoordinator(nil, nil, nil)
	
	if coord == nil {
		t.Fatal("Expected non-nil coordinator")
	}
	
	// Should be able to call methods
	cmds := coord.HandleMessage(nil)
	if cmds != nil {
		t.Error("Expected nil commands")
	}
	
	// Should be able to shutdown
	coord.Shutdown()
}

// TestCoordinatorHandlesUnknownMessageType tests unknown message types
func TestCoordinatorHandlesUnknownMessageType(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	
	model, err := NewModel("", ".", cfg)
	if err != nil {
		t.Fatalf("NewModel failed: %v", err)
	}
	defer model.cleanup()
	
	// Send unknown message type
	unknownMsg := struct{}{}
	
	// Should not panic
	cmds := model.coordinator.HandleMessage(unknownMsg)
	if cmds != nil {
		t.Error("Expected nil commands for unknown message type")
	}
}

// TestLSPCoordinatorMemoryLimit tests that diagnostics are cleaned when limit exceeded
func TestLSPCoordinatorMemoryLimit(t *testing.T) {
	coord := NewLSPCoordinator(nil)
	
	// Send diagnostics for many files (exceeds limit of 100)
	for i := 0; i < 150; i++ {
		coord.HandleMessage(lsp.DiagnosticsMsg{
			URI:         "file:///test" + string(rune('a'+i%26)) + ".go",
			Diagnostics: []lsp.Diagnostic{{Severity: 1}},
		})
	}
	
	// Should have cleaned old entries
	allDiags := coord.AggregateDiagnostics()
	if len(allDiags) > 100 {
		t.Errorf("Expected max 100 diagnostics, got %d", len(allDiags))
	}
}

// TestACPCoordinatorMemoryLimit tests that chat history is cleaned when limit exceeded
func TestACPCoordinatorMemoryLimit(t *testing.T) {
	coord := NewACPCoordinator(nil)
	
	// Send many messages (exceeds limit of 500)
	for i := 0; i < 600; i++ {
		coord.AddToHistory("user", "message "+string(rune('a'+i%26)))
	}
	
	// Should have cleaned old entries
	history := coord.GetHistory()
	if len(history) > 500 {
		t.Errorf("Expected max 500 history entries, got %d", len(history))
	}
}

// TestDAPCoordinatorStateTransitions tests valid state transitions
func TestDAPCoordinatorStateTransitions(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	
	// Initial state
	if coord.GetState() != dap.StateInactive {
		t.Error("Initial state should be Inactive")
	}
	
	// Inactive → Paused
	coord.HandleMessage(dap.StoppedEventMsg{Reason: "breakpoint"})
	if coord.GetState() != dap.StatePaused {
		t.Error("State should be Paused after Stopped")
	}
	
	// Paused → Running
	coord.HandleMessage(dap.ContinuedEventMsg{})
	if coord.GetState() != dap.StateRunning {
		t.Error("State should be Running after Continued")
	}
	
	// Running → Inactive
	coord.HandleMessage(dap.TerminatedEventMsg{})
	if coord.GetState() != dap.StateInactive {
		t.Error("State should be Inactive after Terminated")
	}
}

// TestDAPCoordinatorOutputLimit tests that output log respects limit
func TestDAPCoordinatorOutputLimit(t *testing.T) {
	coord := NewDAPCoordinator(nil)
	
	// Send many output lines (exceeds limit of 200)
	for i := 0; i < 300; i++ {
		coord.AppendOutput("line " + string(rune('a'+i%26)))
	}
	
	// Should have cleaned old entries
	log := coord.GetOutputLog()
	if len(log) > 200 {
		t.Errorf("Expected max 200 output lines, got %d", len(log))
	}
}
