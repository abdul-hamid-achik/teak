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
		m.EnsureClient("main.tst")
	}
	if *attempts != maxRetries {
		t.Fatalf("start attempts = %d, want %d", *attempts, maxRetries)
	}

	// Before the cooldown lapses the server stays disabled.
	m.EnsureClient("main.tst")
	if *attempts != maxRetries {
		t.Fatalf("start attempts = %d, want no retry before cooldown expiry", *attempts)
	}

	*clock = clock.Add(retryCooldown + time.Second)

	// After the cooldown the server must be tried again: this is what lets a
	// language server installed mid-session start working without restarting
	// Teak, which was previously impossible.
	m.EnsureClient("main.tst")
	if *attempts != maxRetries+1 {
		t.Errorf("start attempts = %d, want %d after cooldown expiry", *attempts, maxRetries+1)
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
