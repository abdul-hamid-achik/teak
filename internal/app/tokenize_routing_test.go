package app

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/text"
)

func TestTokenizeCompletionRoutesToOwningEditorAfterTabSwitch(t *testing.T) {
	model := newInputRoutingTestModel(t)
	editorA := newTokenizeRoutingEditor(model, "a.go", "alpha")
	editorB := newTokenizeRoutingEditor(model, "b.go", "beta")
	cmdA := editorA.ScheduleInitialTokenize()
	if cmdA == nil {
		t.Fatal("expected editor A initial tokenization")
	}
	// Match both version and generation so those fields cannot accidentally
	// protect the active tab from editor A's result.
	if cmdB := editorB.ScheduleInitialTokenize(); cmdB == nil {
		t.Fatal("expected editor B initial tokenization")
	}
	model = installTokenizeRoutingEditors(model, editorA, editorB, 1)

	updated := updateInputRoutingModel(t, model, cmdA())
	if got := updated.editors[0].Highlighter.LineCount(); got <= 60 {
		t.Errorf("owner cache has %d lines, want full tokenization", got)
	}
	if got := updated.editors[1].Highlighter.LineCount(); got != 60 {
		t.Errorf("active non-owner cache has %d lines, want preserved 60-line prefix", got)
	}
}

func TestRetokenizeTickRoutesToOwningEditorAfterTabSwitch(t *testing.T) {
	model := newInputRoutingTestModel(t)
	editorA := newTokenizeRoutingEditor(model, "a.go", "alpha")
	editorB := newTokenizeRoutingEditor(model, "b.go", "beta")

	var tickA tea.Cmd
	editorA, tickA = editorA.Update(tea.PasteMsg{Content: "edited "})
	if tickA == nil {
		t.Fatal("expected editor A retokenize tick")
	}
	// Give B the same buffer version as A. Its own tick is deliberately not run.
	editorB, _ = editorB.Update(tea.PasteMsg{Content: "other "})
	expectedBCache := editorB.Highlighter.LineCount()
	model = installTokenizeRoutingEditors(model, editorA, editorB, 1)

	updatedModel, tokenizeCmd := model.Update(tickA())
	updated := updatedModel.(Model)
	if tokenizeCmd == nil {
		t.Fatal("retokenize tick did not schedule owner tokenization")
	}
	updatedModel, _ = updated.Update(tokenizeCmd())
	updated = updatedModel.(Model)

	if got := updated.editors[0].Highlighter.LineCount(); got <= 60 {
		t.Errorf("owner cache has %d lines, want full retokenization", got)
	}
	if got := updated.editors[1].Highlighter.LineCount(); got != expectedBCache {
		t.Errorf("active non-owner cache has %d lines, want preserved %d-line state", got, expectedBCache)
	}
}

func newTokenizeRoutingEditor(model Model, name, identifier string) editor.Editor {
	content := bytes.Repeat([]byte("var "+identifier+" = 42\n"), 80)
	buf := text.NewBufferFromBytes(content)
	buf.FilePath = name
	return editor.New(buf, model.theme, editor.DefaultConfig())
}

func installTokenizeRoutingEditors(model Model, editorA, editorB editor.Editor, active int) Model {
	model.editors = []editor.Editor{editorA, editorB}
	model.tabBar = editor.NewTabBar(model.theme)
	model.tabBar.AddTab("a.go", "a.go")
	model.tabBar.AddTab("b.go", "b.go")
	model.activeTab = active
	model.tabBar.ActiveIdx = active
	return model
}
