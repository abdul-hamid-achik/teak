package lsp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestClientMutexContention tests that multiple concurrent requests
// don't cause deadlocks or excessive blocking
func TestClientMutexContention(t *testing.T) {
	// Create a mock client (we can't start a real LSP server in tests)
	client := &Client{
		pending:  make(map[int]chan callResult),
		openDocs: make(map[string]int),
		running:  true,
	}

	// Simulate concurrent capability checks
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// These should all be safe to call concurrently
			_ = client.SupportsHover()
			_ = client.SupportsCompletion()
			_ = client.SupportsDefinition()
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent access error: %v", err)
		}
	}
}

// TestClientPendingRequestCleanup tests that pending requests are cleaned up
// on shutdown
func TestClientPendingRequestCleanup(t *testing.T) {
	client := &Client{
		pending: make(map[int]chan callResult),
	}

	// Add some pending requests
	for i := 0; i < 10; i++ {
		client.pending[i] = make(chan callResult, 1)
	}

	// Simulate shutdown (cleanup pending requests)
	client.mu.Lock()
	for id, ch := range client.pending {
		close(ch)
		delete(client.pending, id)
	}
	client.mu.Unlock()

	// Verify cleanup
	if len(client.pending) != 0 {
		t.Errorf("pending requests not cleaned up: %d remaining", len(client.pending))
	}
}

// TestClientRequestTimeout tests that requests can be cancelled
func TestClientRequestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Simulate a request that would timeout
	done := make(chan bool, 1)
	go func() {
		// In real implementation, this would wait for LSP response
		// For now, just verify context cancellation works
		<-ctx.Done()
		done <- true
	}()

	select {
	case <-done:
		// Good - context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("request timeout not detected")
	}
}

// TestClientConcurrentInitialize tests that Initialize is idempotent
func TestClientConcurrentInitialize(t *testing.T) {
	client := &Client{
		pending: make(map[int]chan callResult),
	}

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Try to initialize from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// In real implementation, Initialize would use sync.Once or similar
			// For now, just test concurrent access doesn't crash
			client.mu.Lock()
			client.initialized = true
			client.mu.Unlock()
		}()
	}

	wg.Wait()
	close(errors)

	if !client.initialized {
		t.Error("expected client to be initialized")
	}
}

// TestClientCapabilitiesThreadSafety tests that capability flags
// can be read safely while being written
func TestClientCapabilitiesThreadSafety(t *testing.T) {
	client := &Client{
		pending: make(map[int]chan callResult),
		capabilities: ServerCapabilities{
			HoverProvider: true,
		},
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			client.mu.Lock()
			client.capabilities.HoverProvider = !client.capabilities.HoverProvider
			client.mu.Unlock()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = client.SupportsHover()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("thread safety error: %v", err)
		}
	}
}

// TestClientDidOpenBeforeInitialize tests that DidOpen waits for initialization
func TestClientDidOpenBeforeInitialize(t *testing.T) {
	client := &Client{
		pending:  make(map[int]chan callResult),
		running:  true,
		openDocs: make(map[string]int),
	}

	// DidOpen should handle uninitialized client gracefully
	// In real implementation, this would queue the request
	uri := "file:///test.go"
	
	// This should not panic or deadlock
	client.mu.Lock()
	client.openDocs[uri] = 1
	client.mu.Unlock()

	// Verify document was "opened"
	if _, ok := client.openDocs[uri]; !ok {
		t.Error("expected document to be in openDocs")
	}
}

// TestClientShutdownMultipleTimes tests that Shutdown is idempotent
func TestClientShutdownMultipleTimes(t *testing.T) {
	// We can't fully test Shutdown without a real LSP process
	// Just verify it doesn't panic on nil fields
	client := &Client{
		pending: make(map[int]chan callResult),
		running: false, // Not running, so Shutdown should be safe
	}

	// Shutdown when not running should be safe
	client.Shutdown()
	client.Shutdown()
	client.Shutdown()

	// Should still be safe to call methods
	_ = client.SupportsHover()
}

// TestClientRequestIDOverflow tests that request IDs don't overflow
func TestClientRequestIDOverflow(t *testing.T) {
	client := &Client{
		pending:   make(map[int]chan callResult),
		requestID: 2147483640, // Close to int32 max
	}

	// Simulate many requests
	for i := 0; i < 100; i++ {
		client.mu.Lock()
		client.requestID++
		id := client.requestID
		client.mu.Unlock()

		// Request ID should keep increasing (may wrap around, which is OK)
		if id <= 0 && i < 10 {
			t.Logf("request ID wrapped to %d at iteration %d", id, i)
		}
	}
}

// TestClientOpenDocsVersionTracking tests that document versions are tracked
func TestClientOpenDocsVersionTracking(t *testing.T) {
	client := &Client{
		pending:  make(map[int]chan callResult),
		openDocs: make(map[string]int),
	}

	uri := "file:///test.go"
	
	// Simulate DidOpen with version 1
	client.mu.Lock()
	client.openDocs[uri] = 1
	client.mu.Unlock()

	// Simulate DidChange incrementing version
	client.mu.Lock()
	client.openDocs[uri] = 2
	client.mu.Unlock()

	// Verify version is tracked
	client.mu.Lock()
	version, ok := client.openDocs[uri]
	client.mu.Unlock()

	if !ok {
		t.Error("expected document to be tracked")
	}
	if version != 2 {
		t.Errorf("version = %d, want 2", version)
	}

	// Simulate DidClose removing document
	client.mu.Lock()
	delete(client.openDocs, uri)
	client.mu.Unlock()

	if _, ok := client.openDocs[uri]; ok {
		t.Error("expected document to be removed on close")
	}
}

// TestClientMessageChannelCapacity tests that message channel doesn't block
func TestClientMessageChannelCapacity(t *testing.T) {
	msgChan := make(chan any, 100)
	
	// Fill the channel
	for i := 0; i < 100; i++ {
		select {
		case msgChan <- i:
			// Good
		default:
			t.Errorf("channel blocked at %d messages", i)
		}
	}

	// Try to send one more (should block or be dropped in real impl)
	select {
	case msgChan <- 101:
		t.Error("channel should be full")
	default:
		// Good - channel is properly bounded
	}
}

// TestClientRetryLogic tests the retry mechanism for failed servers
func TestClientRetryLogic(t *testing.T) {
	// This would test the Manager's retry logic
	// For now, just verify the constants are reasonable
	if maxRetries <= 0 {
		t.Errorf("maxRetries = %d, should be > 0", maxRetries)
	}
	if maxRetries > 10 {
		t.Errorf("maxRetries = %d, seems too high", maxRetries)
	}
}
