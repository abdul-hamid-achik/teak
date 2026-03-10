package lsp

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestManagerDoubleClose(t *testing.T) {
	m := NewManager("/tmp", nil)

	// First shutdown should work
	m.ShutdownAll()

	// Second shutdown should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ShutdownAll() panicked on second call: %v", r)
		}
	}()

	m.ShutdownAll()
	m.ShutdownAll() // Third call to be extra safe
}

func TestManagerCloseAfterPartialShutdown(t *testing.T) {
	m := NewManager("/tmp", nil)

	// Close should be idempotent
	m.ShutdownAll()

	// Verify channel is closed by checking it returns zero value
	select {
	case msg := <-m.MsgChan():
		if msg != nil {
			t.Error("Expected nil from closed channel")
		}
	default:
		// Channel might be closed but not drained
	}
}

func TestManagerEnsureClientWaitsForConcurrentStartup(t *testing.T) {
	rootDir := t.TempDir()
	cfg := ServerConfig{
		Extensions: []string{".go"},
		Command:    "fake-lsp",
		LanguageID: "go",
	}
	m := NewManager(rootDir, []ServerConfig{cfg})

	ready := make(chan struct{})
	var created int
	var createdMu sync.Mutex

	m.newClient = func(cfg ServerConfig, rootDir string, msgChan chan<- any) (*Client, error) {
		createdMu.Lock()
		created++
		createdMu.Unlock()
		<-ready
		return &Client{
			pending:     make(map[int]chan callResult),
			openDocs:    make(map[string]int),
			running:     true,
			initialized: true,
			msgChan:     msgChan,
			cancelRead:  func() {},
		}, nil
	}
	m.initClient = func(client *Client) error {
		return nil
	}

	path := filepath.Join(rootDir, "main.go")
	results := make(chan *Client, 2)
	errs := make(chan error, 2)

	go func() {
		client, err := m.EnsureClient(path)
		results <- client
		errs <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		_, ok := m.starting[cfg.Command]
		m.mu.Unlock()
		if ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for startup to begin")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	go func() {
		client, err := m.EnsureClient(path)
		results <- client
		errs <- err
	}()

	close(ready)

	client1 := <-results
	client2 := <-results
	err1 := <-errs
	err2 := <-errs

	if err1 != nil {
		t.Fatalf("first EnsureClient() error = %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second EnsureClient() error = %v", err2)
	}
	if client1 == nil || client2 == nil {
		t.Fatalf("EnsureClient() returned nil client(s): %v %v", client1, client2)
	}
	if client1 != client2 {
		t.Fatalf("EnsureClient() returned different clients: %p != %p", client1, client2)
	}

	createdMu.Lock()
	defer createdMu.Unlock()
	if created != 1 {
		t.Fatalf("created %d clients, want 1", created)
	}
}
