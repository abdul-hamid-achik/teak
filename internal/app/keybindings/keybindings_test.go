package keybindings

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
	"teak/internal/app/modes"
)

func TestBindingAppliesToContext(t *testing.T) {
	b := &Binding{
		Keys:        []string{"ctrl+s"},
		Contexts:    []app.FocusArea{app.FocusEditor, app.FocusTree},
		Description: "Save",
	}

	if !b.AppliesToContext(app.FocusEditor) {
		t.Error("expected applies to FocusEditor")
	}
	if !b.AppliesToContext(app.FocusTree) {
		t.Error("expected applies to FocusTree")
	}
	if b.AppliesToContext(app.FocusGitPanel) {
		t.Error("should not apply to FocusGitPanel")
	}
}

func TestBindingAppliesToContextEmpty(t *testing.T) {
	b := &Binding{
		Keys:        []string{"ctrl+s"},
		Contexts:    nil,
		Description: "Save",
	}

	if !b.AppliesToContext(app.FocusEditor) {
		t.Error("expected applies to all when Contexts is empty")
	}
	if !b.AppliesToContext(app.FocusGitPanel) {
		t.Error("expected applies to all when Contexts is empty")
	}
}

func TestBindingAppliesToMode(t *testing.T) {
	b := &Binding{
		Keys:        []string{"esc"},
		Modes:       []modes.ModeID{modes.ModeInsert, modes.ModeSearch},
		Description: "Exit mode",
	}

	if b.AppliesToMode(modes.ModeNormal) {
		t.Error("should not apply to ModeNormal")
	}
	if !b.AppliesToMode(modes.ModeInsert) {
		t.Error("expected applies to ModeInsert")
	}
	if !b.AppliesToMode(modes.ModeSearch) {
		t.Error("expected applies to ModeSearch")
	}
}

func TestBindingAppliesToModeEmpty(t *testing.T) {
	b := &Binding{
		Keys:        []string{"esc"},
		Modes:       nil,
		Description: "Exit mode",
	}

	if !b.AppliesToMode(modes.ModeNormal) {
		t.Error("expected applies to ModeNormal when Modes is empty")
	}
	if b.AppliesToMode(modes.ModeInsert) {
		t.Error("should not apply to ModeInsert when Modes is empty")
	}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.bindings) != 0 {
		t.Errorf("expected empty bindings, got %d", len(r.bindings))
	}
}

func TestRegistryBind(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+s"}, func(m *app.Model) tea.Cmd { return nil }, "Save")

	if len(r.bindings) != 1 {
		t.Errorf("expected 1 binding, got %d", len(r.bindings))
	}
	if len(r.lookup["ctrl+s"]) != 1 {
		t.Errorf("expected lookup entry for ctrl+s")
	}
}

func TestRegistryBindMultipleKeys(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+s", "cmd+s"}, func(m *app.Model) tea.Cmd { return nil }, "Save")

	if len(r.bindings) != 1 {
		t.Errorf("expected 1 binding, got %d", len(r.bindings))
	}
	if len(r.lookup["ctrl+s"]) != 1 {
		t.Errorf("expected lookup entry for ctrl+s")
	}
	if len(r.lookup["cmd+s"]) != 1 {
		t.Errorf("expected lookup entry for cmd+s")
	}
}

func TestInContext(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+t"}, func(m *app.Model) tea.Cmd { return nil }, "New tab",
		InContext(app.FocusEditor))

	b := r.bindings[0]
	if len(b.Contexts) != 1 || b.Contexts[0] != app.FocusEditor {
		t.Errorf("expected Contexts = [FocusEditor]")
	}
}

func TestInModes(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"esc"}, func(m *app.Model) tea.Cmd { return nil }, "Exit",
		InModes(modes.ModeInsert, modes.ModeSearch))

	b := r.bindings[0]
	if len(b.Modes) != 2 {
		t.Errorf("expected 2 modes")
	}
}

func TestWithPriority(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+c"}, func(m *app.Model) tea.Cmd { return nil }, "Cancel",
		WithPriority(10))
	r.Bind([]string{"ctrl+c"}, func(m *app.Model) tea.Cmd { return nil }, "Copy",
		WithPriority(5))

	b1 := r.bindings[0]
	b2 := r.bindings[1]

	if b1.Priority != 10 {
		t.Errorf("expected priority 10, got %d", b1.Priority)
	}
	if b2.Priority != 5 {
		t.Errorf("expected priority 5, got %d", b2.Priority)
	}
}

func TestGetHelp(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+s"}, func(m *app.Model) tea.Cmd { return nil }, "Save")
	r.Bind([]string{"ctrl+c"}, func(m *app.Model) tea.Cmd { return nil }, "Copy")

	help := r.GetHelp()
	if len(help) != 2 {
		t.Errorf("expected 2 help entries, got %d", len(help))
	}
	if help[0].Description != "Save" {
		t.Errorf("expected first help entry 'Save', got %q", help[0].Description)
	}
}

func TestGetHelpReturnsCopy(t *testing.T) {
	r := NewRegistry()

	r.Bind([]string{"ctrl+s"}, func(m *app.Model) tea.Cmd { return nil }, "Save")

	help := r.GetHelp()
	help[0].Description = "Modified"

	if r.bindings[0].Description != "Save" {
		t.Error("GetHelp should return a copy, not the original")
	}
}
