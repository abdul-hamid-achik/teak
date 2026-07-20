package search

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSemanticSetupSingleFlightPerWorkspace(t *testing.T) {
	var calls atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})

	setup := func() error {
		calls.Add(1)
		close(start)
		<-release
		return nil
	}

	const callers = 12
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- runSemanticSetup("single-flight-test", setup)
		}()
	}

	select {
	case <-start:
	case <-time.After(time.Second):
		t.Fatal("semantic setup did not begin")
	}
	// Give the remaining callers a chance to join the in-flight setup before
	// releasing it. A second invocation means concurrent indexing was allowed.
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("setup calls while in flight = %d, want 1", got)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("runSemanticSetup() error = %v", err)
		}
	}
}
