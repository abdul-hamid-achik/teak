package lsp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/charmbracelet/log"

	"teak/internal/toolpath"
)

// Manager manages multiple LSP clients, one per language server.
type Manager struct {
	clients         map[string]*Client // keyed by server command
	configs         []ServerConfig
	rootDir         string
	msgChan         chan any
	mu              sync.RWMutex
	retries         map[string]int
	disabledUntil   map[string]time.Time
	now             func() time.Time         // injectable for tests
	starting        map[string]chan struct{} // guards against concurrent starts
	startingClients map[string]*Client
	closed          bool
	newClient       func(ServerConfig, string, chan<- any) (*Client, error)
	initClient      func(*Client) error
	clientBudget    *clientBudget

	// lifecycle counts both in-flight starts and child processes that still
	// need to be reaped. The change channel allows bounded waits without using
	// sync.WaitGroup.Add concurrently with Wait.
	lifecycle           int
	lifecycleChanged    chan struct{}
	trackedClients      map[*Client]struct{}
	documentGenerations map[string]uint64
}

const (
	maxRetries = 3

	// retryCooldown is how long a server stays disabled after exhausting
	// maxRetries. Without it the first three failures disabled a server for the
	// entire session, so installing a missing server — or fixing a PATH that
	// omitted it — could not take effect without restarting Teak.
	retryCooldown = 60 * time.Second
)

const (

	// A Client can retain a 128 MiB document-snapshot cache, a 72 MiB
	// outbound queue, one 65 MiB frame being written, and up to 10 MiB of
	// framed input. Two concurrently live clients therefore cap the known
	// protocol-side retention at roughly 550 MiB. Language-server process heap
	// is deliberately not counted here because it is owned by the executable,
	// but the same cap bounds its process multiplication as well.
	maxLiveLSPClients              = 2
	maxLSPClientProtocolBytes      = (128 + 72 + 65 + 10) << 20
	maxLSPProtocolReservationBytes = maxLiveLSPClients * maxLSPClientProtocolBytes
)

// ErrClientCapacity means that the process-wide LSP resource budget is fully
// reserved by clients that are still starting or have not yet been reaped.
// It is intentionally distinct from a server startup failure: callers may
// retry after a client is shut down, and it must not burn the retry budget.
var ErrClientCapacity = errors.New("lsp client capacity exhausted")

// clientBudget is a non-blocking, process-wide admission controller. It
// reserves capacity before exec starts and releases it only after the child
// process is reaped. That makes both a burst of lazy starts and a slow
// shutdown safe without blocking Bubble Tea's Update path.
type clientBudget struct {
	mu    sync.Mutex
	limit int
	used  int
}

func newClientBudget(limit int) *clientBudget {
	return &clientBudget{limit: limit}
}

func (b *clientBudget) tryAcquire() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 || b.used >= b.limit {
		return false
	}
	b.used++
	return true
}

func (b *clientBudget) release() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.used > 0 {
		b.used--
	}
	b.mu.Unlock()
}

func (b *clientBudget) inUse() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Every Manager in this process shares this reservation. Teak normally owns
// one manager, but tests, embedded callers, and multiple workspaces must not
// bypass the resource policy by constructing additional managers.
var globalClientBudget = newClientBudget(maxLiveLSPClients)

// clock returns the manager's time source, defaulting to time.Now.
func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// noteFailureLocked records a startup failure and returns the new attempt
// count. On reaching maxRetries it stamps a cooldown so the server is retried
// later instead of being disabled for the rest of the session. Callers must
// hold m.mu.
func (m *Manager) noteFailureLocked(command string) int {
	m.retries[command]++
	if m.retries[command] >= maxRetries {
		if m.disabledUntil == nil {
			m.disabledUntil = make(map[string]time.Time)
		}
		m.disabledUntil[command] = m.clock().Add(retryCooldown)
	}
	return m.retries[command]
}

// NewManager creates a new LSP manager. If userConfigs is non-empty, they are
// merged with the built-in defaults (user entries override by extension match).
func NewManager(rootDir string, userConfigs []ServerConfig) *Manager {
	configs := MergeConfigs(DefaultConfigs(), userConfigs)
	return &Manager{
		clients:             make(map[string]*Client),
		configs:             configs,
		rootDir:             rootDir,
		msgChan:             make(chan any, 100),
		retries:             make(map[string]int),
		disabledUntil:       make(map[string]time.Time),
		starting:            make(map[string]chan struct{}),
		startingClients:     make(map[string]*Client),
		lifecycleChanged:    make(chan struct{}),
		trackedClients:      make(map[*Client]struct{}),
		documentGenerations: make(map[string]uint64),
		newClient:           NewClient,
		initClient: func(client *Client) error {
			return client.Initialize()
		},
		clientBudget: globalClientBudget,
	}
}

// BeginDocument reserves a lifecycle generation before an asynchronous open
// command starts a server. Closing the tab advances the generation immediately,
// making a late command harmless even when no Client existed at scheduling time.
func (m *Manager) BeginDocument(filePath string) uint64 {
	uri := FileURI(filePath)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.documentGenerations == nil {
		m.documentGenerations = make(map[string]uint64)
	}
	m.documentGenerations[uri]++
	return m.documentGenerations[uri]
}

// DocumentGeneration returns the current lifecycle for filePath. Zero means
// no app-owned lifecycle has been reserved.
func (m *Manager) DocumentGeneration(filePath string) uint64 {
	uri := FileURI(filePath)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.documentGenerations[uri]
}

func (m *Manager) lifecycleCurrent(filePath string, generation uint64) (*Client, bool) {
	uri := FileURI(filePath)
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.documentGenerations[uri] != generation {
		return nil, false
	}
	client := m.clients[cfg.Command]
	if client == nil || !client.IsReady() {
		return nil, false
	}
	return client, true
}

// OpenDocument sends didOpen only if the reservation still names the current
// tab lifecycle. The client repeats the generation check around its local
// state mutation, closing the check/use gap with CloseDocument.
// OpenDocument notifies the language server that a document is open. It returns
// the error from starting the server, if any, so callers can surface it: a
// silent failure here previously left the editor reporting "LSP ready" while no
// server existed, which is indistinguishable from a broken language server.
func (m *Manager) OpenDocument(filePath string, generation uint64, languageID string, version int, content string) error {
	uri := FileURI(filePath)
	m.mu.Lock()
	current := !m.closed && m.documentGenerations[uri] == generation
	m.mu.Unlock()
	if !current {
		return nil
	}
	client, err := m.EnsureClient(filePath)
	if err != nil {
		return err
	}
	if client == nil {
		return nil
	}
	if current, ok := m.lifecycleCurrent(filePath, generation); !ok || current != client {
		return nil
	}
	if !client.bindDocumentGeneration(uri, generation) {
		return nil
	}
	client.didOpen(uri, languageID, version, content, generation)
	return nil
}

func (m *Manager) ChangeDocument(filePath string, generation uint64, version int, content string) {
	client, ok := m.lifecycleCurrent(filePath, generation)
	if !ok {
		return
	}
	client.didChange(FileURI(filePath), version, content, generation)
}

func (m *Manager) ChangeDocumentIncremental(filePath string, generation uint64, version, startLine, startCol, endLine, endCol int, replacement, content string) {
	client, ok := m.lifecycleCurrent(filePath, generation)
	if !ok {
		return
	}
	client.didChangeIncremental(FileURI(filePath), version, startLine, startCol, endLine, endCol, replacement, content, true, generation)
}

func (m *Manager) SaveDocument(filePath string, generation uint64) {
	client, ok := m.lifecycleCurrent(filePath, generation)
	if !ok {
		return
	}
	client.didSave(FileURI(filePath), generation)
}

// CloseDocument advances the lifecycle and enqueues didClose before returning.
// The enqueue is bounded/non-blocking, so it is safe on the Bubble Tea path
// and establishes close-before-any-future-reopen ordering.
func (m *Manager) CloseDocument(filePath string) {
	uri := FileURI(filePath)
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return
	}
	m.mu.Lock()
	if m.documentGenerations == nil {
		m.documentGenerations = make(map[string]uint64)
	}
	m.documentGenerations[uri]++
	generation := m.documentGenerations[uri]
	client := m.clients[cfg.Command]
	m.mu.Unlock()
	if client != nil {
		client.closeDocumentGeneration(uri, generation)
	}
}

// MsgChan returns the channel for receiving LSP messages.
func (m *Manager) MsgChan() <-chan any {
	return m.msgChan
}

// EnsureClient starts a language server for the given file if not already running.
// The server is initialized synchronously to ensure it's fully ready before being
// exposed to other goroutines. Use ClientForFile to get a ready client.
func (m *Manager) EnsureClient(filePath string) (*Client, error) {
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return nil, nil // No server for this file type
	}

	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, fmt.Errorf("language server manager is shut down")
		}

		// Check if already running and ready
		if client, ok := m.clients[cfg.Command]; ok && client.IsReady() {
			m.mu.Unlock()
			return client, nil
		}

		// Disabled after too many failures, but only until the cooldown lapses.
		// On expiry the retry budget is reset and the binary is looked up again
		// from scratch, so a server installed mid-session becomes usable.
		if m.retries[cfg.Command] >= maxRetries {
			if until, ok := m.disabledUntil[cfg.Command]; ok && m.clock().Before(until) {
				m.mu.Unlock()
				return nil, fmt.Errorf("language server %s disabled after %d failures; retrying in %s",
					cfg.Command, maxRetries, time.Until(until).Round(time.Second))
			}
			m.retries[cfg.Command] = 0
			delete(m.disabledUntil, cfg.Command)
			m.mu.Unlock()
			toolpath.Invalidate(cfg.Command)
			// Re-enter the loop so the shutdown and already-running checks are
			// re-evaluated against state that may have changed while unlocked.
			continue
		}

		if waitCh, ok := m.starting[cfg.Command]; ok {
			m.mu.Unlock()
			<-waitCh
			continue
		}

		waitCh := make(chan struct{})
		budget := m.clientBudget
		if !budget.tryAcquire() {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w (limit %d; known protocol reservation %d MiB)", ErrClientCapacity, maxLiveLSPClients, maxLSPProtocolReservationBytes>>20)
		}
		m.starting[cfg.Command] = waitCh
		m.beginLifecycleLocked()
		m.mu.Unlock()
		defer m.endLifecycle()

		// Create client outside lock
		client, err := m.newClient(*cfg, m.rootDir, m.msgChan)
		if err != nil || client == nil {
			if err == nil {
				err = fmt.Errorf("language server %s returned a nil client", cfg.Command)
			}
			budget.release()
			m.mu.Lock()
			attempt := m.noteFailureLocked(cfg.Command)
			close(waitCh)
			delete(m.starting, cfg.Command)
			m.mu.Unlock()
			log.Error("lsp: failed to start server", "command", cfg.Command, "attempt", attempt, "max", maxRetries, "err", err)
			return nil, err
		}

		// Track the process before initialization so a concurrent application
		// shutdown can terminate and await a server that is still initializing.
		m.mu.Lock()
		m.trackClientLocked(client, budget)
		if m.closed {
			close(waitCh)
			delete(m.starting, cfg.Command)
			m.mu.Unlock()
			client.Shutdown()
			return nil, fmt.Errorf("language server manager is shut down")
		}
		m.startingClients[cfg.Command] = client
		m.mu.Unlock()

		// Initialize SYNCHRONOUSLY to ensure client is fully ready before registration
		// This prevents race conditions where ClientForFile gets an uninitialized client
		if err := m.initClient(client); err != nil {
			m.mu.Lock()
			attempt := m.noteFailureLocked(cfg.Command)
			close(waitCh)
			delete(m.starting, cfg.Command)
			delete(m.startingClients, cfg.Command)
			m.mu.Unlock()
			log.Error("lsp: failed to initialize server", "command", cfg.Command, "attempt", attempt, "max", maxRetries, "err", err)
			client.Shutdown()
			return nil, err
		}

		// Only register AFTER successful initialization. Shutdown may have
		// started while the server was being initialized; never expose it in
		// that case and let the asynchronous client teardown reap it.
		m.mu.Lock()
		if m.closed {
			close(waitCh)
			delete(m.starting, cfg.Command)
			delete(m.startingClients, cfg.Command)
			m.mu.Unlock()
			client.Shutdown()
			return nil, fmt.Errorf("language server manager is shut down")
		}
		m.clients[cfg.Command] = client
		close(waitCh)
		delete(m.starting, cfg.Command)
		delete(m.startingClients, cfg.Command)
		m.mu.Unlock()

		return client, nil
	}
}

// ClientForFile returns the active and ready LSP client for a given file, or nil.
// Returns nil if the client exists but is still initializing.
func (m *Manager) ClientForFile(filePath string) *Client {
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[cfg.Command]
	if !ok || !client.IsReady() {
		return nil
	}
	return client
}

// ConfigForFile returns the effective LSP config (defaults + user overrides) for a file.
func (m *Manager) ConfigForFile(filePath string) *ServerConfig {
	return configForFile(m.configs, filePath)
}

// ShutdownAll gracefully shuts down all language servers.
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	clients := make([]*Client, 0, len(m.clients))
	for name, client := range m.clients {
		clients = append(clients, client)
		m.trackClientLocked(client, nil)
		delete(m.clients, name)
	}
	for _, client := range m.startingClients {
		clients = append(clients, client)
		m.trackClientLocked(client, nil)
	}
	m.mu.Unlock()

	// Shutdown uses bounded background teardown. Do not wait under the
	// manager lock (or on the Bubble Tea update path): a broken language server
	// must not freeze the terminal while it is being killed and reaped.
	for _, client := range clients {
		client.Shutdown()
	}
	// Deliberately keep msgChan open. Clients may still be unwinding their
	// reader/notification goroutines; closing a shared producer channel would
	// turn a late notification into a panic. The app stops consuming it when
	// the program exits and the manager rejects all future starts.
}

// WaitForShutdown initiates shutdown and waits until all starts have unwound
// and every child process has been reaped, or until ctx is cancelled.
func (m *Manager) WaitForShutdown(ctx context.Context) bool {
	m.ShutdownAll()
	for {
		m.mu.RLock()
		if m.lifecycle == 0 {
			m.mu.RUnlock()
			return true
		}
		changed := m.lifecycleChanged
		m.mu.RUnlock()

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

func (m *Manager) trackClientLocked(client *Client, budget *clientBudget) {
	if client == nil {
		budget.release()
		return
	}
	if _, ok := m.trackedClients[client]; ok {
		// A duplicate tracking request owns no additional process. This should
		// only happen when ShutdownAll races with initialization; give back a
		// just-acquired reservation rather than leaking it.
		budget.release()
		return
	}
	m.trackedClients[client] = struct{}{}
	m.beginLifecycleLocked()
	go func() {
		_ = client.WaitForShutdown(context.Background())
		m.mu.Lock()
		delete(m.trackedClients, client)
		// Release before signalling lifecycle completion so a caller that has
		// waited for shutdown can immediately retry a deferred lazy start.
		budget.release()
		if m.lifecycle > 0 {
			m.lifecycle--
		}
		m.signalLifecycleLocked()
		m.mu.Unlock()
	}()
}

// ServerHealth is the observable lifecycle state of the configured server for
// one file. RetryAt is populated while the manager is cooling down after
// repeated startup failures, so a UI can distinguish a deliberate retry wait
// from a server that has never been started.
type ServerHealth struct {
	Name     string
	State    string
	Running  bool
	Ready    bool
	Attempts int
	RetryAt  time.Time
}

// ServerHealth returns the current lifecycle state without starting a server.
// It is intentionally read-only and safe to call from rendering code.
func (m *Manager) ServerHealth(filePath string) ServerHealth {
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return ServerHealth{}
	}
	health := ServerHealth{Name: cfg.Command, State: "idle"}
	m.mu.RLock()
	defer m.mu.RUnlock()
	health.Attempts = m.retries[cfg.Command]
	if m.closed {
		health.State = "stopped"
		return health
	}
	if _, starting := m.starting[cfg.Command]; starting {
		health.State = "starting"
		return health
	}
	if until, cooling := m.disabledUntil[cfg.Command]; cooling && m.clock().Before(until) {
		health.State = "retrying"
		health.RetryAt = until
		return health
	}
	if client, ok := m.clients[cfg.Command]; ok {
		health.Running = client.IsRunning()
		health.Ready = client.IsReady()
		switch {
		case health.Ready:
			health.State = "ready"
		case health.Running:
			health.State = "running"
		default:
			health.State = "failed"
		}
		return health
	}
	if health.Attempts > 0 {
		health.State = "failed"
	}
	return health
}

// ServerStatus retains the compact tuple API used by older callers.
func (m *Manager) ServerStatus(filePath string) (name string, running bool, ready bool) {
	health := m.ServerHealth(filePath)
	return health.Name, health.Running, health.Ready
}
