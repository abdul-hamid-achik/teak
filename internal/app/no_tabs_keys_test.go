package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/config"
)

// modelWithNoTabs returns a Model in the state reached by closing every tab,
// where activeEditor() returns nil.
func modelWithNoTabs(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(m.cleanup)
	m.width, m.height = 120, 40
	m.editors = nil
	m.activeTab = 0
	if m.activeEditor() != nil {
		t.Fatal("setup: expected activeEditor() to be nil with no tabs")
	}
	return m
}

// Each of these dereferenced activeEditor() without a nil check, so pressing the
// key with no tabs open panicked and took the editor down.
func TestLSPRequestsDoNotPanicWithNoTabs(t *testing.T) {
	requests := map[string]func(Model) (Model, tea.Cmd){
		"requestDefinition":      Model.requestDefinition,
		"requestCodeActions":     Model.requestCodeActions,
		"requestDocumentSymbols": Model.requestDocumentSymbols,
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			m := modelWithNoTabs(t)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked with no tabs open: %v", name, r)
				}
			}()
			if _, cmd := request(m); cmd != nil {
				t.Errorf("%s returned a command with no active editor", name)
			}
		})
	}
}

func TestGoToLineDoesNotPanicWithNoTabs(t *testing.T) {
	m := modelWithNoTabs(t)
	m.goToLineMode = true
	m.goToLineInput = "5"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("go-to-line panicked with no tabs open: %v", r)
		}
	}()

	updated, _ := m.handleGoToLineInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got, ok := updated.(Model); !ok {
		t.Fatalf("handleGoToLineInput returned %T, want Model", updated)
	} else if got.goToLineMode {
		t.Error("go-to-line mode stayed open after Enter")
	}
}
