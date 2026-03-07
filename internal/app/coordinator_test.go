package app

import (
	"testing"

	"teak/internal/acp"
	"teak/internal/dap"
	"teak/internal/lsp"
)

// TestCoordinatorCreation tests that the coordinator can be created
func TestCoordinatorCreation(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)
	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}

	if coord.lsp == nil {
		t.Error("expected LSP coordinator to be initialized")
	}

	if coord.dap == nil {
		t.Error("expected DAP coordinator to be initialized")
	}

	if coord.acp == nil {
		t.Error("expected ACP coordinator to be initialized")
	}
}

// TestCoordinatorHandleLSPMessage tests routing LSP messages
func TestCoordinatorHandleLSPMessage(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	msg := lsp.DiagnosticsMsg{
		URI:         "file:///test.go",
		Diagnostics: []lsp.Diagnostic{},
	}

	cmds := coord.HandleMessage(msg)
	// Diagnostics are stored, may return nil
	// Just verify it doesn't panic
	_ = cmds
}

// TestCoordinatorHandleDAPMessage tests routing DAP messages
func TestCoordinatorHandleDAPMessage(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	msg := dap.StoppedEventMsg{
		Reason: "breakpoint",
	}

	cmds := coord.HandleMessage(msg)
	// Should be handled by DAP coordinator
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestCoordinatorHandleACPMessage tests routing ACP messages
func TestCoordinatorHandleACPMessage(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	msg := acp.AgentTextMsg{
		Text: "test",
	}

	cmds := coord.HandleMessage(msg)
	// Should be handled by ACP coordinator
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestCoordinatorHandleJumpToFrame tests JumpToFrameMsg handling
func TestCoordinatorHandleJumpToFrame(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	msg := JumpToFrameMsg{
		FilePath: "/test.go",
		Line:     10,
	}

	cmds := coord.HandleMessage(msg)
	// JumpToFrameMsg is handled directly by coordinator
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestCoordinatorHandleBreakpointClick tests BreakpointClickMsg handling
func TestCoordinatorHandleBreakpointClick(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	msg := BreakpointClickMsg{
		Line: 10,
	}

	cmds := coord.HandleMessage(msg)
	// BreakpointClickMsg is handled directly by coordinator
	if cmds == nil {
		t.Error("expected commands to be returned")
	}
}

// TestCoordinatorGetLSPCoordinator tests getting LSP coordinator
func TestCoordinatorGetLSPCoordinator(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	lspCoord := coord.GetLSPCoordinator()
	if lspCoord == nil {
		t.Error("expected non-nil LSP coordinator")
	}
}

// TestCoordinatorGetDAPCoordinator tests getting DAP coordinator
func TestCoordinatorGetDAPCoordinator(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	dapCoord := coord.GetDAPCoordinator()
	if dapCoord == nil {
		t.Error("expected non-nil DAP coordinator")
	}
}

// TestCoordinatorGetACPCoordinator tests getting ACP coordinator
func TestCoordinatorGetACPCoordinator(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	acpCoord := coord.GetACPCoordinator()
	if acpCoord == nil {
		t.Error("expected non-nil ACP coordinator")
	}
}

// TestCoordinatorShutdown tests shutdown
func TestCoordinatorShutdown(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	// Should not panic
	coord.Shutdown()
}

// TestCoordinatorHandleUnknownMessage tests unknown message handling
func TestCoordinatorHandleUnknownMessage(t *testing.T) {
	coord := NewCoordinator(nil, nil, nil)

	// Unknown message type
	msg := struct{}{}

	cmds := coord.HandleMessage(msg)
	// Should return nil for unknown messages
	if cmds != nil {
		t.Error("expected nil commands for unknown message")
	}
}
