package dap

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// DebugConfig describes how to launch a debug adapter.
type DebugConfig struct {
	Type    string   // e.g., "go", "node", "python"
	Command string   // debug adapter command
	Args    []string // command arguments
	Program string   // program to debug
	Cwd     string   // working directory
	Env     map[string]string
}

// Manager manages debug sessions.
type Manager struct {
	client     *Client
	config     DebugConfig
	rootDir    string
	msgChan    chan any
	mu         sync.Mutex
	startMu    sync.Mutex // serializes competing starts without blocking Stop
	state      DebugState
	closed     bool
	generation uint64

	lifecycle        int
	lifecycleChanged chan struct{}
	trackedClients   map[*Client]struct{}
}

// NewManager creates a new debug manager.
func NewManager(rootDir string) *Manager {
	return &Manager{
		rootDir:          rootDir,
		msgChan:          make(chan any, 100),
		state:            StateInactive,
		lifecycleChanged: make(chan struct{}),
		trackedClients:   make(map[*Client]struct{}),
	}
}

// Start begins a debug session with the given config.
func (m *Manager) Start(config DebugConfig) error {
	return m.start(config)
}

// StartAndLaunch creates and launches a debug session as one serialized
// lifecycle operation. Keeping both DAP requests under the same manager lock
// prevents a concurrent Stop from leaving a half-started adapter behind.
func (m *Manager) StartAndLaunch(config DebugConfig) error {
	if err := m.start(config); err != nil {
		return err
	}
	if err := m.launch(); err != nil {
		m.Stop()
		return err
	}
	return nil
}

func (m *Manager) start(config DebugConfig) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	var staleClient *Client
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("debug manager is shut down")
	}
	if m.client != nil && m.running() {
		m.mu.Unlock()
		return fmt.Errorf("debug session already active")
	}
	if m.client != nil {
		// A terminated adapter may leave its process alive long enough for the
		// next start attempt to arrive. Detach and shut it down before replacing
		// the manager's client so restart cannot orphan the old process.
		staleClient = m.client
		m.client = nil
		m.state = StateInactive
		m.trackClientLocked(staleClient)
	}
	m.config = config
	generation := m.generation
	m.beginLifecycleLocked()
	m.mu.Unlock()
	defer m.endLifecycle()
	if staleClient != nil {
		staleClient.Shutdown()
	}

	client, err := NewClient(config.Command, config.Args, m.msgChan)
	if err != nil {
		return fmt.Errorf("start debug adapter: %w", err)
	}

	m.mu.Lock()
	m.trackClientLocked(client)
	if m.closed || generation != m.generation {
		m.mu.Unlock()
		client.Shutdown()
		return fmt.Errorf("debug session start was cancelled")
	}
	// Publish immediately so Stop can terminate an adapter that hangs during
	// initialize. Never hold the manager lock while a DAP request is in flight.
	m.client = client
	m.state = StateInactive
	m.mu.Unlock()

	// Initialize the debug adapter
	if err := client.Initialize(); err != nil {
		client.Shutdown()
		m.mu.Lock()
		if m.client == client {
			m.client = nil
		}
		m.mu.Unlock()
		return fmt.Errorf("initialize debug adapter: %w", err)
	}

	m.mu.Lock()
	if m.client != client {
		m.mu.Unlock()
		client.Shutdown()
		return fmt.Errorf("debug session stopped while initializing")
	}
	m.state = StateStopped
	m.mu.Unlock()
	return nil
}

// Launch starts debugging the program.
func (m *Manager) Launch() error {
	return m.launch()
}

func (m *Manager) launch() error {
	m.mu.Lock()
	client := m.client
	config := m.config
	m.mu.Unlock()
	if client == nil {
		return fmt.Errorf("no debug session")
	}
	if err := client.Launch(config.Program); err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	m.mu.Lock()
	if m.client == client {
		m.state = StateRunning
	}
	m.mu.Unlock()
	return nil
}

// Stop stops the debug session.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.generation++
	client := m.client
	m.client = nil
	m.state = StateInactive
	m.trackClientLocked(client)
	m.mu.Unlock()
	if client != nil {
		client.Shutdown()
	}
}

// Shutdown permanently closes the manager and initiates adapter teardown.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	m.generation++
	client := m.client
	m.client = nil
	m.state = StateInactive
	m.trackClientLocked(client)
	m.mu.Unlock()
	if client != nil {
		client.Shutdown()
	}
}

// WaitForShutdown waits for in-flight starts to unwind and for every tracked
// adapter process to be reaped, or until ctx is cancelled.
func (m *Manager) WaitForShutdown(ctx context.Context) bool {
	m.Shutdown()
	for {
		m.mu.Lock()
		if m.lifecycle == 0 {
			m.mu.Unlock()
			return true
		}
		changed := m.lifecycleChanged
		m.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

func (m *Manager) beginLifecycleLocked() {
	m.lifecycle++
}

func (m *Manager) signalLifecycleLocked() {
	close(m.lifecycleChanged)
	m.lifecycleChanged = make(chan struct{})
}

func (m *Manager) endLifecycle() {
	m.mu.Lock()
	if m.lifecycle > 0 {
		m.lifecycle--
	}
	m.signalLifecycleLocked()
	m.mu.Unlock()
}

func (m *Manager) trackClientLocked(client *Client) {
	if client == nil {
		return
	}
	if _, ok := m.trackedClients[client]; ok {
		return
	}
	m.trackedClients[client] = struct{}{}
	m.beginLifecycleLocked()
	go func() {
		_ = client.WaitForShutdown(context.Background())
		m.mu.Lock()
		delete(m.trackedClients, client)
		if m.lifecycle > 0 {
			m.lifecycle--
		}
		m.signalLifecycleLocked()
		m.mu.Unlock()
	}()
}

// errNoThreads reports that the adapter listed no threads to act on.
var errNoThreads = errors.New("debug session has no threads")

// activeThreadID returns the thread the adapter stopped in when it is still
// listed, otherwise the first listed thread. It returns errNoThreads when the
// adapter lists none, so a step can never report success without acting.
func activeThreadID(client *Client) (int, error) {
	threads, err := client.Threads()
	if err != nil {
		return 0, err
	}
	if len(threads) == 0 {
		return 0, errNoThreads
	}
	if stopped := client.StoppedThreadID(); stopped > 0 {
		for _, thread := range threads {
			if thread.Id == stopped {
				return stopped, nil
			}
		}
	}
	return threads[0].Id, nil
}

// Continue resumes execution.
func (m *Manager) Continue() error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return fmt.Errorf("no debug session")
	}

	threadID, err := activeThreadID(client)
	if err != nil {
		return err
	}
	if err := client.Continue(threadID); err != nil {
		return err
	}
	m.mu.Lock()
	if m.client == client {
		m.state = StateRunning
	}
	m.mu.Unlock()
	return nil
}

// Next steps over to the next line.
func (m *Manager) Next() error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return fmt.Errorf("no debug session")
	}

	threadID, err := activeThreadID(client)
	if err != nil {
		return err
	}
	return client.Next(threadID)
}

// StepIn steps into a function call.
func (m *Manager) StepIn() error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return fmt.Errorf("no debug session")
	}

	threadID, err := activeThreadID(client)
	if err != nil {
		return err
	}
	return client.StepIn(threadID)
}

// StepOut steps out of the current function.
func (m *Manager) StepOut() error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return fmt.Errorf("no debug session")
	}

	threadID, err := activeThreadID(client)
	if err != nil {
		return err
	}
	return client.StepOut(threadID)
}

// SetBreakpoints sets breakpoints in a file.
func (m *Manager) SetBreakpoints(filePath string, lines []int) ([]Breakpoint, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("no debug session")
	}
	return client.SetBreakpoints(filePath, lines)
}

// GetStackTrace returns the stack trace for the current thread.
func (m *Manager) GetStackTrace() ([]StackFrame, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	threadID, err := activeThreadID(client)
	if errors.Is(err, errNoThreads) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return client.StackTrace(threadID)
}

// GetVariables returns variables in a scope.
func (m *Manager) GetVariables(variablesReference int) ([]Variable, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	return client.Variables(variablesReference)
}

// GetScopes returns scopes for a stack frame.
func (m *Manager) GetScopes(frameId int) ([]Scope, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("no debug session")
	}

	return client.Scopes(frameId)
}

// State returns the current debug state.
func (m *Manager) State() DebugState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && !m.client.IsReady() {
		return StateInactive
	}
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

// DefaultNodeDebugConfig returns an empty config until Node DAP support exists.
func DefaultNodeDebugConfig(program string) DebugConfig {
	_ = program
	return DebugConfig{}
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
