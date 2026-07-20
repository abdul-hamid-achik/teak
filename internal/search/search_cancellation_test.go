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
