package app

import (
	"context"
	"log"
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/acp"
	"teak/internal/dap"
	"teak/internal/lsp"
)

// LSPCoordinatorInterface defines the interface for LSP coordinator.
type LSPCoordinatorInterface interface {
	HandleMessage(msg tea.Msg) []tea.Cmd
	GetDiagnostics(path string) []lsp.Diagnostic
	StorePreparedDiagnostics(path string, diagnostics []lsp.Diagnostic)
	SetTriggerChars(path string, chars []string)
	GetTriggerChars(path string) []string
	ClearDiagnostics(path string)
	RelocateFilePath(oldPath, newPath string)
	AggregateDiagnostics() []lsp.Diagnostic
	Shutdown()
}

// DAPCoordinatorInterface defines the interface for DAP coordinator.
type DAPCoordinatorInterface interface {
	HandleMessage(msg tea.Msg) []tea.Cmd
	SetStackFrames(frames []dap.StackFrame)
	SetVariables(vars []dap.Variable)
	SetState(state dap.DebugState)
	GetState() dap.DebugState
	IsRunning() bool
	IsPaused() bool
	SelectFrame(idx int) tea.Cmd
	GetCurrentFrame() dap.StackFrame
	GetStackFrames() []dap.StackFrame
	GetVariables() []dap.Variable
	AppendOutput(line string)
	ClearOutput()
	GetOutputLog() []string
	GetCurrentFrameIndex() int
	GetStackFrameCount() int
	Shutdown()
}

// ACPCoordinatorInterface defines the interface for ACP coordinator.
type ACPCoordinatorInterface interface {
	HandleMessage(msg tea.Msg) []tea.Cmd
	AddToHistory(role, content string)
	ClearHistory()
	GetHistory() []ChatMessage
	SetSessionInfo(sessionID, modelID, mode string)
	GetSessionInfo() (string, string, string)
	IsRunning() bool
	SetRunning(running bool)
	GetManager() *acp.Manager
	Shutdown()
}

// Coordinator orchestrates all subsystem coordinators.
type Coordinator struct {
	lsp LSPCoordinatorInterface
	dap DAPCoordinatorInterface
	acp ACPCoordinatorInterface
}

// NewCoordinator creates a new main coordinator.
func NewCoordinator(lspMgr *lsp.Manager, dapMgr *dap.Manager, acpMgr *acp.Manager) *Coordinator {
	return &Coordinator{
		lsp: NewLSPCoordinator(lspMgr),
		dap: NewDAPCoordinator(dapMgr),
		acp: NewACPCoordinator(acpMgr),
	}
}

// HandleMessage routes messages to appropriate coordinators with panic recovery.
func (c *Coordinator) HandleMessage(msg tea.Msg) []tea.Cmd {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Coordinator panic recovered: %v", r)
			// Could return error message to UI here
		}
	}()

	switch m := msg.(type) {
	// LSP messages
	case lspMsg, lsp.DiagnosticsMsg, lsp.CompletionResultMsg, lsp.HoverResultMsg,
		lsp.DefinitionResultMsg, lsp.ReferencesResultMsg, lsp.FormatResultMsg,
		lsp.CodeActionResultMsg, lsp.DocumentSymbolResultMsg, lsp.RenameResultMsg,
		lsp.FoldingRangeResultMsg, lsp.SignatureHelpResultMsg, lsp.LspErrorMsg,
		lsp.LspProgressMsg, lsp.LspShowMessageMsg, LspReadyMsg:
		return c.lsp.HandleMessage(msg)

	// DAP messages
	case dapMsg:
		inner, ok := m.msg.(tea.Msg)
		if !ok || inner == nil {
			return nil
		}
		// DAP traffic is wrapped while it crosses the async manager boundary.
		// Update still needs the wrapper for the app-level debugger handling, but
		// the coordinator must observe the inner event to keep its state current.
		c.dap.HandleMessage(inner)
		return nil
	case dap.StoppedEventMsg, dap.ContinuedEventMsg, dap.TerminatedEventMsg,
		dap.ExitedEventMsg, dap.OutputEventMsg, dap.BreakpointEventMsg:
		return c.dap.HandleMessage(msg)

	// ACP messages
	case acpMsg, acp.AgentTextMsg, acp.AgentThoughtMsg, acp.AgentToolCallMsg,
		acp.AgentToolCallUpdateMsg, acp.AgentPlanMsg, acp.AgentWriteFileMsg,
		acp.AgentWriteCancelledMsg, acp.AgentPermissionRequestMsg, acp.AgentPromptResponseMsg,
		acp.AgentSessionInfoMsg, acp.AgentModelChangedMsg, acp.AgentModeChangedMsg,
		acp.AgentErrorMsg, acp.AgentStartedMsg, acp.AgentStoppedMsg,
		acp.FileReadRequestMsg:
		return c.acp.HandleMessage(msg)

	// Direct messages (handled by coordinator itself)
	case JumpToFrameMsg:
		return c.handleJumpToFrame(m)
	case BreakpointClickMsg:
		return c.handleBreakpointClick(m)

	default:
		return nil
	}
}

// handleJumpToFrame handles jumping to a stack frame.
func (c *Coordinator) handleJumpToFrame(msg JumpToFrameMsg) []tea.Cmd {
	// This message is for the app layer to handle
	// Return it so app.go can process it
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleBreakpointClick handles clicking on a breakpoint gutter.
func (c *Coordinator) handleBreakpointClick(msg BreakpointClickMsg) []tea.Cmd {
	// This message is for the app layer to handle
	// Return it so app.go can process it
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// GetLSPCoordinator returns the LSP coordinator.
func (c *Coordinator) GetLSPCoordinator() LSPCoordinatorInterface {
	return c.lsp
}

// GetDAPCoordinator returns the DAP coordinator.
func (c *Coordinator) GetDAPCoordinator() DAPCoordinatorInterface {
	return c.dap
}

// GetACPCoordinator returns the ACP coordinator.
func (c *Coordinator) GetACPCoordinator() ACPCoordinatorInterface {
	return c.acp
}

// Shutdown shuts down all coordinators.
func (c *Coordinator) Shutdown() {
	if c.lsp != nil {
		c.lsp.Shutdown()
	}
	if c.dap != nil {
		c.dap.Shutdown()
	}
	if c.acp != nil {
		c.acp.Shutdown()
	}
}

type coordinatorShutdownWaiter interface {
	WaitForShutdown(context.Context) bool
}

// ShutdownAndWait initiates all subsystem teardowns and waits in parallel for
// their child processes to be reaped. The caller supplies one global bound.
func (c *Coordinator) ShutdownAndWait(ctx context.Context) bool {
	c.Shutdown()

	waiters := make([]coordinatorShutdownWaiter, 0, 3)
	for _, subsystem := range []any{c.lsp, c.dap, c.acp} {
		if waiter, ok := subsystem.(coordinatorShutdownWaiter); ok {
			waiters = append(waiters, waiter)
		}
	}
	if len(waiters) == 0 {
		return true
	}

	results := make(chan bool, len(waiters))
	var wg sync.WaitGroup
	for _, waiter := range waiters {
		wg.Add(1)
		go func(waiter coordinatorShutdownWaiter) {
			defer wg.Done()
			results <- waiter.WaitForShutdown(ctx)
		}(waiter)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(results)
		for result := range results {
			if !result {
				return false
			}
		}
		return true
	case <-ctx.Done():
		return false
	}
}
