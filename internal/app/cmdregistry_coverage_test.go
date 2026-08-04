package app

import (
	"testing"

	"teak/internal/editor"
)

func TestCommandRegistryCoversCoreShortcuts(t *testing.T) {
	m := newInputRoutingTestModel(t)
	registry := m.commandRegistry()
	ids := make(map[string]Command, len(registry))
	for _, cmd := range registry {
		if cmd.ID == "" || cmd.Label == "" || cmd.Execute == nil {
			t.Fatalf("registry entry %+v has an empty ID, label, or executor", cmd)
		}
		ids[cmd.ID] = cmd
	}
	wanted := []string{
		"format_file", "goto_definition", "rename_symbol", "code_actions",
		"hover", "document_symbols", "toggle_split", "close_split",
		"cycle_split", "toggle_breakpoint", "fold", "unfold", "fold_all",
		"unfold_all", "next_tab", "prev_tab", "next_problem", "prev_problem",
	}
	for _, id := range wanted {
		if _, ok := ids[id]; !ok {
			t.Errorf("command %q missing from the palette registry", id)
		}
	}
}

func TestCommandPaletteTabCycling(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "a.txt", "x\n", "x\n")
	addDirtyEditor(t, &m, "b.txt", "y\n", "y\n")
	m.activateTab(0)

	updatedAny, _ := m.handleCommandPaletteAction(nextTabMsg{})
	m = updatedAny.(Model)
	if m.activeTab != 1 {
		t.Fatalf("active tab after next_tab = %d, want 1", m.activeTab)
	}

	updatedAny, _ = m.handleCommandPaletteAction(prevTabMsg{})
	m = updatedAny.(Model)
	if m.activeTab != 0 {
		t.Fatalf("active tab after prev_tab = %d, want 0", m.activeTab)
	}
}

func TestCommandPaletteFoldCommands(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "a.txt", "func a() {\n\tone()\n}\nfunc b() {\n\ttwo()\n}\n", "content\n")
	m.activeEditor().Folds.SetRegions([]editor.FoldRegion{
		{StartLine: 0, EndLine: 2},
		{StartLine: 3, EndLine: 5},
	})

	updatedAny, _ := m.handleCommandPaletteAction(foldAllMsg{})
	m = updatedAny.(Model)
	if !m.activeEditor().Folds.HasCollapsedRegions() {
		t.Fatal("fold_all did not collapse any region")
	}

	updatedAny, _ = m.handleCommandPaletteAction(unfoldAllMsg{})
	m = updatedAny.(Model)
	if m.activeEditor().Folds.HasCollapsedRegions() {
		t.Fatal("unfold_all left collapsed regions behind")
	}
}
