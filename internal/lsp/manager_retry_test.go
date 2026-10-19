package lsp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// newFailingManager builds a Manager whose server startup always fails, with a
// controllable clock so cooldown behaviour is testable without sleeping.
func newFailingManager(t *testing.T, startErr error) (*Manager, *time.Time, *int) {
	t.Helper()
	clock := time.Now()
	attempts := 0
	m := NewManager(t.TempDir(), []ServerConfig{{
		Command:    "test-language-server",
		Extensions: []string{".tst"},
		LanguageID: "test",
	}})
	m.now = func() time.Time { return clock }
	m.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		attempts++
		return nil, startErr
	}
	return m, &clock, &attempts
}

func TestEnsureClientDisablesServerAfterMaxRetries(t *testing.T) {
	m, _, attempts := newFailingManager(t, errors.New("boom"))

	for range maxRetries {
		if _, err := m.EnsureClient("main.tst"); err == nil {
			t.Fatal("EnsureClient: expected startup failure")
		}
	}
	if *attempts != maxRetries {
		t.Fatalf("start attempts = %d, want %d", *attempts, maxRetries)
	}

	_, err := m.EnsureClient("main.tst")
	if err == nil {
		t.Fatal("EnsureClient: expected server to be disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error = %q, want it to mention the server is disabled", err)
	}
	if *attempts != maxRetries {
		t.Errorf("start attempts = %d, want no further attempt while disabled", *attempts)
	}
}

func TestDisabledServerIsRetriedAfterCooldown(t *testing.T) {
	m, clock, attempts := newFailingManager(t, errors.New("boom"))

	for range maxRetries {
		_, _ = m.EnsureClient("main.tst")
	}
	if *attempts != maxRetries {
		t.Fatalf("start attempts = %d, want %d", *attempts, maxRetries)
	}

	// Before the cooldown lapses the server stays disabled.
	_, _ = m.EnsureClient("main.tst")
	if *attempts != maxRetries {
		t.Fatalf("start attempts = %d, want no retry before cooldown expiry", *attempts)
	}

	*clock = clock.Add(retryCooldown + time.Second)

	// After the cooldown the server must be tried again: this is what lets a
	// language server installed mid-session start working without restarting
	// Teak, which was previously impossible.
	_, _ = m.EnsureClient("main.tst")
	if *attempts != maxRetries+1 {
		t.Errorf("start attempts = %d, want %d after cooldown expiry", *attempts, maxRetries+1)
	}
}

func TestServerHealthReportsRetryingCooldown(t *testing.T) {
	m, clock, _ := newFailingManager(t, errors.New("boom"))

	for range maxRetries {
		_, _ = m.EnsureClient("main.tst")
	}

	health := m.ServerHealth("main.tst")
	if health.Name != "test-language-server" {
		t.Fatalf("server name = %q, want test-language-server", health.Name)
	}
	if health.State != "retrying" {
		t.Fatalf("server state = %q, want retrying", health.State)
	}
	if health.Attempts != maxRetries {
		t.Fatalf("server attempts = %d, want %d", health.Attempts, maxRetries)
	}
	if !health.RetryAt.After(*clock) {
		t.Fatalf("retry deadline = %v, want after %v", health.RetryAt, *clock)
	}
}

func TestCapacityExhaustionDoesNotBurnRetryBudget(t *testing.T) {
	m, _, attempts := newFailingManager(t, errors.New("boom"))
	// Saturate the shared budget so tryAcquire fails before any start happens.
	m.clientBudget = newClientBudget(0)

	for range maxRetries + 2 {
		_, err := m.EnsureClient("main.tst")
		if !errors.Is(err, ErrClientCapacity) {
			t.Fatalf("EnsureClient error = %v, want ErrClientCapacity", err)
		}
	}
	if *attempts != 0 {
		t.Errorf("start attempts = %d, want 0 when capacity is exhausted", *attempts)
	}
	// Capacity pressure is transient, so it must not disable the server.
	m.mu.RLock()
	retries := m.retries["test-language-server"]
	m.mu.RUnlock()
	if retries != 0 {
		t.Errorf("retries = %d, want 0: capacity exhaustion must not count as a startup failure", retries)
	}
}

func TestOpenDocumentReturnsStartupError(t *testing.T) {
	m, _, _ := newFailingManager(t, errors.New("boom"))

	generation := m.BeginDocument("main.tst")
	err := m.OpenDocument("main.tst", generation, "test", 1, "content")
	// A swallowed error here previously let the editor report "LSP ready" for a
	// server that never started.
	if err == nil {
		t.Fatal("OpenDocument: expected the startup error to be returned")
	}
}

func TestOpenDocumentIgnoresRetiredGeneration(t *testing.T) {
	m, _, attempts := newFailingManager(t, errors.New("boom"))

	stale := m.BeginDocument("main.tst")
	m.BeginDocument("main.tst") // closing and reopening the tab retires `stale`

	if err := m.OpenDocument("main.tst", stale, "test", 1, "content"); err != nil {
		t.Errorf("OpenDocument for a retired generation returned %v, want nil", err)
	}
	if *attempts != 0 {
		t.Errorf("start attempts = %d, want 0 for a retired generation", *attempts)
	}
}

// A successful start must clear the consecutive-failure budget. Otherwise two
// earlier transient failures leave the counter at maxRetries-1 for the rest of
// the session, and one later crash-restart failure disables a healthy server.
func TestSuccessfulStartResetsStartupFailureBudget(t *testing.T) {
	m := NewManager(t.TempDir(), []ServerConfig{{
		Command:    "budget-language-server",
		Extensions: []string{".bgt"},
		LanguageID: "bgt",
	}})
	m.now = func() time.Time { return time.Unix(1_000_000, 0) }
	m.initClient = func(*Client) error { return nil }

	succeed := false
	attempts := 0
	var live *Client
	m.newClient = func(ServerConfig, string, chan<- any) (*Client, error) {
		attempts++
		if !succeed {
			return nil, errors.New("boom")
		}
		live = newBudgetTestClient()
		return live, nil
	}
	defer func() {
		if live != nil {
			close(live.processDone)
		}
	}()

	for range maxRetries - 1 {
		if _, err := m.EnsureClient("main.bgt"); err == nil {
			t.Fatal("EnsureClient: expected startup failure")
		}
	}
	succeed = true
	if _, err := m.EnsureClient("main.bgt"); err != nil {
		t.Fatalf("EnsureClient after transient failures: %v", err)
	}

	// Simulate the server crashing: the registered client is no longer ready.
	live.mu.Lock()
	live.running = false
	live.mu.Unlock()

	succeed = false
	_, _ = m.EnsureClient("main.bgt") // one fresh failure after recovery
	before := attempts
	_, err := m.EnsureClient("main.bgt")
	if err != nil && strings.Contains(err.Error(), "disabled") {
		t.Fatalf("server disabled after a single failure following a successful start: %v", err)
	}
	if attempts == before {
		t.Errorf("no restart attempt after a single failure following a successful start")
	}
}
