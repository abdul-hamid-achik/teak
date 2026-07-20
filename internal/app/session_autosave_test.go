package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"teak/internal/config"
	"teak/internal/session"
)

func TestSessionAutoSaveCoalescesLatestSnapshot(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.appCfg.Session.Enabled = true
	model.appCfg.Session.AutoSaveInterval = 1
	addDirtyEditor(t, &model, "first.go", "one\n", "one\n")

	entered := make(chan session.State, 2)
	release := make(chan struct{})
	model.sessionSaver = func(state session.State) error {
		entered <- state
		<-release
		return nil
	}

	updatedAny, firstCmd := model.Update(sessionAutoSaveMsg{})
	model = updatedAny.(Model)
	if firstCmd == nil || !model.sessionSaves.inFlight {
		t.Fatal("first autosave did not start an asynchronous writer")
	}

	firstResult := make(chan sessionSaveResultMsg, 1)
	go func() { firstResult <- firstCmd().(sessionSaveResultMsg) }()
	select {
	case state := <-entered:
		if len(state.Tabs) != 1 || state.Tabs[0].FilePath != model.editors[0].Buffer.FilePath {
			t.Fatalf("first autosave state = %#v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("first autosave writer did not start")
	}

	addDirtyEditor(t, &model, "latest.go", "two\n", "two\n")
	updatedAny, queuedCmd := model.Update(sessionAutoSaveMsg{})
	model = updatedAny.(Model)
	if queuedCmd != nil || model.sessionSaves.queued == nil {
		t.Fatal("overlapping autosave was not coalesced")
	}

	close(release)
	updatedAny, nextCmd := model.Update(<-firstResult)
	model = updatedAny.(Model)
	if nextCmd == nil || !model.sessionSaves.inFlight {
		t.Fatal("latest queued autosave did not start after the first writer")
	}

	secondResult := make(chan sessionSaveResultMsg, 1)
	go func() { secondResult <- nextCmd().(sessionSaveResultMsg) }()
	select {
	case state := <-entered:
		if len(state.Tabs) != 2 || state.Tabs[1].FilePath != model.editors[1].Buffer.FilePath {
			t.Fatalf("coalesced autosave state = %#v, want latest snapshot", state)
		}
	case <-time.After(time.Second):
		t.Fatal("latest autosave writer did not start")
	}

	updatedAny, _ = model.Update(<-secondResult)
	if updated := updatedAny.(Model); updated.sessionSaves.inFlight || updated.sessionSaves.queued != nil {
		t.Fatalf("autosave state not drained: %#v", updated.sessionSaves)
	}
}

func TestSessionAutoSaveIgnoresStaleCompletion(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.appCfg.Session.Enabled = true
	model.appCfg.Session.AutoSaveInterval = 1
	addDirtyEditor(t, &model, "first.go", "one\n", "one\n")
	model.sessionSaver = func(session.State) error { return nil }

	updatedAny, firstCmd := model.Update(sessionAutoSaveMsg{})
	model = updatedAny.(Model)
	firstResult := firstCmd().(sessionSaveResultMsg)

	addDirtyEditor(t, &model, "latest.go", "two\n", "two\n")
	updatedAny, _ = model.Update(sessionAutoSaveMsg{})
	model = updatedAny.(Model)
	updatedAny, secondCmd := model.Update(firstResult)
	model = updatedAny.(Model)
	if secondCmd == nil || !model.sessionSaves.inFlight || model.sessionSaves.generation == firstResult.generation {
		t.Fatal("latest snapshot did not begin a distinct session write")
	}

	updatedAny, staleCmd := model.Update(firstResult)
	updated := updatedAny.(Model)
	if staleCmd != nil || !updated.sessionSaves.inFlight || updated.sessionSaves.generation == firstResult.generation {
		t.Fatal("stale session completion changed the active latest write")
	}
}

func TestFinalSessionSaveRunsAfterInflightAutoSave(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.appCfg.Session.Enabled = true
	model.appCfg.Session.AutoSaveInterval = 1
	addDirtyEditor(t, &model, "old.go", "old\n", "old\n")

	var mu sync.Mutex
	var saved []session.State
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	model.sessionSaver = func(state session.State) error {
		mu.Lock()
		saved = append(saved, state)
		mu.Unlock()
		entered <- struct{}{}
		<-release
		return nil
	}

	updatedAny, autoCmd := model.Update(sessionAutoSaveMsg{})
	model = updatedAny.(Model)
	autoResult := make(chan sessionSaveResultMsg, 1)
	go func() { autoResult <- autoCmd().(sessionSaveResultMsg) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("autosave writer did not start")
	}

	addDirtyEditor(t, &model, "final.go", "final\n", "final\n")
	updated, shutdownCmd := model.finalizeQuit()
	if shutdownCmd != nil || !updated.shutdownStarted || updated.sessionSaves.queued == nil {
		t.Fatal("quit did not queue the final session snapshot behind autosave")
	}
	model = updated

	close(release)
	updatedAny, finalCmd := model.Update(<-autoResult)
	model = updatedAny.(Model)
	if finalCmd == nil || !model.sessionSaves.inFlight {
		t.Fatal("final session save did not start after autosave")
	}
	finalResult := make(chan sessionSaveResultMsg, 1)
	go func() { finalResult <- finalCmd().(sessionSaveResultMsg) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("final session writer did not start")
	}

	updatedAny, cleanupCmd := model.Update(<-finalResult)
	if cleanupCmd == nil || !updatedAny.(Model).shutdownStarted {
		t.Fatal("final session save did not advance shutdown")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 2 || len(saved[1].Tabs) != 2 || saved[1].Tabs[1].FilePath != model.editors[1].Buffer.FilePath {
		t.Fatalf("saved session sequence = %#v, want final snapshot last", saved)
	}
}

func TestFinalSessionSaveErrorStillContinuesShutdown(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	model.appCfg.Session.Enabled = true
	addDirtyEditor(t, &model, "final.go", "final\n", "final\n")
	model.sessionSaver = func(session.State) error { return errors.New("disk unavailable") }

	shuttingDown, saveCmd := model.finalizeQuit()
	if saveCmd == nil || !shuttingDown.shutdownStarted {
		t.Fatal("final session save was not scheduled before shutdown")
	}
	result, ok := saveCmd().(sessionSaveResultMsg)
	if !ok || result.err == nil {
		t.Fatalf("final save result = %#v, want write error", result)
	}
	updatedAny, cleanupCmd := shuttingDown.Update(result)
	updated := updatedAny.(Model)
	if cleanupCmd == nil || !updated.shutdownStarted || updated.sessionSaves.inFlight {
		t.Fatal("shutdown did not continue after final session save error")
	}
}
