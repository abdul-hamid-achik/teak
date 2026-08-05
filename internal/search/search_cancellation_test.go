package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"teak/internal/ui"
)

func TestTextSearchContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := TextSearchContext(ctx, t.TempDir(), "needle", SearchOpts{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TextSearchContext() error = %v, want context.Canceled", err)
	}
}

func TestSemanticSetupWaiterCanCancel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	leaderDone := make(chan error, 1)
	go func() {
		leaderDone <- runSemanticSetup("cancel-waiter", func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	begin := time.Now()
	err := runSemanticSetupContext(ctx, "cancel-waiter", func(context.Context) error {
		t.Fatal("waiter unexpectedly became setup leader")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runSemanticSetupContext() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled waiter returned after %s", elapsed)
	}

	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader error = %v", err)
	}
}

func TestSemanticSetupWaiterDoesNotRestartStoppedIndexBuild(t *testing.T) {
	rootDir := "stopped-index-build"
	key := workspaceKey(rootDir)
	flight := &semanticSetupFlight{
		done: make(chan struct{}),
		err:  errors.Join(errSemanticIndexBuildStopped, context.Canceled),
	}
	close(flight.done)
	semanticSetupFlights.mu.Lock()
	semanticSetupFlights.flights[key] = flight
	semanticSetupFlights.mu.Unlock()
	t.Cleanup(func() {
		semanticSetupFlights.mu.Lock()
		if semanticSetupFlights.flights[key] == flight {
			delete(semanticSetupFlights.flights, key)
		}
		semanticSetupFlights.mu.Unlock()
	})

	// A waiter keeps the flight pointer it observed before the leader removes
	// the map entry and closes done. Seed that exact observable state directly
	// so this regression test has no scheduler or sleep dependency.
	err := runSemanticSetupContext(context.Background(), rootDir, func(context.Context) error {
		t.Fatal("waiter restarted a stopped index build")
		return nil
	})
	if !errors.Is(err, errSemanticIndexBuildStopped) {
		t.Fatalf("runSemanticSetupContext() error = %v, want stopped index build", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runSemanticSetupContext() error = %v, want cancellation cause preserved", err)
	}
}

func TestModelCancelsInFlightSearchWhenQueryChanges(t *testing.T) {
	model := New(ui.DefaultTheme(), t.TempDir(), ModeText)
	model.input.SetValue("old")
	model.lastQuery = "old"
	ctx, cancel := context.WithCancel(context.Background())
	model.searchContext = ctx
	model.searchCancel = cancel

	model.input.SetValue("new")
	updated, _ := model.Update(struct{}{})
	select {
	case <-ctx.Done():
	default:
		t.Fatal("query change did not cancel the previous search")
	}
	if updated.searchContext == nil || updated.searchContext == ctx {
		t.Fatal("query change did not create a replacement search context")
	}
}

func TestModelCancelStopsAndInvalidatesInFlightSearch(t *testing.T) {
	model := New(ui.DefaultTheme(), t.TempDir(), ModeSemantic)
	model.input.SetValue("meaning")
	model.searching = true
	model.indexing = true
	model.debounceGen = 7
	ctx, cancel := context.WithCancel(context.Background())
	model.searchContext = ctx
	model.searchCancel = cancel

	model.Cancel()

	select {
	case <-ctx.Done():
	default:
		t.Fatal("Cancel did not stop the active search context")
	}
	if model.searchContext != nil || model.searchCancel != nil {
		t.Fatal("Cancel retained the completed search context")
	}
	if model.searching || model.indexing {
		t.Fatalf("Cancel left activity flags set: searching=%t indexing=%t", model.searching, model.indexing)
	}
	if model.debounceGen != 8 {
		t.Fatalf("Cancel generation = %d, want 8 so late results are stale", model.debounceGen)
	}

	// Teardown paths may converge on cancellation; repeated calls must be safe
	// and must not keep changing the generation once there is no active work.
	model.Cancel()
	if model.debounceGen != 8 {
		t.Fatalf("second Cancel generation = %d, want idempotent 8", model.debounceGen)
	}
}
