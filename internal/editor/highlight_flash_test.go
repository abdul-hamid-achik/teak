package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/editor/overlays"
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

func TestAcceptingCompletionInvalidatesHighlightCache(t *testing.T) {
	// Both accept paths mutate the buffer without passing through the keystroke
	// epilogue, so each must invalidate the token cache synchronously or the
	// colours stay mapped to pre-edit line indices until the async retokenize.
	paths := []struct {
		name   string
		accept func(Editor) Editor
	}{
		{
			name: "keyboard enter",
			accept: func(e Editor) Editor {
				updated, _ := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				return updated
			},
		},
		{
			name: "mouse selection",
			accept: func(e Editor) Editor {
				e.AutocompleteSelectAt(0)
				return e
			},
		},
		{
			// The server-supplied replacement range takes a different mutation
			// branch than cursor insertion; it must reach the same epilogue.
			name: "mouse selection with edit range",
			accept: func(e Editor) Editor {
				e.Buffer.SetCursor(text.Position{Line: 20, Col: 3})
				e.ShowAutocomplete([]overlays.AutocompleteItem{{
					Label:      "fmtfull",
					InsertText: "fmtfull",
					Edit:       overlays.AutocompleteEdit{StartLine: 20, StartCol: 0, EndLine: 20, EndCol: 3},
					HasEdit:    true,
				}})
				e.AutocompleteSelectAt(0)
				return e
			},
		},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			ed := tokenizedEditor(t, 40)
			ed.Buffer.SetCursor(text.Position{Line: 20, Col: 0})
			ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "fmt", InsertText: "fmt"}})

			updated := path.accept(ed)

			if updated.Highlighter.CoversRange(0, 40) {
				t.Error("highlighter still reports coverage after accepting a completion")
			}
			if !updated.Highlighter.IsDirty() {
				t.Error("highlighter not marked dirty after accepting a completion")
			}
			if got := len(updated.Highlighter.Line(20)); got != 0 {
				t.Errorf("edited line kept %d stale tokens, want 0", got)
			}
			if path.name == "mouse selection with edit range" {
				// Pin that the replacement branch actually ran; a rejected edit
				// range falls back to cursor insertion, which edits line 20 too
				// but leaves the "fun" prefix in place.
				want := "fmtfullc example() error { return nil }"
				if got := string(updated.Buffer.Line(20)); got != want {
					t.Errorf("edited line = %q, want %q (edit range should replace the typed prefix)", got, want)
				}
			}
		})
	}
}
