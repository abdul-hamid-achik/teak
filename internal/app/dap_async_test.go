package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/dap"
)

type blockingDebugRunner struct {
	startGate     chan struct{}
	stopGate      chan struct{}
	actionGate    chan struct{}
	started       chan struct{}
	stopped       chan struct{}
	actionStarted chan struct{}
}

func (r blockingDebugRunner) StartAndLaunch(dap.DebugConfig) error {
	close(r.started)
	<-r.startGate
	return nil
}

func (r blockingDebugRunner) Stop() {
	close(r.stopped)
	<-r.stopGate
}

func (r blockingDebugRunner) Run(debugAction) error {
	if r.actionStarted != nil {
		close(r.actionStarted)
	}
	if r.actionGate != nil {
		<-r.actionGate
	}
	return nil
}

func newDAPAsyncTestModel(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)
	model.welcome = nil
	model.showTree = false
	model.focus = FocusEditor
	model.activeEditor().Buffer.FilePath = "main.go"
	model.tabBar.Tabs[model.activeTab].FilePath = "main.go"
	return model
}

func TestBeginDebugStartDoesNotBlockUpdateOrStartTwice(t *testing.T) {
	model := newDAPAsyncTestModel(t)
	runner := blockingDebugRunner{
		startGate: make(chan struct{}),
		started:   make(chan struct{}),
		stopGate:  make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	model.debugRunner = runner

	started := time.Now()
	updatedAny, cmd := model.handleDebugStart()
	updated := updatedAny.(Model)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("handleDebugStart blocked Update for %s", elapsed)
	}
	if cmd == nil || !updated.debugLifecycle.pending {
		t.Fatal("debug start did not schedule an asynchronous pending operation")
	}

	_, duplicate := updated.handleDebugStart()
	if duplicate != nil {
		t.Fatal("second debug start scheduled a duplicate session")
	}

	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("debug start command did not run")
	}
	close(runner.startGate)
	msg := <-result
	startResult, ok := msg.(debugStartResultMsg)
	if !ok || startResult.Err != nil {
		t.Fatalf("start command result = %#v, want successful debugStartResultMsg", msg)
	}

	finalAny, _ := updated.Update(startResult)
	final := finalAny.(Model)
	if final.debugLifecycle.pending || final.debuggerPanel.State() != dap.StateRunning {
		t.Fatal("successful asynchronous start did not update debugger UI")
	}
}

func TestDebugLifecycleIgnoresStaleStartAndStateResults(t *testing.T) {
	model := newDAPAsyncTestModel(t)
	model.debugLifecycle.generation = 2
	model.debugLifecycle.pending = true

	updatedAny, _ := model.Update(debugStartResultMsg{Generation: 1})
	updated := updatedAny.(Model)
	if !updated.debugLifecycle.pending || updated.debuggerPanel.State() != dap.StateInactive {
		t.Fatal("stale start result changed debugger lifecycle state")
	}

	updatedAny, _ = updated.Update(debugStateMsg{
		Generation: 1,
		Frames:     []dap.StackFrame{{Name: "stale", Line: 1}},
	})
	updated = updatedAny.(Model)
	if strings.Contains(updated.debuggerPanel.View(), "stale") {
		t.Fatal("stale debug state populated the debugger panel")
	}
}

func TestDebugStopRunsAsynchronouslyAndInvalidatesStart(t *testing.T) {
	model := newDAPAsyncTestModel(t)
	runner := blockingDebugRunner{
		startGate: make(chan struct{}),
		started:   make(chan struct{}),
		stopGate:  make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	model.debugRunner = runner
	model.debugLifecycle.generation = 3
	model.debugLifecycle.pending = true

	started := time.Now()
	updatedAny, cmd := model.handleDebugStop("Debugging stopped")
	updated := updatedAny.(Model)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("handleDebugStop blocked Update for %s", elapsed)
	}
	if cmd == nil || !updated.debugLifecycle.pending || updated.debugLifecycle.generation != 4 {
		t.Fatal("debug stop did not schedule an invalidating asynchronous operation")
	}
	if !updated.debugLifecycle.stopping || updated.debuggerPanel.State() != dap.StateInactive {
		t.Fatal("debug stop did not immediately make the UI inactive")
	}

	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-runner.stopped:
	case <-time.After(time.Second):
		t.Fatal("debug stop command did not run")
	}
	close(runner.stopGate)
	if _, ok := (<-result).(debugStopResultMsg); !ok {
		t.Fatal("debug stop command did not return debugStopResultMsg")
	}
}

func TestDebugActionResultReportsErrorWithoutBlocking(t *testing.T) {
	model := newDAPAsyncTestModel(t)
	model.debugLifecycle.generation = 1
	model.debugLifecycle.actionPending = true
	updatedAny, _ := model.Update(debugActionResultMsg{
		Generation: 1,
		Action:     debugActionNext,
		Err:        errors.New("adapter unavailable"),
	})
	updated := updatedAny.(Model)
	if updated.debugLifecycle.actionPending {
		t.Fatal("debug action result left action pending")
	}
	if updated.status != "Debug error: adapter unavailable" {
		t.Fatalf("status = %q", updated.status)
	}
}

func TestDebugActionRunsAsynchronouslyAndIsSerialized(t *testing.T) {
	model := newDAPAsyncTestModel(t)
	runner := blockingDebugRunner{
		actionGate:    make(chan struct{}),
		actionStarted: make(chan struct{}),
	}
	model.debugRunner = runner
	model.debugLifecycle.generation = 7
	model.debuggerPanel.SetState(dap.StatePaused)

	started := time.Now()
	updatedAny, cmd := model.handleDebugAction(debugActionNext)
	updated := updatedAny.(Model)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("handleDebugAction blocked Update for %s", elapsed)
	}
	if cmd == nil || !updated.debugLifecycle.actionPending {
		t.Fatal("debug action did not schedule an asynchronous pending operation")
	}
	_, duplicate := updated.handleDebugAction(debugActionNext)
	if duplicate != nil {
		t.Fatal("second debug action was not serialized")
	}

	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-runner.actionStarted:
	case <-time.After(time.Second):
		t.Fatal("debug action command did not run")
	}
	close(runner.actionGate)

	updatedAny, _ = updated.Update(<-result)
	updated = updatedAny.(Model)
	if updated.debugLifecycle.actionPending || updated.debuggerPanel.State() != dap.StateRunning {
		t.Fatal("successful debug action did not update the debugger UI")
	}
}

func TestDebugKeyboardAndPaletteUseAsyncLifecycle(t *testing.T) {
	newRunner := func() blockingDebugRunner {
		return blockingDebugRunner{
			startGate: make(chan struct{}),
			started:   make(chan struct{}),
			stopGate:  make(chan struct{}),
			stopped:   make(chan struct{}),
		}
	}

	t.Run("F5", func(t *testing.T) {
		model := newDAPAsyncTestModel(t)
		model.debugRunner = newRunner()
		updatedAny, cmd, handled := model.handleGlobalKey(tea.KeyPressMsg{Code: tea.KeyF5})
		updated := updatedAny.(Model)
		if !handled || cmd == nil || !updated.debugLifecycle.pending {
			t.Fatal("F5 did not delegate startup to the asynchronous lifecycle")
		}
	})

	t.Run("command palette", func(t *testing.T) {
		model := newDAPAsyncTestModel(t)
		model.debugRunner = newRunner()
		updatedAny, cmd := model.handleCommandPaletteAction(debugStartMsg{})
		updated := updatedAny.(Model)
		if cmd == nil || !updated.debugLifecycle.pending {
			t.Fatal("command palette start did not delegate to the asynchronous lifecycle")
		}
	})
}
