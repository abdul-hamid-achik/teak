package lsp

import (
	"fmt"
	"log"
	"sync"
)

// Manager manages multiple LSP clients, one per language server.
type Manager struct {
	clients map[string]*Client // keyed by server command
	configs []ServerConfig
	rootDir string
	msgChan chan any
	mu      sync.Mutex
	retries map[string]int
	closed  bool
}

const maxRetries = 3

// NewManager creates a new LSP manager. If userConfigs is non-empty, they are
// merged with the built-in defaults (user entries override by extension match).
func NewManager(rootDir string, userConfigs []ServerConfig) *Manager {
	configs := MergeConfigs(DefaultConfigs(), userConfigs)
	return &Manager{
		clients: make(map[string]*Client),
		configs: configs,
		rootDir: rootDir,
		msgChan: make(chan any, 100),
		retries: make(map[string]int),
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

	m.mu.Lock()

	// Check if already running and ready
	if client, ok := m.clients[cfg.Command]; ok && client.IsReady() {
		m.mu.Unlock()
		return client, nil
	}

	// Check if disabled due to too many retries
	if m.retries[cfg.Command] >= maxRetries {
		m.mu.Unlock()
		return nil, fmt.Errorf("language server %s disabled after %d failures", cfg.Command, maxRetries)
	}

	// Remove any failed/initializing client to retry
	if _, ok := m.clients[cfg.Command]; ok {
		delete(m.clients, cfg.Command)
	}

	m.mu.Unlock()

	// Create client outside lock
	client, err := NewClient(*cfg, m.rootDir, m.msgChan)
	if err != nil {
		m.mu.Lock()
		m.retries[cfg.Command]++
		m.mu.Unlock()
		log.Printf("lsp: failed to start %s (attempt %d/%d): %v", cfg.Command, m.retries[cfg.Command], maxRetries, err)
		return nil, err
	}

	// Initialize SYNCHRONOUSLY to ensure client is fully ready before registration
	// This prevents race conditions where ClientForFile gets an uninitialized client
	if err := client.Initialize(); err != nil {
		m.mu.Lock()
		m.retries[cfg.Command]++
		m.mu.Unlock()
		log.Printf("lsp: failed to initialize %s (attempt %d/%d): %v", cfg.Command, m.retries[cfg.Command], maxRetries, err)
		client.Shutdown()
		return nil, err
	}

	// Only register AFTER successful initialization
	m.mu.Lock()
	m.clients[cfg.Command] = client
	m.mu.Unlock()

	return client, nil
}

// ClientForFile returns the active and ready LSP client for a given file, or nil.
// Returns nil if the client exists but is still initializing.
func (m *Manager) ClientForFile(filePath string) *Client {
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	client, ok := m.clients[cfg.Command]
	if !ok || !client.IsReady() {
		return nil
	}
	return client
}

// ShutdownAll gracefully shuts down all language servers.
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	for name, client := range m.clients {
		client.Shutdown()
		delete(m.clients, name)
	}
	close(m.msgChan)
	m.closed = true
}

// ServerStatus returns the status of the language server for a file.
// Returns the server command name, whether it's running, and whether it's ready.
func (m *Manager) ServerStatus(filePath string) (name string, running bool, ready bool) {
	cfg := configForFile(m.configs, filePath)
	if cfg == nil {
		return "", false, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	client, ok := m.clients[cfg.Command]
	if !ok {
		return cfg.Command, false, false
	}
	return cfg.Command, true, client.IsReady()
}
