package app

import (
	"testing"

	"teak/internal/codemap"
)

func TestHandleCodemapResultDropsStaleGeneration(t *testing.T) {
	model := Model{modelState: &modelState{codemapGeneration: 2, status: "current"}}
	updatedAny, cmd := model.handleCodemapResult(codemapResultMsg{
		generation: 1,
		kind:       "callers",
		symbols:    []codemap.Symbol{{Symbol: "old"}},
	})
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatal("stale codemap result returned a command")
	}
	if updated.status != "current" {
		t.Fatalf("stale codemap result changed status to %q", updated.status)
	}
}

func TestStartCodemapQueryCancelsPreviousRequest(t *testing.T) {
	cancelled := false
	model := Model{modelState: &modelState{
		codemapGeneration: 4,
		codemapCancel:     func() { cancelled = true },
	}}

	updated, cmd := model.startCodemapQuery("callers")
	if cmd != nil {
		t.Fatal("startCodemapQuery() returned a command without an active editor")
	}
	if !cancelled {
		t.Fatal("startCodemapQuery() did not cancel the previous request")
	}
	if updated.codemapGeneration != 5 {
		t.Fatalf("codemap generation = %d, want 5", updated.codemapGeneration)
	}
	if updated.codemapCancel != nil {
		t.Fatal("startCodemapQuery() retained a cancellation handle without an active editor")
	}
}
