package app

import (
	"sync"

	"teak/internal/text"
)

// maxConcurrentLSPMaterializations bounds the transient full-text copies made
// while synchronizing immutable ropes with language servers. A full sync can
// briefly require one additional copy of a large document; keeping this small
// makes opening or editing many large tabs predictable rather than multiplying
// peak memory by the number of tabs.
const maxConcurrentLSPMaterializations = 2

// lspMaterializationBudget is shared by every Rope -> string path that feeds
// the LSP transport. Acquiring happens only inside tea.Cmd functions, never in
// Update. The materialize hook is intentionally testable so concurrency tests
// can block the expensive operation without constructing huge ropes.
type lspMaterializationBudget struct {
	slots       chan struct{}
	materialize func(*text.Rope) string
	once        sync.Once
}

func newLSPMaterializationBudget(limit int) *lspMaterializationBudget {
	if limit < 1 {
		limit = 1
	}
	return &lspMaterializationBudget{
		slots:       make(chan struct{}, limit),
		materialize: func(snapshot *text.Rope) string { return snapshot.String() },
	}
}

// sharedLSPMaterializations deliberately has process scope. A Model owns its
// coordinators, but multiple Models can coexist in tests or embedding hosts;
// a per-model semaphore would let those models still produce an unbounded
// number of simultaneous full-text copies.
var sharedLSPMaterializations = newLSPMaterializationBudget(maxConcurrentLSPMaterializations)

func lspMaterializationsFor(budget *lspMaterializationBudget) *lspMaterializationBudget {
	if budget == nil {
		return sharedLSPMaterializations
	}
	budget.once.Do(func() {
		if budget.slots == nil {
			budget.slots = make(chan struct{}, 1)
		}
		if budget.materialize == nil {
			budget.materialize = func(snapshot *text.Rope) string { return snapshot.String() }
		}
	})
	return budget
}

// materializeSnapshot waits for a global slot and then flattens snapshot. If
// cancelled is closed while waiting, it returns false without retaining the
// snapshot or consuming a slot. Cancellation after String starts is necessarily
// cooperative at the caller: Rope.String itself is an in-memory operation.
func (b *lspMaterializationBudget) materializeSnapshot(snapshot *text.Rope, cancelled <-chan struct{}) (string, bool) {
	return b.materializeSnapshotIf(snapshot, cancelled, nil)
}

// materializeSnapshotIf avoids flattening a snapshot that became obsolete
// while it waited for the global budget. valid must be a cheap immutable
// lifecycle check; it is called outside Update and before/after acquisition.
func (b *lspMaterializationBudget) materializeSnapshotIf(snapshot *text.Rope, cancelled <-chan struct{}, valid func() bool) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	if valid != nil && !valid() {
		return "", false
	}
	b = lspMaterializationsFor(b)
	if !b.acquire(cancelled) {
		return "", false
	}
	defer b.release()

	// Do not flatten a stale snapshot that lost its race with cancellation at
	// the exact instant a slot became available.
	if cancelled != nil {
		select {
		case <-cancelled:
			return "", false
		default:
		}
	}
	if valid != nil && !valid() {
		return "", false
	}
	return b.materialize(snapshot), true
}

func (b *lspMaterializationBudget) acquire(cancelled <-chan struct{}) bool {
	b = lspMaterializationsFor(b)
	if cancelled == nil {
		b.slots <- struct{}{}
		return true
	}
	select {
	case b.slots <- struct{}{}:
		return true
	case <-cancelled:
		return false
	}
}

func (b *lspMaterializationBudget) release() {
	b = lspMaterializationsFor(b)
	<-b.slots
}
