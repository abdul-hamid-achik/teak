package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"teak/internal/dap"
)

// debugAction identifies an adapter request that can block on DAP I/O.
type debugAction uint8

const (
	debugActionContinue debugAction = iota
	debugActionNext
	debugActionStepIn
	debugActionStepOut
)

type debugLifecycleState struct {
	generation    uint64
	pending       bool
	stopping      bool
	actionPending bool
}

// debugRunner is the narrow boundary between Bubble Tea's event loop and
// blocking DAP operations. Its methods are always called from tea.Cmds.
type debugRunner interface {
	StartAndLaunch(dap.DebugConfig) error
	Stop()
	Run(debugAction) error
}

type managerDebugRunner struct {
	manager *dap.Manager
}

func (r managerDebugRunner) StartAndLaunch(config dap.DebugConfig) error {
	if r.manager == nil {
		return fmt.Errorf("debug manager unavailable")
	}
	return r.manager.StartAndLaunch(config)
}

func (r managerDebugRunner) Stop() {
	if r.manager != nil {
		r.manager.Stop()
	}
}

func (r managerDebugRunner) Run(action debugAction) error {
	if r.manager == nil {
		return fmt.Errorf("debug manager unavailable")
	}
	switch action {
	case debugActionContinue:
		return r.manager.Continue()
	case debugActionNext:
		return r.manager.Next()
	case debugActionStepIn:
		return r.manager.StepIn()
	case debugActionStepOut:
		return r.manager.StepOut()
	default:
		return fmt.Errorf("unknown debug action")
	}
}

func (m Model) runnerForDebugCommand() debugRunner {
	if m.debugRunner != nil {
		return m.debugRunner
	}
	return managerDebugRunner{manager: m.debugMgr}
}

func (m Model) handleDebugStart() (tea.Model, tea.Cmd) {
	if m.debugLifecycle.pending {
		return m, nil
	}
	editor := m.activeEditor()
	if editor == nil || editor.Buffer.FilePath == "" {
		return m, nil
	}
	config := dap.ConfigForProgram(editor.Buffer.FilePath)
	if config.Command == "" {
		m.status = "No debugger configured for this file type"
		return m, nil
	}

	m.debugLifecycle.generation++
	m.debugLifecycle.pending = true
	m.debugLifecycle.stopping = false
	generation := m.debugLifecycle.generation
	runner := m.runnerForDebugCommand()
	m.status = "Starting debugger…"
	return m, func() tea.Msg {
		return debugStartResultMsg{
			Generation: generation,
			Err:        runner.StartAndLaunch(config),
		}
	}
}

func (m Model) handleDebugStop(status string) (tea.Model, tea.Cmd) {
	if m.debugLifecycle.pending && m.debugLifecycle.stopping {
		return m, nil
	}
	if !m.debugLifecycle.pending && m.debugRunner == nil && (m.debugMgr == nil || !m.debugMgr.IsRunning()) {
		return m, nil
	}
	m.debugLifecycle.generation++
	m.debugLifecycle.pending = true
	m.debugLifecycle.stopping = true
	generation := m.debugLifecycle.generation
	runner := m.runnerForDebugCommand()
	m.status = "Stopping debugger…"
	m.debuggerPanel.SetState(dap.StateInactive)
	m.setExecutionLocation("", -1)
	return m, func() tea.Msg {
		runner.Stop()
		return debugStopResultMsg{Generation: generation, Status: status}
	}
}

func (m Model) handleDebugAction(action debugAction) (tea.Model, tea.Cmd) {
	if m.debugLifecycle.pending || m.debugLifecycle.actionPending {
		return m, nil
	}
	if m.debugRunner == nil && (m.debugMgr == nil || !m.debugMgr.IsRunning()) {
		return m, nil
	}
	m.debugLifecycle.actionPending = true
	generation := m.debugLifecycle.generation
	runner := m.runnerForDebugCommand()
	return m, func() tea.Msg {
		return debugActionResultMsg{
			Generation: generation,
			Action:     action,
			Err:        runner.Run(action),
		}
	}
}

func (m Model) handleDebugStartResult(msg debugStartResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.debugLifecycle.generation || !m.debugLifecycle.pending {
		return m, nil
	}
	m.debugLifecycle.pending = false
	m.debugLifecycle.stopping = false
	if msg.Err != nil {
		m.status = fmt.Sprintf("Debug error: %v", msg.Err)
		return m, nil
	}
	m.debuggerPanel.SetState(dap.StateRunning)
	m.showTree = true
	m.sidebarTab = SidebarDebugger
	m.setFocus(FocusDebugger)
	m.status = "Debugging started"
	m.relayout()
	return m, m.syncAllBreakpointsToDAP()
}

func (m Model) handleDebugStopResult(msg debugStopResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.debugLifecycle.generation || !m.debugLifecycle.pending {
		return m, nil
	}
	m.debugLifecycle.pending = false
	m.debugLifecycle.stopping = false
	m.debugLifecycle.actionPending = false
	m.debuggerPanel.SetState(dap.StateInactive)
	m.setExecutionLocation("", -1)
	m.status = msg.Status
	return m, nil
}

func (m Model) handleDebugActionResult(msg debugActionResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.debugLifecycle.generation {
		return m, nil
	}
	m.debugLifecycle.actionPending = false
	if msg.Err != nil {
		m.status = fmt.Sprintf("Debug error: %v", msg.Err)
		return m, nil
	}
	if msg.Action == debugActionContinue || msg.Action == debugActionNext || msg.Action == debugActionStepIn || msg.Action == debugActionStepOut {
		m.debuggerPanel.SetState(dap.StateRunning)
		m.setExecutionLocation("", -1)
	}
	return m, nil
}
