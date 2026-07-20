package acp

import (
	"context"
	"errors"
	"testing"
)

func TestPromptLifecycleCancelsCurrentPromptAndRejectsConcurrentPrompt(t *testing.T) {
	mgr := NewManager(t.TempDir(), "echo", nil)
	mgr.running = true

	ctx, generation, err := mgr.beginPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.beginPrompt(); err == nil {
		t.Fatal("beginPrompt() allowed concurrent prompts")
	}
	mgr.Cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("prompt context error = %v, want context.Canceled", ctx.Err())
	}
	mgr.finishPrompt(generation)
	_, nextGeneration, err := mgr.beginPrompt()
	if err != nil {
		t.Fatalf("beginPrompt() after completion error = %v", err)
	}
	if mgr.IsCurrentPromptGeneration(generation) {
		t.Fatal("older prompt generation was still accepted")
	}
	if !mgr.IsCurrentPromptGeneration(nextGeneration) {
		t.Fatal("newest prompt generation was rejected")
	}
}
