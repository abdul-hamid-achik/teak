package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestQuitFilterRoutesExternalQuitThroughModel(t *testing.T) {
	model := newInputRoutingTestModel(t)

	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{name: "quit", msg: tea.QuitMsg{}},
		{name: "interrupt", msg: tea.InterruptMsg{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := QuitFilter(model, tt.msg).(requestQuitMsg); !ok {
				t.Fatalf("QuitFilter(%T) did not return requestQuitMsg", tt.msg)
			}
		})
	}
}

func TestQuitFilterAllowsApprovedQuit(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.quitApproved = true

	if _, ok := QuitFilter(model, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Fatal("QuitFilter blocked an approved tea.QuitMsg")
	}
}

func TestExternalQuitWithDirtyBufferShowsConfirmation(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("unsaved"))

	updatedAny, cmd := model.Update(requestQuitMsg{})
	updated := updatedAny.(Model)

	if updated.unsavedConfirm == nil {
		t.Fatal("external quit did not show the unsaved changes confirmation")
	}
	if updated.quitApproved {
		t.Fatal("external quit was approved before the user made a decision")
	}
	if cmd != nil {
		t.Fatal("external quit returned a command before the user confirmed")
	}
}

func TestFinalizeQuitWaitsForCleanupBeforeApprovingTerminalQuit(t *testing.T) {
	model := newInputRoutingTestModel(t)

	shuttingDown, cleanupCmd := model.finalizeQuit()
	if cleanupCmd == nil {
		t.Fatal("finalizeQuit() did not schedule cleanup")
	}
	if !shuttingDown.shutdownStarted {
		t.Fatal("finalizeQuit() did not mark shutdown as started")
	}
	if shuttingDown.quitApproved {
		t.Fatal("terminal quit was approved before cleanup completed")
	}

	msg := cleanupCmd()
	if _, ok := msg.(shutdownCompleteMsg); !ok {
		t.Fatalf("cleanup command returned %T, want shutdownCompleteMsg", msg)
	}

	finalAny, quitCmd := shuttingDown.Update(msg)
	final := finalAny.(Model)
	if !final.quitApproved {
		t.Fatal("terminal quit was not approved after cleanup completed")
	}
	if quitCmd == nil {
		t.Fatal("shutdown completion did not request terminal quit")
	}
	quitMsg := quitCmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T, want tea.QuitMsg", quitMsg)
	}
}
