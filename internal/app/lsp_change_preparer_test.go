package app

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
	"teak/internal/text"
	"teak/internal/toolpath"
)

type lspChangeDispatchResultMsg struct{}

func TestLSPChangePreparerReturnsDispatchFailureMessage(t *testing.T) {
	preparer := newLSPChangePreparer()
	want := lspChangeDispatchResultMsg{}
	cmd := preparer.queue(lspChangePayload{
		path:     "/workspace/retry.go",
		version:  1,
		snapshot: text.NewFromString("package retry\n"),
		dispatch: func(string) tea.Msg { return want },
	})
	if cmd == nil {
		t.Fatal("queue() command = nil")
	}
	if got := cmd(); got != want {
		t.Fatalf("queue() message = %#v, want %#v", got, want)
	}
}

func TestEnsureFormattingDocumentSnapshotReturnsOpenError(t *testing.T) {
	root := t.TempDir()
	path := root + "/format.lint-open-error"
	mgr := lsp.NewManager(root, []lsp.ServerConfig{{
		Extensions: []string{".lint-open-error"},
		Command:    "teak-lsp-that-does-not-exist",
		LanguageID: "lint-open-error",
	}})
	mgr.BeginDocument(path)

	synced, err := ensureFormattingDocumentSnapshot(
		mgr,
		path,
		1,
		text.NewFromString("format me\n"),
		&lsp.Client{},
	)
	if synced {
		t.Fatal("ensureFormattingDocumentSnapshot() synced = true after didOpen failed")
	}
	if err == nil {
		t.Fatal("ensureFormattingDocumentSnapshot() error = nil after didOpen failed")
	}
	if !toolpath.IsMissing(err) {
		t.Fatalf("ensureFormattingDocumentSnapshot() error = %T %v, want missing-tool startup error", err, err)
	}
}

func TestLSPChangePreparerLatestWinsAndMaterializesOneSnapshotAtATime(t *testing.T) {
	p := newLSPChangePreparer()

	started := make(chan string, 2)
	release := make(chan struct{})
	finished := make(chan struct{})
	var active, maxActive atomic.Int32
	p.materialize = func(snapshot *text.Rope) string {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		content := snapshot.String()
		started <- content
		<-release
		active.Add(-1)
		return content
	}

	var mu sync.Mutex
	var sent []string
	makePayload := func(version int, content string) lspChangePayload {
		return lspChangePayload{
			path:     "/workspace/main.go",
			version:  version,
			snapshot: text.NewFromString(content),
			dispatch: func(content string) tea.Msg {
				mu.Lock()
				sent = append(sent, content)
				mu.Unlock()
				return nil
			},
		}
	}

	first := p.queue(makePayload(1, "one"))
	go func() {
		first()
		close(finished)
	}()

	select {
	case got := <-started:
		if got != "one" {
			t.Fatalf("first materialization = %q, want one", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first snapshot was not materialized")
	}

	// These represent two edits arriving while the first full-text snapshot is
	// being prepared. The old result must be thrown away, and only v3 may be
	// dispatched to the LSP queue.
	p.queue(makePayload(2, "two"))
	p.queue(makePayload(3, "three"))
	release <- struct{}{}

	select {
	case got := <-started:
		if got != "three" {
			t.Fatalf("second materialization = %q, want newest snapshot three", got)
		}
	case <-time.After(time.Second):
		t.Fatal("newest snapshot was not materialized")
	}
	release <- struct{}{}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("preparer did not finish")
	}

	if got := maxActive.Load(); got != 1 {
		t.Fatalf("concurrent materializations = %d, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "three" {
		t.Fatalf("sent = %#v, want only newest snapshot", sent)
	}
}

func TestLSPChangePreparerCancelDiscardsActiveSnapshot(t *testing.T) {
	p := newLSPChangePreparer()
	started := make(chan struct{})
	release := make(chan struct{})
	p.materialize = func(snapshot *text.Rope) string {
		close(started)
		<-release
		return snapshot.String()
	}

	var dispatched atomic.Int32
	cmd := p.queue(lspChangePayload{
		path:     "/workspace/main.go",
		version:  1,
		snapshot: text.NewFromString("content"),
		dispatch: func(string) tea.Msg {
			dispatched.Add(1)
			return nil
		},
	})
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("snapshot was not materialized")
	}

	p.cancel("/workspace/main.go")
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled preparer did not finish")
	}
	if got := dispatched.Load(); got != 0 {
		t.Fatalf("cancelled snapshot dispatched %d times, want 0", got)
	}
}

func TestLSPChangePreparerBurstRetainsAndDispatchesOnlyNewestPendingSnapshot(t *testing.T) {
	p := newLSPChangePreparer()
	var materialized []string
	p.materialize = func(snapshot *text.Rope) string {
		content := snapshot.String()
		materialized = append(materialized, content)
		return content
	}
	var dispatched []string

	var first func() tea.Msg
	for version := 1; version <= 128; version++ {
		content := fmt.Sprintf("version-%d", version)
		cmd := p.queue(lspChangePayload{
			path:     "/workspace/main.go",
			version:  version,
			snapshot: text.NewFromString(content),
			dispatch: func(content string) tea.Msg {
				dispatched = append(dispatched, content)
				return nil
			},
		})
		if version == 1 {
			first = cmd
		}
	}
	if first == nil {
		t.Fatal("first command was not queued")
	}
	first()

	if got, want := materialized, []string{"version-128"}; !slices.Equal(got, want) {
		t.Fatalf("materialized = %#v, want %#v", got, want)
	}
	if got, want := dispatched, []string{"version-128"}; !slices.Equal(got, want) {
		t.Fatalf("dispatched = %#v, want %#v", got, want)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.documents) != 0 {
		t.Fatalf("completed burst retained %d document entries", len(p.documents))
	}
}

func TestLSPChangePreparerRejectsDelayedOlderVersion(t *testing.T) {
	p := newLSPChangePreparer()
	var sent []string
	newPayload := func(version int, content string) lspChangePayload {
		return lspChangePayload{
			path:     "/workspace/main.go",
			version:  version,
			snapshot: text.NewFromString(content),
			dispatch: func(content string) tea.Msg {
				sent = append(sent, content)
				return nil
			},
		}
	}
	newest := p.queue(newPayload(9, "newest"))
	if stale := p.queue(newPayload(8, "stale")); stale != nil {
		t.Fatal("older version returned a command")
	}
	newest()
	if got, want := sent, []string{"newest"}; !slices.Equal(got, want) {
		t.Fatalf("sent = %#v, want %#v", got, want)
	}
}

func TestLSPMaterializationBudgetCapsAllOutboundRoutes(t *testing.T) {
	budget := newLSPMaterializationBudget(maxConcurrentLSPMaterializations)
	started := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	var active, maxActive atomic.Int32
	budget.materialize = func(snapshot *text.Rope) string {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return snapshot.String()
	}

	preparer := newLSPChangePreparerWithBudget(budget)
	mgr := lsp.NewManager(t.TempDir(), nil)
	m := testModel(modelState{lspMgr: mgr, lspChanges: preparer})

	preparerCmd := preparer.queue(lspChangePayload{
		path:     "/workspace/prepared.unknown",
		version:  1,
		snapshot: text.NewFromString("prepared"),
		dispatch: func(string) tea.Msg { return nil },
	})
	buffer := text.NewBuffer()
	buffer.FilePath = "/workspace/open.unknown"
	buffer.InsertAtCursor([]byte("open"))
	openCmd := m.lspDidOpen(buffer)

	legacyPath := "/workspace/legacy.unknown"
	mgr.BeginDocument(legacyPath)
	legacyCmd := lspDidChangeCmd(mgr, legacyPath, 1, text.NewFromString("legacy"), budget)

	var wg sync.WaitGroup
	for _, cmd := range []tea.Cmd{preparerCmd, openCmd, legacyCmd} {
		wg.Add(1)
		go func(cmd tea.Cmd) {
			defer wg.Done()
			if cmd != nil {
				cmd()
			}
		}(cmd)
	}
	// Formatting takes the same gate even though its lightweight zero client
	// will reject the eventual no-server notification after materialization.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = ensureFormattingDocumentSnapshot(nil, "/workspace/format.unknown", 1, text.NewFromString("format"), &lsp.Client{}, budget)
	}()

	waitForMaterialization := func() {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("outbound routes did not enter shared materializer")
		}
	}
	for range maxConcurrentLSPMaterializations {
		waitForMaterialization()
	}
	if got := maxActive.Load(); got > maxConcurrentLSPMaterializations {
		t.Fatalf("concurrent LSP materializations = %d, limit = %d", got, maxConcurrentLSPMaterializations)
	}

	// Admit the remaining two operations one at a time. Observing all four
	// proves didOpen, legacy didChange and formatting use the same gate rather
	// than merely running concurrently beside the preparer.
	for range 2 {
		release <- struct{}{}
		waitForMaterialization()
	}
	for range maxConcurrentLSPMaterializations {
		release <- struct{}{}
	}
	wg.Wait()
	if got := maxActive.Load(); got > maxConcurrentLSPMaterializations {
		t.Fatalf("concurrent LSP materializations after drain = %d, limit = %d", got, maxConcurrentLSPMaterializations)
	}
}

func TestLSPChangePreparerCloseCancelsWhileWaitingForGlobalSlot(t *testing.T) {
	budget := newLSPMaterializationBudget(1)
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	budget.materialize = func(snapshot *text.Rope) string {
		close(blockerStarted)
		<-releaseBlocker
		return snapshot.String()
	}

	blockerDone := make(chan struct{})
	go func() {
		defer close(blockerDone)
		budget.materializeSnapshot(text.NewFromString("blocker"), nil)
	}()
	select {
	case <-blockerStarted:
	case <-time.After(time.Second):
		t.Fatal("blocker did not acquire global materialization slot")
	}

	preparer := newLSPChangePreparerWithBudget(budget)
	var dispatched atomic.Int32
	cmd := preparer.queue(lspChangePayload{
		path:     "/workspace/closed.unknown",
		version:  1,
		snapshot: text.NewFromString("must-not-flatten"),
		dispatch: func(string) tea.Msg {
			dispatched.Add(1)
			return nil
		},
	})
	if cmd == nil {
		t.Fatal("queue returned nil command")
	}
	completed := make(chan struct{})
	go func() { cmd(); close(completed) }()

	// This is the same close path used by tab teardown. It must unblock a
	// waiting command without waiting for another document's Rope.String.
	m := testModel(modelState{lspChanges: preparer})
	m.cancelLSPChangePreparation("/workspace/closed.unknown")
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("close did not cancel queued materialization")
	}
	if got := dispatched.Load(); got != 0 {
		t.Fatalf("closed document dispatched %d changes", got)
	}

	close(releaseBlocker)
	select {
	case <-blockerDone:
	case <-time.After(time.Second):
		t.Fatal("blocker did not finish")
	}
	preparer.mu.Lock()
	defer preparer.mu.Unlock()
	if len(preparer.documents) != 0 {
		t.Fatalf("close retained %d preparer entries", len(preparer.documents))
	}
}
