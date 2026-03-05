package dap

import (
	"fmt"
	"sync"
)

// DebugConfig describes how to launch a debug adapter.
type DebugConfig struct {
	Type      string   // e.g., "go", "node", "python"
	Command   string   // debug adapter command
	Args      []string // command arguments
	Program   string   // program to debug
	Cwd       string   // working directory
	Env       map[string]string
}

// Manager manages debug sessions.
type Manager struct {
	client  *Client
	config  DebugConfig
	rootDir string
	msgChan chan any
	mu      sync.Mutex
	state   DebugState
}

// NewManager creates a new debug manager.
func NewManager(rootDir string) *Manager {
	return &Manager{
		rootDir: rootDir,
		msgChan: make(chan any, 100),
		state:   StateInactive,
	}
}

// Start begins a debug session with the given config.
func (m *Manager) Start(config DebugConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil && m.running() {
		return fmt.Errorf("debug session already active")
	}

	m.config = config

	client, err := NewClient(config.Command, config.Args, m.msgChan)
	if err != nil {
		return fmt.Errorf("start debug adapter: %w", err)
	}

	m.client = client

	// Initialize the debug adapter
	if err := client.Initialize(); err != nil {
		client.Shutdown()
		m.client = nil
		return fmt.Errorf("initialize debug adapter: %w", err)
	}

	m.state = StateStopped
	return nil
}

// Launch starts debugging the program.
func (m *Manager) Launch() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return fmt.Errorf("no debug session")
	}

	if err := m.client.Launch(m.config.Program); err != nil {
		return fmt.Errorf("launch: %w", err)
	}

	m.state = StateRunning
	return nil
}

// Stop stops the debug session.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		m.client.Shutdown()
		m.client = nil
	}
	m.state = StateInactive
}

// Continue resumes execution.
func (m *Manager) Continue() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return fmt.Errorf("no debug session")
	}

	// Get threads first
	threads, err := m.client.Threads()
	if err != nil {
		return err
	}

	if len(threads) > 0 {
		if err := m.client.Continue(threads[0].Id); err != nil {
			return err
		}
		m.state = StateRunning
	}
	return nil
}

// Next steps over to the next line.
func (m *Manager) Next() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return fmt.Errorf("no debug session")
	}

	threads, err := m.client.Threads()
	if err != nil {
		return err
	}

	if len(threads) > 0 {
		if err := m.client.Next(threads[0].Id); err != nil {
			return err
		}
	}
	return nil
}

// StepIn steps into a function call.
func (m *Manager) StepIn() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return fmt.Errorf("no debug session")
	}

	threads, err := m.client.Threads()
	if err != nil {
		return err
	}

	if len(threads) > 0 {
		if err := m.client.StepIn(threads[0].Id); err != nil {
			return err
		}
	}
	return nil
}

// StepOut steps out of the current function.
func (m *Manager) StepOut() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return fmt.Errorf("no debug session")
	}

	threads, err := m.client.Threads()
	if err != nil {
		return err
	}

	if len(threads) > 0 {
		if err := m.client.StepOut(threads[0].Id); err != nil {
			return err
		}
	}
	return nil
}

// SetBreakpoints sets breakpoints in a file.
func (m *Manager) SetBreakpoints(filePath string, lines []int) ([]Breakpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	return m.client.SetBreakpoints(filePath, lines)
}

// GetStackTrace returns the stack trace for the current thread.
func (m *Manager) GetStackTrace() ([]StackFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	threads, err := m.client.Threads()
	if err != nil {
		return nil, err
	}

	if len(threads) == 0 {
		return nil, nil
	}

	return m.client.StackTrace(threads[0].Id)
}

// GetVariables returns variables in a scope.
func (m *Manager) GetVariables(variablesReference int) ([]Variable, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	return m.client.Variables(variablesReference)
}

// GetScopes returns scopes for a stack frame.
func (m *Manager) GetScopes(frameId int) ([]Scope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	return m.client.Scopes(frameId)
}

// State returns the current debug state.
func (m *Manager) State() DebugState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// IsRunning returns whether a debug session is active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running()
}

func (m *Manager) running() bool {
	return m.client != nil && m.client.IsReady()
}

// MsgChan returns the message channel for receiving debug events.
func (m *Manager) MsgChan() <-chan any {
	return m.msgChan
}

// DefaultGoDebugConfig returns a default debug config for Go programs using delve.
func DefaultGoDebugConfig(program string) DebugConfig {
	return DebugConfig{
		Type:    "go",
		Command: "dlv",
		Args:    []string{"dap"},
		Program: program,
	}
}

// DefaultNodeDebugConfig returns a default debug config for Node.js programs.
func DefaultNodeDebugConfig(program string) DebugConfig {
	return DebugConfig{
		Type:    "node",
		Command: "node",
		Args:    []string{"-e", "console.log('Node debug adapter not implemented')"},
		Program: program,
	}
}

// ConfigForProgram returns a debug config for the given program path.
func ConfigForProgram(programPath string) DebugConfig {
	// Simple heuristic based on file extension
	switch {
	case hasExtension(programPath, ".go"):
		return DefaultGoDebugConfig(programPath)
	case hasExtension(programPath, ".js"), hasExtension(programPath, ".ts"):
		return DefaultNodeDebugConfig(programPath)
	default:
		return DebugConfig{}
	}
}

func hasExtension(path, ext string) bool {
	return len(path) >= len(ext) && path[len(path)-len(ext):] == ext
}
