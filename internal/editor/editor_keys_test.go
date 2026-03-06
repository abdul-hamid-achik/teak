package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

// helper to create a ready editor with content and cursor at a position.
func newEditor(content string, line, col int) Editor {
	buf := text.NewBufferFromBytes([]byte(content))
	e := New(buf, ui.DefaultTheme(), DefaultConfig())
	e.SetSize(80, 24)
	e.Buffer.Cursor = text.Position{Line: line, Col: col}
	return e
}

func editorContent(e Editor) string {
	return string(e.Buffer.Bytes())
}

// --- Clipboard ---

func TestEditorCopyDoesNotModifyBuffer(t *testing.T) {
	e := newEditor("hello world", 0, 0)
	e.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+c"})

	if got := editorContent(e); got != "hello world" {
		t.Errorf("ctrl+c should not modify buffer, got %q", got)
	}
	if e.Buffer.Selection == nil {
		t.Error("ctrl+c should preserve selection")
	}
}

func TestEditorCutWithSelection(t *testing.T) {
	e := newEditor("hello world", 0, 0)
	e.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+x"})

	if got := editorContent(e); got != " world" {
		t.Errorf("ctrl+x should cut selection, got %q", got)
	}
}

func TestEditorCutWithoutSelection(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+x"})

	// Without selection, nothing should be cut
	if got := editorContent(e); got != "hello world" {
		t.Errorf("ctrl+x without selection should not modify buffer, got %q", got)
	}
}

func TestEditorCopyWithoutSelection(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+c"})

	if got := editorContent(e); got != "hello world" {
		t.Errorf("ctrl+c without selection should not modify buffer, got %q", got)
	}
}

func TestEditorPasteViaCtrlV(t *testing.T) {
	e := newEditor("hello", 0, 0)

	// ctrl+v relies on system clipboard; just ensure it doesn't crash
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+v"})
	// Content may or may not change depending on clipboard state
	_ = editorContent(e)
}

// --- Tab / Indent ---

func TestEditorTabInsert(t *testing.T) {
	e := newEditor("hello", 0, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "tab"})

	got := editorContent(e)
	if got != "    hello" {
		t.Errorf("tab should insert 4 spaces, got %q", got)
	}
}

func TestEditorShiftTabDedent(t *testing.T) {
	e := newEditor("    hello", 0, 4)

	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+tab"})

	got := editorContent(e)
	if got != "hello" {
		t.Errorf("shift+tab should dedent, got %q", got)
	}
}

func TestEditorShiftTabNoIndent(t *testing.T) {
	e := newEditor("hello", 0, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+tab"})

	got := editorContent(e)
	if got != "hello" {
		t.Errorf("shift+tab on non-indented line should be no-op, got %q", got)
	}
}

func TestEditorCtrlBracketIndent(t *testing.T) {
	e := newEditor("hello", 0, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+]"})

	got := editorContent(e)
	if got != "    hello" {
		t.Errorf("ctrl+] should indent, got %q", got)
	}
}

// --- Undo / Redo ---

func TestEditorUndoRedoMultiple(t *testing.T) {
	e := newEditor("abc", 0, 0)

	// Insert x then y (undo may group consecutive char inserts)
	e, _ = e.Update(tea.KeyPressMsg{Text: "x"})
	e, _ = e.Update(tea.KeyPressMsg{Text: "y"})
	if got := editorContent(e); got != "xyabc" {
		t.Fatalf("after inserts got %q", got)
	}

	// Undo — may undo both chars as a group
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+z"})
	afterUndo := editorContent(e)
	if afterUndo != "xabc" && afterUndo != "abc" {
		t.Errorf("after undo got %q, expected xabc or abc", afterUndo)
	}

	// If grouped, one more undo is a no-op or further undo
	if afterUndo == "xabc" {
		e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+z"})
		if got := editorContent(e); got != "abc" {
			t.Errorf("after second undo got %q", got)
		}
	}

	// Redo
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+y"})
	afterRedo := editorContent(e)
	// Should restore at least one insert
	if afterRedo == "abc" {
		t.Error("redo should restore content")
	}

	// ctrl+shift+z alias for redo
	e2 := newEditor("test", 0, 0)
	e2, _ = e2.Update(tea.KeyPressMsg{Text: "z"})
	e2, _ = e2.Update(tea.KeyPressMsg{Text: "ctrl+z"})
	e2, _ = e2.Update(tea.KeyPressMsg{Text: "ctrl+shift+z"})
	if got := editorContent(e2); got != "ztest" {
		t.Errorf("ctrl+shift+z redo got %q", got)
	}
}

// --- Navigation ---

func TestEditorCtrlLeftRight(t *testing.T) {
	e := newEditor("hello world foo", 0, 0)

	// Move word right
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+right"})
	if e.Buffer.Cursor.Col == 0 {
		t.Error("ctrl+right should move cursor right from start")
	}

	// Move word left
	origCol := e.Buffer.Cursor.Col
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+left"})
	if e.Buffer.Cursor.Col >= origCol {
		t.Error("ctrl+left should move cursor left")
	}
}

func TestEditorCtrlHomeEnd(t *testing.T) {
	e := newEditor("line1\nline2\nline3", 1, 3)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+home"})
	if e.Buffer.Cursor.Line != 0 || e.Buffer.Cursor.Col != 0 {
		t.Errorf("ctrl+home should go to doc start, got %v", e.Buffer.Cursor)
	}

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+end"})
	if e.Buffer.Cursor.Line != 2 {
		t.Errorf("ctrl+end should go to last line, got line %d", e.Buffer.Cursor.Line)
	}
}

func TestEditorPageUpDown(t *testing.T) {
	content := "line0\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9"
	e := newEditor(content, 0, 0)
	e.SetSize(80, 3) // small viewport

	e, _ = e.Update(tea.KeyPressMsg{Text: "pgdown"})
	if e.Buffer.Cursor.Line == 0 {
		t.Error("pgdown should move cursor down")
	}

	prevLine := e.Buffer.Cursor.Line
	e, _ = e.Update(tea.KeyPressMsg{Text: "pgup"})
	if e.Buffer.Cursor.Line >= prevLine {
		t.Error("pgup should move cursor up")
	}
}

// --- Selection ---

func TestEditorShiftUpDown(t *testing.T) {
	e := newEditor("line1\nline2\nline3", 1, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+down"})
	if e.Buffer.Selection == nil {
		t.Fatal("shift+down should create selection")
	}

	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+up"})
	// Selection should still exist (back to original)
}

func TestEditorCtrlShiftLeftRight(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+shift+left"})
	if e.Buffer.Selection == nil {
		t.Fatal("ctrl+shift+left should create selection")
	}

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+shift+right"})
	// Selection should still exist
}

func TestEditorShiftHomeEnd(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+home"})
	if e.Buffer.Selection == nil {
		t.Fatal("shift+home should create selection")
	}

	e = newEditor("hello world", 0, 5)
	e, _ = e.Update(tea.KeyPressMsg{Text: "shift+end"})
	if e.Buffer.Selection == nil {
		t.Fatal("shift+end should create selection")
	}
}

func TestEditorCtrlShiftHomeEnd(t *testing.T) {
	e := newEditor("line1\nline2\nline3", 1, 3)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+shift+home"})
	if e.Buffer.Selection == nil {
		t.Fatal("ctrl+shift+home should create selection")
	}

	e = newEditor("line1\nline2\nline3", 1, 3)
	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+shift+end"})
	if e.Buffer.Selection == nil {
		t.Fatal("ctrl+shift+end should create selection")
	}
}

func TestEditorCtrlDSelectNextOccurrence(t *testing.T) {
	e := newEditor("foo bar foo", 0, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+d"})
	// Should attempt to select next occurrence; just ensure no crash
}

func TestEditorCtrlLSelectLine(t *testing.T) {
	e := newEditor("hello\nworld", 0, 2)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+l"})
	// Should select the current line; just ensure no crash
}

// --- Editing shortcuts ---

func TestEditorCtrlBackspaceDeleteWord(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+backspace"})
	got := editorContent(e)
	if got == "hello world" {
		t.Error("ctrl+backspace should delete word backward")
	}
}

func TestEditorCtrlDeleteWord(t *testing.T) {
	e := newEditor("hello world", 0, 5)

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+delete"})
	got := editorContent(e)
	if got == "hello world" {
		t.Error("ctrl+delete should delete word forward")
	}
}

func TestEditorAltUpDownMoveLine(t *testing.T) {
	e := newEditor("line1\nline2\nline3", 0, 0)

	e, _ = e.Update(tea.KeyPressMsg{Text: "alt+down"})
	lines := string(e.Buffer.Line(0))
	if lines == "line1" {
		t.Error("alt+down should move current line down")
	}

	e, _ = e.Update(tea.KeyPressMsg{Text: "alt+up"})
	// Should move back; just ensure no crash
}

func TestEditorAltShiftUpDownDuplicateLine(t *testing.T) {
	e := newEditor("hello\nworld", 0, 0)
	origLineCount := e.Buffer.LineCount()

	e, _ = e.Update(tea.KeyPressMsg{Text: "alt+shift+down"})
	if e.Buffer.LineCount() <= origLineCount {
		t.Error("alt+shift+down should duplicate line")
	}

	e = newEditor("hello\nworld", 1, 0)
	origLineCount = e.Buffer.LineCount()
	e, _ = e.Update(tea.KeyPressMsg{Text: "alt+shift+up"})
	if e.Buffer.LineCount() <= origLineCount {
		t.Error("alt+shift+up should duplicate line")
	}
}

func TestEditorCtrlShiftKDeleteLine(t *testing.T) {
	e := newEditor("line1\nline2\nline3", 1, 0)
	origLineCount := e.Buffer.LineCount()

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+shift+k"})
	if e.Buffer.LineCount() >= origLineCount {
		t.Error("ctrl+shift+k should delete line")
	}
}

func TestEditorEnterAutoIndent(t *testing.T) {
	e := newEditor("    hello", 0, 9)

	e, _ = e.Update(tea.KeyPressMsg{Text: "enter"})
	got := editorContent(e)
	// Auto-indent should add newline with indentation
	if got == "    hello\n" {
		t.Error("auto-indent should preserve indentation on new line")
	}
}

func TestEditorEnterNoAutoIndent(t *testing.T) {
	e := newEditor("    hello", 0, 9)
	e.Config.AutoIndent = false

	e, _ = e.Update(tea.KeyPressMsg{Text: "enter"})
	got := editorContent(e)
	if got != "    hello\n" {
		t.Errorf("without auto-indent, expected plain newline, got %q", got)
	}
}

func TestEditorEscapeHidesOverlays(t *testing.T) {
	e := newEditor("hello", 0, 0)
	e.ShowHover("some hover")

	e, _ = e.Update(tea.KeyPressMsg{Text: "escape"})
	if e.HoverView() != "" {
		t.Error("escape should hide hover")
	}
}

// --- Auto-close brackets ---

func TestEditorAutoCloseBracket(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"open paren", "(", "()"},
		{"open brace", "{", "{}"},
		{"open bracket", "[", "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEditor("", 0, 0)
			e, _ = e.Update(tea.KeyPressMsg{Text: tt.input})
			got := editorContent(e)
			if got != tt.expect {
				t.Errorf("auto-close: got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestEditorSkipClosingBracket(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		cursor int
	}{
		{"close paren", ")", 1},
		{"close brace", "}", 1},
		{"close bracket", "]", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Position cursor before a matching close bracket
			open := map[string]string{")": "()", "}": "{}", "]": "[]"}
			e := newEditor(open[tt.input], 0, 1) // between open and close
			e, _ = e.Update(tea.KeyPressMsg{Text: tt.input})
			if e.Buffer.Cursor.Col != 2 {
				t.Errorf("should skip over closing bracket, cursor at col %d", e.Buffer.Cursor.Col)
			}
			// Content should be unchanged
			got := editorContent(e)
			if got != open[tt.input] {
				t.Errorf("content should not change, got %q", got)
			}
		})
	}
}

func TestEditorBackspaceBetweenBrackets(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"parens", "()"},
		{"braces", "{}"},
		{"brackets", "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEditor(tt.content, 0, 1) // cursor between brackets
			e, _ = e.Update(tea.KeyPressMsg{Text: "backspace"})
			got := editorContent(e)
			if got != "" {
				t.Errorf("backspace between %s: expected empty, got %q", tt.name, got)
			}
		})
	}
}

// --- Mouse motion (drag selection) ---

func TestEditorMouseDrag(t *testing.T) {
	e := newEditor("hello world", 0, 0)

	// Start drag with left click
	e, _ = e.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      5,
		Y:      0,
	})

	if !e.dragging {
		t.Fatal("left click should start drag")
	}

	// Drag to extend selection
	e, _ = e.Update(tea.MouseMotionMsg{
		X: 10,
		Y: 0,
	})

	if e.Buffer.Selection == nil {
		t.Error("mouse motion while dragging should create selection")
	}
}

func TestEditorMouseMotionWithoutDrag(t *testing.T) {
	e := newEditor("hello world", 0, 0)

	e, _ = e.Update(tea.MouseMotionMsg{
		X: 10,
		Y: 0,
	})

	if e.Buffer.Selection != nil {
		t.Error("mouse motion without dragging should not create selection")
	}
}

func TestEditorMouseRelease(t *testing.T) {
	e := newEditor("hello world", 0, 0)
	e.dragging = true

	e, _ = e.Update(tea.MouseReleaseMsg{})

	if e.dragging {
		t.Error("mouse release should stop dragging")
	}
}

// --- Shift+Click selection ---

func TestEditorShiftClick(t *testing.T) {
	e := newEditor("hello world", 0, 0)

	// Click normally first
	e, _ = e.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      5,
		Y:      0,
	})

	// Shift-click cannot be tested directly via MouseClickMsg Mod field,
	// but we can verify the feature path doesn't crash
}

// --- Autocomplete tab selection ---

func TestEditorAutocompleteTabSelect(t *testing.T) {
	e := newEditor("hello", 0, 0)
	e.autocomplete.Show([]AutocompleteItem{
		{Label: "foobar", InsertText: "foobar"},
	})

	e, _ = e.Update(tea.KeyPressMsg{Text: "tab"})

	if e.autocomplete.Visible {
		t.Error("tab should accept autocomplete and hide it")
	}
	got := editorContent(e)
	if got != "foobarhello" {
		t.Errorf("tab should insert autocomplete text, got %q", got)
	}
}

// --- Comment toggle ---

func TestEditorCommentToggleRemove(t *testing.T) {
	e := newEditor("// hello", 0, 0)
	e.Config.CommentPrefix = "//"

	e, _ = e.Update(tea.KeyPressMsg{Text: "ctrl+/"})

	got := editorContent(e)
	if got != "hello" {
		t.Errorf("toggle comment should remove comment prefix, got %q", got)
	}
}

// --- Edited flag triggers retokenize ---

func TestEditorEditedTriggers(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"backspace", "backspace"},
		{"delete", "delete"},
		{"tab", "tab"},
		{"shift+tab", "shift+tab"},
		{"ctrl+z", "ctrl+z"},
		{"ctrl+y", "ctrl+y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEditor("    hello world", 0, 5)
			e.Buffer.FilePath = "test.go"
			e = New(e.Buffer, ui.DefaultTheme(), e.Config)
			e.SetSize(80, 24)
			e.Buffer.Cursor = text.Position{Line: 0, Col: 5}

			_, cmd := e.Update(tea.KeyPressMsg{Text: tt.key})
			// Editing keys should produce a retokenize command when highlighter exists
			if cmd == nil {
				t.Errorf("%s: expected non-nil cmd for retokenize", tt.name)
			}
		})
	}
}

// --- TriggerCompletion ---

func TestEditorTriggerCompletionNoLSP(t *testing.T) {
	e := newEditor("hello", 0, 5)
	e.HasLSP = false

	cmd := e.TriggerCompletion()
	if cmd != nil {
		t.Error("should return nil without LSP")
	}
}

func TestEditorTriggerCompletionWithLSP(t *testing.T) {
	e := newEditor("hello", 0, 5)
	e.HasLSP = true
	e.Buffer.FilePath = "test.go"

	cmd := e.TriggerCompletion()
	if cmd == nil {
		t.Error("should return command when cursor is after identifier char")
	}
}

func TestEditorTriggerCompletionAtDot(t *testing.T) {
	e := newEditor("foo.", 0, 4)
	e.HasLSP = true
	e.Buffer.FilePath = "test.go"

	cmd := e.TriggerCompletion()
	if cmd == nil {
		t.Error("should trigger completion after dot")
	}
}

func TestEditorTriggerCompletionAtLineStart(t *testing.T) {
	e := newEditor("hello", 0, 0)
	e.HasLSP = true
	e.Buffer.FilePath = "test.go"

	cmd := e.TriggerCompletion()
	if cmd != nil {
		t.Error("should not trigger completion at line start")
	}
}
