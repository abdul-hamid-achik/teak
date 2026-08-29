package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
)

// armStatusExpiry pumps a user interaction so the wrapper schedules the
// expiry tick for the status that is currently displayed. Arming is tied to
// the next interaction by design; a mouse motion over chrome neither edits
// the buffer nor schedules commands of its own.
func armStatusExpiry(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updatedAny, cmd := m.Update(tea.MouseMotionMsg(tea.Mouse{X: 1, Y: 1}))
	updated, ok := updatedAny.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updatedAny)
	}
	return updated, cmd
}

// setStatusViaUpdate drives a status change through the public Update entry
// point so the expiry wrapper runs.
func setStatusViaUpdate(t *testing.T, m Model, msg lsp.LspErrorMsg) Model {
	t.Helper()
	updatedAny, _ := m.Update(msg)
	updated, ok := updatedAny.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updatedAny)
	}
	if updated.status == "" {
		t.Fatal("status message was not set")
	}
	return updated
}

func TestStatusMessageExpiresAfterLifetime(t *testing.T) {
	origLifetime := statusMessageLifetime
	statusMessageLifetime = time.Millisecond
	t.Cleanup(func() { statusMessageLifetime = origLifetime })

	m := newInputRoutingTestModel(t)
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "textDocument/hover", Message: "boom"})
	seq := m.statusSeq

	m2, cmd := armStatusExpiry(t, m)
	if cmd == nil {
		t.Fatal("status expiry timer was not scheduled on the following message")
	}
	fired, ok := cmd().(statusExpiredMsg)
	if !ok {
		t.Fatalf("expiry command produced %T, want statusExpiredMsg", cmd())
	}
	if fired.seq != seq {
		t.Fatalf("expiry seq = %d, want %d", fired.seq, seq)
	}

	updatedAny, _ := m2.Update(fired)
	m = updatedAny.(Model)
	if m.status != "" {
		t.Fatalf("status = %q after expiry; want it cleared so the hover diagnostic can reclaim the slot", m.status)
	}
	if m.statusTimerArmed {
		t.Fatal("timer still marked armed after clearing the status")
	}
}

func TestNewerStatusNotClearedByOlderTimer(t *testing.T) {
	origLifetime := statusMessageLifetime
	statusMessageLifetime = time.Millisecond
	t.Cleanup(func() { statusMessageLifetime = origLifetime })

	m := newInputRoutingTestModel(t)
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "a", Message: "first"})
	firstSeq := m.statusSeq
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "b", Message: "second"})
	if m.statusSeq == firstSeq {
		t.Fatal("setting a second status did not advance the expiry sequence")
	}

	// The older timer fires late: it must not clear the newer message.
	updatedAny, _ := m.Update(statusExpiredMsg{seq: firstSeq})
	m = updatedAny.(Model)
	if m.status != "LSP error [b]: second (code 0)" {
		t.Fatalf("stale timer cleared or altered the newer status: %q", m.status)
	}

	// The newer timer then clears it.
	updatedAny, _ = m.Update(statusExpiredMsg{seq: m.statusSeq})
	m = updatedAny.(Model)
	if m.status != "" {
		t.Fatalf("status = %q after its own expiry timer fired", m.status)
	}
}

// A stale fire must leave the newer status alone, and the next interaction
// re-arms expiry for it instead of leaving it on screen forever.
func TestStaleTimerRearmsForCurrentStatus(t *testing.T) {
	origLifetime := statusMessageLifetime
	statusMessageLifetime = time.Millisecond
	t.Cleanup(func() { statusMessageLifetime = origLifetime })

	m := newInputRoutingTestModel(t)
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "a", Message: "first"})
	firstSeq := m.statusSeq
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "b", Message: "second"})

	updatedAny, cmd := m.Update(statusExpiredMsg{seq: firstSeq})
	m = updatedAny.(Model)
	if m.status == "" {
		t.Fatal("stale timer cleared the newer status")
	}
	if cmd != nil {
		t.Fatalf("stale fire scheduled %T; re-arming waits for the next interaction", cmd)
	}

	m, cmd = armStatusExpiry(t, m)
	if cmd == nil {
		t.Fatal("next interaction did not re-arm expiry for the current status")
	}
	if fired, ok := cmd().(statusExpiredMsg); !ok || fired.seq != m.statusSeq {
		t.Fatalf("re-armed command = %#v, want statusExpiredMsg for seq %d", cmd(), m.statusSeq)
	}

	updatedAny, _ = m.Update(statusExpiredMsg{seq: m.statusSeq})
	m = updatedAny.(Model)
	if m.status != "" {
		t.Fatalf("status = %q after the re-armed timer fired", m.status)
	}
}

// The status set during construction (config load warnings) is delivered
// through Init; it must carry an expiry tick too.
func TestStartupStatusExpiryScheduled(t *testing.T) {
	m := newInputRoutingTestModel(t)
	if cmd := m.startupStatusExpiryCmd(); cmd != nil {
		t.Fatal("startup expiry scheduled without a status message")
	}

	m.status = "Config warnings: fixture"
	m.statusSeq++
	cmd := m.startupStatusExpiryCmd()
	if cmd == nil {
		t.Fatal("startup status did not get an expiry tick")
	}
}

// collectCmdMsgs executes cmd and flattens any tea.BatchMsg tree into the
// concrete messages its members produce, so a test can assert that no
// member secretly carries the expiry tick.
func collectCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, member := range batch {
			msgs = append(msgs, collectCmdMsgs(member)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

// TestStatusExpiryNeverWrapsANonNilCmd pins the wrapper's core invariant: an
// interaction whose inner cmd is non-nil must return that cmd untouched —
// never tea.Batch(cmd, tick). Batching outside the runtime returns a BatchMsg
// whose members never execute (they deadlocked the suite under teatest), so
// arming is deferred to the next nil-cmd interaction instead. A naive
// unconditional batch passes TestStatusMessageExpiresAfterLifetime (Batch of
// nil,tick compacts to the tick) but fails here by arming on this interaction
// and by leaking statusExpiredMsg into the returned cmd's members.
func TestStatusExpiryNeverWrapsANonNilCmd(t *testing.T) {
	origLifetime := statusMessageLifetime
	statusMessageLifetime = time.Millisecond
	t.Cleanup(func() { statusMessageLifetime = origLifetime })

	// Needs a real editor with an open file: the routing-only harness has no
	// buffer, so a keypress there legitimately returns a nil cmd and arms. The
	// welcome screen must be gone too, or it swallows the keypress.
	m := newViewTestModel(t, false)
	m.welcome = nil
	m = setStatusViaUpdate(t, m, lsp.LspErrorMsg{Method: "textDocument/hover", Message: "boom"})

	// Ctrl+S is a user interaction whose inner update returns a non-nil cmd
	// (the async save flow); executing it yields the save's own concrete
	// message, not a batch.
	updatedAny, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("precondition failed: ctrl+s returned a nil cmd; pick a different interaction")
	}
	if updated.statusTimerArmed {
		t.Fatal("status timer armed on an interaction with a non-nil cmd; the wrapper must defer arming, not batch the tick into the returned cmd")
	}
	for _, msg := range collectCmdMsgs(cmd) {
		if _, expired := msg.(statusExpiredMsg); expired {
			t.Fatalf("expiry tick leaked into the interaction cmd's members: %#v", msg)
		}
	}

	// The deferred tick arms on the next nil-cmd interaction.
	updatedAny2, armCmd := updated.Update(tea.MouseMotionMsg(tea.Mouse{X: 1, Y: 1}))
	m2 := updatedAny2.(Model)
	if armCmd == nil {
		t.Fatal("status expiry timer was not armed by the following nil-cmd interaction")
	}
	fired, ok := armCmd().(statusExpiredMsg)
	if !ok {
		t.Fatalf("expiry command produced %T, want statusExpiredMsg", armCmd())
	}
	if fired.seq != m2.statusSeq {
		t.Fatalf("expiry seq = %d, want %d", fired.seq, m2.statusSeq)
	}
}
