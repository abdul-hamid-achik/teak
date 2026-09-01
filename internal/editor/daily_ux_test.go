package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor/overlays"
	"teak/internal/text"
)

func TestUpdateFindReturnsDebounceCommand(t *testing.T) {
	ed := findTestEditor(t, 20)
	updated, cmd := ed.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd == nil {
		t.Fatal("typing in find discarded the debounce command")
	}
	if updated.find.MatchCount() != 0 {
		t.Fatal("matches were computed synchronously")
	}
}

func TestTabIndentsMultilineSelection(t *testing.T) {
	ed := newEditor("alpha\nbeta\n", 0, 0)
	ed.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 1, Col: 4})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "tab"})
	if got, want := ed.Buffer.Content(), "    alpha\n    beta\n"; got != want {
		t.Fatalf("tab indent = %q, want %q", got, want)
	}
}

func TestTabInsertsTabWhenInsertTabs(t *testing.T) {
	ed := newEditor("x", 0, 1)
	ed.Config.InsertTabs = true
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "tab"})
	if got, want := ed.Buffer.Content(), "x\t"; got != want {
		t.Fatalf("insert_tabs tab = %q, want %q", got, want)
	}
}

func TestCtrlUnderscoreTogglesComment(t *testing.T) {
	ed := newEditor("hello\n", 0, 0)
	ed.Config.CommentPrefix = "//"
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+_"})
	if !strings.HasPrefix(ed.Buffer.Content(), "//") {
		t.Fatalf("ctrl+_ did not comment, got %q", ed.Buffer.Content())
	}
}

func TestPasteNormalizesCRLF(t *testing.T) {
	ed := newEditor("", 0, 0)
	ed, _ = ed.Update(tea.PasteMsg{Content: "a\r\nb\r\n"})
	if strings.Contains(ed.Buffer.Content(), "\r") {
		t.Fatalf("paste left CR bytes: %q", ed.Buffer.Content())
	}
	if got, want := ed.Buffer.Content(), "a\nb\n"; got != want {
		t.Fatalf("paste content = %q, want %q", got, want)
	}
}

func TestQuoteWrapsSelection(t *testing.T) {
	ed := newEditor("hello", 0, 0)
	ed.Buffer.SetSelection(text.Position{}, text.Position{Col: 5})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "\""})
	if got, want := ed.Buffer.Content(), `"hello"`; got != want {
		t.Fatalf("quote wrap = %q, want %q", got, want)
	}
}

func TestQuoteAfterWordInsertsSingle(t *testing.T) {
	ed := newEditor("hello", 0, 5)
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "\""})
	if got, want := ed.Buffer.Content(), `hello"`; got != want {
		t.Fatalf("bare quote = %q, want %q", got, want)
	}
}

func TestExpandSnippetCaretAndPlaceholders(t *testing.T) {
	got, caret := expandSnippet("Println($0)")
	if got != "Println()" || caret != 8 {
		t.Fatalf("expandSnippet(Println($0)) = %q, caret %d", got, caret)
	}
	got, caret = expandSnippet("fmt.${1:Println}($0)")
	if got != "fmt.Println()" || caret != 12 {
		t.Fatalf("expandSnippet(fmt.${1:Println}($0)) = %q, caret %d", got, caret)
	}
}

func TestApplyCompletionExpandsSnippetAndAdditionalEdits(t *testing.T) {
	ed := newEditor("package p\n\nfunc main() {\n\tfm\n}\n", 3, 3)
	ed.applyCompletion(overlays.AutocompleteItem{
		Label:      "fmt",
		InsertText: "fmt",
		AdditionalEdits: []overlays.AutocompleteTextEdit{{
			StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 0,
			NewText: "import \"fmt\"\n",
		}},
	})
	if !strings.Contains(ed.Buffer.Content(), `import "fmt"`) {
		t.Fatalf("additional edit missing: %q", ed.Buffer.Content())
	}
	if !strings.Contains(ed.Buffer.Content(), "fmt") {
		t.Fatalf("primary insert missing: %q", ed.Buffer.Content())
	}
}

func TestHideFindKeepsMatchesForRepeat(t *testing.T) {
	ed := findUXEditor("needle here\nneedle there", text.Position{})
	ed.ShowFind()
	for _, ch := range "needle" {
		ed.UpdateFind(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	ed = drainFindScan(t, ed)
	if ed.FindMatchCount() == 0 {
		t.Fatal("setup produced no matches")
	}
	ed.escapeFind()
	if ed.IsFindVisible() {
		t.Fatal("find still visible after escape")
	}
	if !ed.CanRepeatFind() || ed.FindMatchCount() == 0 {
		t.Fatal("escaped find forgot the last query/matches")
	}
}

func TestLanguageLabelForFile(t *testing.T) {
	if got := LanguageLabelForFile("main.go"); got != "Go" {
		t.Fatalf("LanguageLabelForFile(main.go) = %q", got)
	}
	if got := LanguageLabelForFile(""); got != "Plain Text" {
		t.Fatalf("LanguageLabelForFile empty = %q", got)
	}
}

func TestEnterAfterBraceAddsIndent(t *testing.T) {
	ed := newEditor("func() {", 0, 8)
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "enter"})
	if got, want := ed.Buffer.Content(), "func() {\n    "; got != want {
		t.Fatalf("enter after brace = %q, want %q", got, want)
	}
}

func TestHorizontalWheelScrollsX(t *testing.T) {
	ed := newEditor(strings.Repeat("x", 80)+"\n", 0, 0)
	ed.Viewport.Width = 20
	ed.Viewport.Height = 5
	ed, _ = ed.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelRight}))
	if ed.Viewport.ScrollX == 0 {
		t.Fatal("horizontal wheel did not scroll right")
	}
	ed, _ = ed.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelLeft}))
	if ed.Viewport.ScrollX != 0 {
		t.Fatalf("scroll left = %d, want 0", ed.Viewport.ScrollX)
	}
}

func TestFindCaseSensitiveToggle(t *testing.T) {
	ed := findUXEditor("Foo foo", text.Position{})
	ed.ShowFind()
	for _, ch := range "foo" {
		ed.UpdateFind(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	ed = drainFindScan(t, ed)
	if ed.FindMatchCount() != 2 {
		t.Fatalf("insensitive matches = %d, want 2", ed.FindMatchCount())
	}
	ed.UpdateFind(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	ed = drainFindScan(t, ed)
	if ed.FindMatchCount() != 1 {
		t.Fatalf("case-sensitive matches = %d, want 1", ed.FindMatchCount())
	}
}

func TestCollapsedFoldsWinOverWordWrap(t *testing.T) {
	ed := newEditor("func f() {\n\tx := 1\n}\n", 0, 0)
	ed.Config.WordWrap = true
	ed.SetSize(40, 10)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: 0, EndLine: 2}})
	ed.Folds.FoldAll()
	view := ed.View()
	if strings.Contains(view, "x := 1") {
		t.Fatalf("folded body still visible under wrap:\n%s", view)
	}
}

func TestFoldAllMovesCaretOffHiddenLine(t *testing.T) {
	ed := newEditor("func f() {\n\tx := 1\n}\n", 1, 0)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: 0, EndLine: 2}})
	ed.Folds.FoldAll()
	ed.RevealCursorAfterFold()
	if ed.Folds.IsLineHidden(ed.Buffer.Cursor.Line) {
		t.Fatalf("caret still on hidden line %d", ed.Buffer.Cursor.Line)
	}
}

func TestCtrlShiftLSelectsAllOccurrences(t *testing.T) {
	ed := newEditor("foo foo foo\n", 0, 0)
	ed.Buffer.SetSelection(text.Position{}, text.Position{Col: 3})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+shift+l"})
	if got := ed.Buffer.Selections.Count(); got != 3 {
		t.Fatalf("ctrl+shift+l selection count = %d, want 3", got)
	}
}

func TestCtrlUUndoesLastCursor(t *testing.T) {
	ed := newEditor("foo foo foo\n", 0, 0)
	ed.Buffer.SetSelection(text.Position{}, text.Position{Col: 3})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+d"})
	if got := ed.Buffer.Selections.Count(); got != 2 {
		t.Fatalf("ctrl+d selection count = %d, want 2", got)
	}
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+u"})
	if got := ed.Buffer.Selections.Count(); got != 1 {
		t.Fatalf("ctrl+u selection count = %d, want 1", got)
	}
}

func TestShiftAltISplitsSelectionIntoLines(t *testing.T) {
	ed := newEditor("alpha\nbeta\n", 0, 0)
	ed.Buffer.SetSelection(text.Position{}, text.Position{Line: 1, Col: 4})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "shift+alt+i"})
	if got := ed.Buffer.Selections.Count(); got != 2 {
		t.Fatalf("shift+alt+i selection count = %d, want 2", got)
	}
}
