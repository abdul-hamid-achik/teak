package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
)

func TestLineTransformPreparedRoutesAndCommits(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer = text.NewBufferFromBytes([]byte("a\nb\nc"))
	model.activeEditor().Buffer.SetCursor(text.Position{Line: 1})
	ed, prepare := model.activeEditor().Update(tea.KeyPressMsg{Text: "alt+down"})
	if prepare == nil {
		t.Fatal("line move did not schedule preparation")
	}
	model.setEditor(model.activeTab, ed)
	beforeVersion := ed.Buffer.Version()

	updatedAny, followup := model.Update(prepare())
	updated := updatedAny.(Model)
	if got, want := updated.activeEditor().Buffer.Content(), "a\nc\nb"; got != want {
		t.Fatalf("routed line transform content = %q, want %q", got, want)
	}
	if got := updated.activeEditor().Buffer.Version(); got != beforeVersion+1 {
		t.Fatalf("routed line transform version = %d, want %d", got, beforeVersion+1)
	}
	_ = followup
}
