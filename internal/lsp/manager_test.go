package lsp

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestManagerGlobalClientBudgetRejectsWithoutRetryAndReleasesAfterReap(t *testing.T) {
	root := t.TempDir()
	budget := newClientBudget(1)
	first := NewManager(root, []ServerConfig{{Extensions: []string{".one"}, Command: "first-lsp", LanguageID: "one"}})
	second := NewManager(root, []ServerConfig{{Extensions: []string{".two"}, Command: "second-lsp", LanguageID: "two"}})
	// Production managers share one package-global budget. A dedicated shared
	// instance keeps this test isolated while proving the cross-manager policy.
	first.clientBudget = budget
	second.clientBudget = budget

	var firstClient *Client
	first.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		firstClient = newBudgetTestClient()
		return firstClient, nil
	}
	secondCreated := 0
	second.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		secondCreated++
		return newBudgetTestClient(), nil
	}
	first.initClient = func(*Client) error { return nil }
	second.initClient = func(*Client) error { return nil }

	if _, err := first.EnsureClient(filepath.Join(root, "one.one")); err != nil {
		t.Fatalf("first EnsureClient() error = %v", err)
	}
	if got := budget.inUse(); got != 1 {
		t.Fatalf("budget in use after first start = %d, want 1", got)
	}

	if _, err := second.EnsureClient(filepath.Join(root, "two.two")); !errors.Is(err, ErrClientCapacity) {
		t.Fatalf("second EnsureClient() error = %v, want ErrClientCapacity", err)
	}
	if secondCreated != 0 {
		t.Fatalf("newClient calls after capacity rejection = %d, want 0", secondCreated)
	}
	if retries := second.retries["second-lsp"]; retries != 0 {
		t.Fatalf("capacity rejection retries = %d, want 0", retries)
	}

	first.ShutdownAll()
	close(firstClient.processDone) // the real reaper closes this after wait(2).
	deadline := time.Now().Add(time.Second)
	for budget.inUse() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := budget.inUse(); got != 0 {
		t.Fatalf("budget in use after client reap = %d, want 0", got)
	}

	client, err := second.EnsureClient(filepath.Join(root, "two.two"))
	if err != nil {
		t.Fatalf("EnsureClient() after release error = %v", err)
	}
	if client == nil || secondCreated != 1 {
		t.Fatalf("EnsureClient() after release = %p, created %d; want client and one creation", client, secondCreated)
	}
	second.ShutdownAll()
	close(client.processDone)
}

func newBudgetTestClient() *Client {
	return &Client{
		pending:     make(map[int]chan callResult),
		openDocs:    make(map[string]int),
		running:     true,
		initialized: true,
		cancelRead:  func() {},
		processDone: make(chan struct{}),
	}
}

func TestManagerClientBudgetReleasesFailedStartBeforeAnotherManagerRetries(t *testing.T) {
	root := t.TempDir()
	budget := newClientBudget(1)
	failing := NewManager(root, []ServerConfig{{Extensions: []string{".fail"}, Command: "failing-lsp", LanguageID: "fail"}})
	retrying := NewManager(root, []ServerConfig{{Extensions: []string{".retry"}, Command: "retry-lsp", LanguageID: "retry"}})
	failing.clientBudget = budget
	retrying.clientBudget = budget
	failing.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		return nil, fmt.Errorf("executable unavailable")
	}
	retrying.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		return newBudgetTestClient(), nil
	}
	retrying.initClient = func(*Client) error { return nil }

	if _, err := failing.EnsureClient(filepath.Join(root, "bad.fail")); err == nil {
		t.Fatal("failed start returned nil error")
	}
	if got := budget.inUse(); got != 0 {
		t.Fatalf("budget in use after failed start = %d, want 0", got)
	}

	client, err := retrying.EnsureClient(filepath.Join(root, "good.retry"))
	if err != nil {
		t.Fatalf("retry manager EnsureClient() error = %v", err)
	}
	retrying.ShutdownAll()
	close(client.processDone)
}

func TestManagerClientBudgetReleasesMalformedNilClientResult(t *testing.T) {
	m := NewManager(t.TempDir(), []ServerConfig{{Extensions: []string{".nil"}, Command: "nil-lsp", LanguageID: "nil"}})
	budget := newClientBudget(1)
	m.clientBudget = budget
	m.newClient = func(ServerConfig, string, chan<- any) (*Client, error) { return nil, nil }

	if _, err := m.EnsureClient("file.nil"); err == nil {
		t.Fatal("nil client result returned nil error")
	}
	if got := budget.inUse(); got != 0 {
		t.Fatalf("budget in use after nil client result = %d, want 0", got)
	}
}

func TestManagerGlobalClientBudgetCountsClientWhileItIsStarting(t *testing.T) {
	root := t.TempDir()
	budget := newClientBudget(1)
	starting := NewManager(root, []ServerConfig{{Extensions: []string{".start"}, Command: "starting-lsp", LanguageID: "start"}})
	contender := NewManager(root, []ServerConfig{{Extensions: []string{".other"}, Command: "other-lsp", LanguageID: "other"}})
	starting.clientBudget = budget
	contender.clientBudget = budget

	created := make(chan struct{})
	continueStart := make(chan struct{})
	var startedClient *Client
	starting.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		close(created)
		<-continueStart
		startedClient = newBudgetTestClient()
		return startedClient, nil
	}
	starting.initClient = func(*Client) error { return nil }
	contender.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		t.Fatal("capacity-rejected contender attempted process creation")
		return nil, nil
	}

	startResult := make(chan error, 1)
	go func() {
		_, err := starting.EnsureClient(filepath.Join(root, "opening.start"))
		startResult <- err
	}()
	select {
	case <-created:
	case <-time.After(time.Second):
		t.Fatal("initial start did not reserve capacity")
	}
	if _, err := contender.EnsureClient(filepath.Join(root, "other.other")); !errors.Is(err, ErrClientCapacity) {
		t.Fatalf("contender EnsureClient() error = %v, want ErrClientCapacity while first is starting", err)
	}
	close(continueStart)
	if err := <-startResult; err != nil {
		t.Fatalf("initial EnsureClient() error = %v", err)
	}
	starting.ShutdownAll()
	close(startedClient.processDone)
}

func TestManagerClientBudgetHoldsInitializationFailureUntilClientIsReaped(t *testing.T) {
	root := t.TempDir()
	budget := newClientBudget(1)
	m := NewManager(root, []ServerConfig{{Extensions: []string{".init"}, Command: "init-lsp", LanguageID: "init"}})
	m.clientBudget = budget

	failedClient := newBudgetTestClient()
	created := 0
	m.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		created++
		if created == 1 {
			return failedClient, nil
		}
		return newBudgetTestClient(), nil
	}
	m.initClient = func(*Client) error {
		if created == 1 {
			return fmt.Errorf("initialize failed")
		}
		return nil
	}

	path := filepath.Join(root, "main.init")
	if _, err := m.EnsureClient(path); err == nil {
		t.Fatal("initialization failure returned nil error")
	}
	if got := budget.inUse(); got != 1 {
		t.Fatalf("budget in use before failed client reap = %d, want 1", got)
	}
	close(failedClient.processDone)
	deadline := time.Now().Add(time.Second)
	for budget.inUse() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := budget.inUse(); got != 0 {
		t.Fatalf("budget in use after failed client reap = %d, want 0", got)
	}

	client, err := m.EnsureClient(path)
	if err != nil {
		t.Fatalf("EnsureClient() retry after failed initialization = %v", err)
	}
	m.ShutdownAll()
	close(client.processDone)
}

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

func TestManagerConfigForFileUsesMergedUserConfig(t *testing.T) {
	m := NewManager("/tmp", []ServerConfig{
		{
			Extensions: []string{".go"},
			Command:    "custom-gopls",
			Args:       []string{"--stdio"},
			LanguageID: "go-custom",
		},
	})

	cfg := m.ConfigForFile("/tmp/main.go")
	if cfg == nil {
		t.Fatal("ConfigForFile() returned nil for .go file")
	}
	if cfg.Command != "custom-gopls" {
		t.Fatalf("Command = %q, want %q", cfg.Command, "custom-gopls")
	}
	if cfg.LanguageID != "go-custom" {
		t.Fatalf("LanguageID = %q, want %q", cfg.LanguageID, "go-custom")
	}
}

func TestManagerDocumentLifecycleDropsDelayedOpenAfterClose(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	m := NewManager(root, []ServerConfig{{Extensions: []string{".go"}, Command: "fake-lsp", LanguageID: "go"}})
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	m.clients["fake-lsp"] = client

	generation := m.BeginDocument(path)
	m.CloseDocument(path)
	// Simulate the command that was scheduled by the file-load Update but did
	// not run until the user had already closed the tab.
	m.OpenDocument(path, generation, "go", 1, "late")

	uri := FileURI(path)
	if _, open := client.DocumentVersion(uri); open {
		t.Fatal("a delayed didOpen resurrected a closed document")
	}
	if messages := capturedOutboundMessages(t, writer.Bytes()); len(messages) != 0 {
		t.Fatalf("a delayed didOpen reached outbound transport: %#v", messages)
	}
}

func TestManagerDocumentLifecycleOrdersCloseBeforeReopen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	m := NewManager(root, []ServerConfig{{Extensions: []string{".go"}, Command: "fake-lsp", LanguageID: "go"}})
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	m.clients["fake-lsp"] = client

	first := m.BeginDocument(path)
	m.OpenDocument(path, first, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	m.CloseDocument(path)
	second := m.BeginDocument(path)
	m.OpenDocument(path, second, "go", 1, "two")

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 3)
	if len(messages) != 3 {
		t.Fatalf("outbound messages = %d, want open-close-open", len(messages))
	}
	for index, want := range []string{"textDocument/didOpen", "textDocument/didClose", "textDocument/didOpen"} {
		if got, _ := messages[index]["method"].(string); got != want {
			t.Fatalf("message %d method = %q, want %q", index, got, want)
		}
	}
	uri := FileURI(path)
	if version, open := client.DocumentVersion(uri); !open || version != 1 {
		t.Fatalf("reopened document state = version %d open %v, want open version 1", version, open)
	}
}
