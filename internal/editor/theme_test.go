package editor

import (
	"testing"

	"teak/internal/editor/overlays"
	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestEditorSetThemeReplacesHighlighterAndRejectsOldResults(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	buf.FilePath = "main.go"
	ed := New(buf, ui.NordTheme(), DefaultConfig())
	oldHighlighter := ed.Highlighter
	ed.ScheduleInitialTokenize()
	oldGeneration := ed.tokenizer.full.generation
	next := ui.DraculaTheme()

	cmd := ed.SetTheme(next)
	if cmd == nil {
		t.Fatal("SetTheme returned nil tokenization command")
	}
	if ed.Highlighter == oldHighlighter {
		t.Fatal("SetTheme did not replace the highlighter")
	}
	if ed.theme != next {
		t.Fatal("SetTheme did not update the editor theme")
	}
	if ed.acceptsTokenizeComplete(TokenizeCompleteMsg{
		EditorID: ed.ID(), Version: buf.Version(), Generation: oldGeneration,
		Lines: [][]highlight.StyledToken{{{Text: "stale"}}},
	}) {
		t.Fatal("SetTheme accepted tokenization from the previous theme")
	}

	updated, _ := ed.Update(cmd())
	if updated.Highlighter.LineCount() == 0 {
		t.Fatal("SetTheme tokenization did not populate the replacement highlighter")
	}
}

func TestThemeHoldingEditorModelsSetThemeWithoutResettingState(t *testing.T) {
	next := ui.DraculaTheme()

	buf := text.NewBufferFromBytes([]byte("x"))
	ed := New(buf, ui.NordTheme(), DefaultConfig())
	ed.autocomplete.Show([]overlays.AutocompleteItem{{Label: "item"}})
	ed.hover.Show("documentation")
	ed.signatureHelp.Show(&overlays.SignatureData{Signatures: []overlays.SignatureInfo{{Label: "fn()"}}})
	ed.contextMenu.Show([]ContextMenuItem{{Label: "Action"}}, 1, 2)
	ed.find.Show()
	ed.SetTheme(next)
	if !ed.autocomplete.Visible || !ed.hover.Visible || !ed.signatureHelp.Visible || !ed.contextMenu.Visible || !ed.find.Visible() {
		t.Fatal("SetTheme reset visible editor overlay state")
	}
	if ed.contextMenu.theme != next || ed.find.theme != next {
		t.Fatal("SetTheme did not propagate the theme to every editor overlay")
	}

	tabBar := NewTabBar(ui.NordTheme())
	tabBar.AddTab("main.go", "main.go")
	tabBar.SetTheme(next)
	if tabBar.theme != next || len(tabBar.Tabs) != 1 {
		t.Fatal("TabBar SetTheme changed state or left the old theme")
	}

	help := NewHelpModel(ui.NordTheme())
	help.SetTheme(next)
	if help.theme != next || len(help.lines) == 0 {
		t.Fatal("HelpModel SetTheme did not rebuild themed lines")
	}

	welcome := NewWelcome(ui.NordTheme())
	welcome.SetTheme(next)
	if welcome.theme != next || !welcome.Active {
		t.Fatal("Welcome SetTheme changed state or left the old theme")
	}
}
