package app

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
)

// lspChangePayload is a complete, immutable description of one outbound LSP
// document update. It intentionally contains no Editor or Model pointer:
// Bubble Tea may execute the command after either object has advanced to a
// newer edit or been closed. The dispatch function is built only from the
// manager/client and scalar protocol metadata captured at scheduling time.
type lspChangePayload struct {
	path     string
	version  int
	snapshot *text.Rope
	dispatch func(string)

	sequence uint64 // assigned by lspChangePreparer while holding its mutex
}

type lspChangePrepareDocument struct {
	sequence      uint64
	latestVersion int
	hasVersion    bool
	pending       *lspChangePayload
	active        bool
	waitCancel    chan struct{} // closes only while an active worker waits for a global slot
}

// lspChangePreparer serializes expensive Rope.String calls per document.
// Language-server outbound queues already coalesce writes, but preparing a
// full replacement first used to let every fast keystroke concurrently flatten
// the same large rope. This layer keeps just the newest immutable snapshot and
// discards a materialized result if a later edit arrived while it was running.
//
// Different documents use a small process-wide materialization budget. That
// retains responsiveness across tabs without allowing every large open buffer
// to allocate a full string copy simultaneously.
type lspChangePreparer struct {
	mu        sync.Mutex
	documents map[string]*lspChangePrepareDocument
	budget    *lspMaterializationBudget
	// materialize is a focused test hook. Production uses budget.materialize so
	// every LSP path shares the same concurrency gate.
	materialize func(*text.Rope) string
}

func newLSPChangePreparer() *lspChangePreparer {
	return newLSPChangePreparerWithBudget(sharedLSPMaterializations)
}

func newLSPChangePreparerWithBudget(budget *lspMaterializationBudget) *lspChangePreparer {
	return &lspChangePreparer{
		documents: make(map[string]*lspChangePrepareDocument),
		budget:    lspMaterializationsFor(budget),
	}
}

// queue records payload as the latest desired synchronization point for its
// path. The returned command never captures a live editor or model; it only
// holds this independent coordinator and the path key.
func (p *lspChangePreparer) queue(payload lspChangePayload) tea.Cmd {
	if p == nil || payload.path == "" || payload.snapshot == nil || payload.dispatch == nil {
		return nil
	}

	p.mu.Lock()
	if p.documents == nil {
		p.documents = make(map[string]*lspChangePrepareDocument)
	}
	doc := p.documents[payload.path]
	if doc == nil {
		doc = &lspChangePrepareDocument{}
		p.documents[payload.path] = doc
	}
	// Update normally runs in version order, but tea.Cmd scheduling itself is
	// concurrent. Do not let a delayed, older command replace an already
	// captured newer document; Client has the same guard at the protocol edge.
	if doc.hasVersion && payload.version <= doc.latestVersion {
		p.mu.Unlock()
		return nil
	}
	doc.sequence++
	doc.latestVersion = payload.version
	doc.hasVersion = true
	payload.sequence = doc.sequence
	// Replacing pending drops the only coordinator reference to superseded rope
	// snapshots immediately. An in-flight snapshot cannot be interrupted, but
	// its result is discarded below before it reaches the LSP client.
	doc.pending = &payload
	// If an older worker is only queued behind another document's large
	// conversion, wake it now. It will select the newer pending snapshot before
	// it can allocate its own string copy.
	doc.cancelWaitingMaterialization()
	p.mu.Unlock()

	path := payload.path
	return func() tea.Msg {
		p.run(path)
		return nil
	}
}

// cancel invalidates both a queued payload and an in-flight materialization.
// It is used when a document lifecycle closes. Rope.String itself is not
// cancellable, so an already running call is allowed to finish but can no
// longer enqueue a stale protocol message or retain a pending successor.
func (p *lspChangePreparer) cancel(path string) {
	if p == nil || path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	doc := p.documents[path]
	if doc == nil {
		return
	}
	doc.sequence++
	doc.pending = nil
	doc.cancelWaitingMaterialization()
	if !doc.active {
		delete(p.documents, path)
	}
}

func (d *lspChangePrepareDocument) cancelWaitingMaterialization() {
	if d.waitCancel == nil {
		return
	}
	close(d.waitCancel)
	d.waitCancel = nil
}

func (p *lspChangePreparer) run(path string) {
	if p == nil {
		return
	}

	p.mu.Lock()
	doc := p.documents[path]
	if doc == nil || doc.active || doc.pending == nil {
		p.mu.Unlock()
		return
	}
	doc.active = true
	work := *doc.pending
	doc.pending = nil
	waitCancel := make(chan struct{})
	doc.waitCancel = waitCancel
	p.mu.Unlock()

	for {
		budget := lspMaterializationsFor(p.budget)
		if !budget.acquire(waitCancel) {
			p.mu.Lock()
			if doc.sequence != work.sequence && doc.pending != nil {
				work = *doc.pending
				doc.pending = nil
				waitCancel = make(chan struct{})
				doc.waitCancel = waitCancel
				p.mu.Unlock()
				continue
			}
			doc.active = false
			if p.documents[path] == doc {
				delete(p.documents, path)
			}
			p.mu.Unlock()
			return
		}

		// A new change can arrive in the tiny window after acquire succeeds but
		// before Rope.String begins. Do not flatten the stale rope in that case.
		select {
		case <-waitCancel:
			budget.release()
			p.mu.Lock()
			if doc.pending != nil {
				work = *doc.pending
				doc.pending = nil
				waitCancel = make(chan struct{})
				doc.waitCancel = waitCancel
				p.mu.Unlock()
				continue
			}
			doc.active = false
			if p.documents[path] == doc {
				delete(p.documents, path)
			}
			p.mu.Unlock()
			return
		default:
		}

		materialize := p.materialize
		if materialize == nil {
			materialize = budget.materialize
		}
		content := materialize(work.snapshot)
		budget.release()

		p.mu.Lock()
		// A newer edit (or a close) arrived while String was allocating. Never
		// send this outdated content. Retain exclusive ownership of the document
		// while moving directly to its latest pending payload, which is what
		// enforces the one-materialization-per-document bound.
		if doc.sequence != work.sequence {
			if doc.pending == nil {
				doc.active = false
				if p.documents[path] == doc {
					delete(p.documents, path)
				}
				p.mu.Unlock()
				return
			}
			work = *doc.pending
			doc.pending = nil
			waitCancel = make(chan struct{})
			doc.waitCancel = waitCancel
			p.mu.Unlock()
			continue
		}
		doc.waitCancel = nil
		p.mu.Unlock()

		// Manager and Client independently re-check document lifecycle,
		// suppression and monotonic versions immediately before queuing stdin.
		// This call is deliberately outside the preparer's mutex so another tab
		// can submit its latest snapshot without blocking on JSON encoding.
		work.dispatch(content)

		p.mu.Lock()
		if doc.pending == nil {
			doc.active = false
			if p.documents[path] == doc {
				delete(p.documents, path)
			}
			p.mu.Unlock()
			return
		}
		work = *doc.pending
		doc.pending = nil
		waitCancel = make(chan struct{})
		doc.waitCancel = waitCancel
		p.mu.Unlock()
	}
}
