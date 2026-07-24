package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/text"
	"teak/internal/ui"
)

// tokenizedEditor returns an editor over a Go file whose highlighter has run.
func tokenizedEditor(t *testing.T, lines int) Editor {
	t.Helper()
	var sb strings.Builder
	for range lines {
		sb.WriteString("func example() error { return nil }\n")
	}
	buf := text.NewBufferFromBytes([]byte(sb.String()))
	buf.FilePath = "main.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(100, 30)
	ed.Highlighter.Tokenize(buf.Bytes())
	return ed
}

func TestTypingKeepsSyntaxColourOnUneditedLines(t *testing.T) {
	ed := tokenizedEditor(t, 40)
	ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})

	before := len(ed.Highlighter.Line(0))
	if before == 0 {
		t.Fatal("setup: expected tokens on line 0")
	}

	updated, _ := ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	// Discarding the whole token cache on every keystroke made the entire
	// viewport flash from syntax-coloured to plain text until the async
	// tokenization landed ~150ms later.
	if got := len(updated.Highlighter.Line(0)); got != before {
		t.Errorf("line 0 has %d tokens after typing on line 20, want %d retained", got, before)
	}
}

func TestTypingDropsTokensOnlyForTheEditedLine(t *testing.T) {
	ed := tokenizedEditor(t, 40)
	ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})

	updated, _ := ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	// The edited line's tokens no longer describe its content, so they must go.
	if got := len(updated.Highlighter.Line(20)); got != 0 {
		t.Errorf("edited line kept %d stale tokens, want 0", got)
	}
}

func TestTypingStillSchedulesRetokenization(t *testing.T) {
	ed := tokenizedEditor(t, 40)
	ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})

	updated, cmd := ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	// Retaining tokens must not convince the editor the range is fresh, or the
	// stale colours would never be corrected.
	if updated.Highlighter.CoversRange(0, 40) {
		t.Error("highlighter still reports coverage after an edit")
	}
	if !updated.Highlighter.IsDirty() {
		t.Error("highlighter not marked dirty after an edit")
	}
	if cmd == nil {
		t.Error("no command returned after an edit; retokenization would never run")
	}
}

func TestPressingEnterShiftsRetainedTokensDown(t *testing.T) {
	ed := tokenizedEditor(t, 40)
	ed.Buffer.SetCursor(text.Position{Line: 5, Col: 0})

	before := ed.Highlighter.Line(20)
	if len(before) == 0 {
		t.Fatal("setup: expected tokens on line 20")
	}
	firstText := before[0].Text

	updated, _ := ed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	after := updated.Highlighter.Line(21)
	if len(after) == 0 {
		t.Fatal("tokens for the line pushed from 20 to 21 were lost")
	}
	if after[0].Text != firstText {
		t.Errorf("line 21 first token = %q, want %q from the line that moved down",
			after[0].Text, firstText)
	}
}
