package editor

import (
	"testing"

	"teak/internal/editor/overlays"
	"teak/internal/text"
	"teak/internal/ui"
)

// editorWithBuffer returns an editor whose buffer holds content, with the
// cursor placed at (line, col).
func editorWithBuffer(t *testing.T, content string, line, col int) *Editor {
	t.Helper()
	buf := text.NewBufferFromBytes([]byte(content))
	// A file path gives the editor a highlighter, which the retokenize
	// scheduling assertions depend on.
	buf.FilePath = "test.go"
	e := New(buf, ui.DefaultTheme(), DefaultConfig())
	e.Buffer.Cursor = text.Position{Line: line, Col: col}
	return &e
}

func TestApplyCompletionReplacesTypedPrefix(t *testing.T) {
	// The user typed "fm" and the server offers "fmt" with a textEdit covering
	// the prefix. Inserting at the cursor instead of replacing produced "fmfmt".
	e := editorWithBuffer(t, "fm", 0, 2)

	e.applyCompletion(overlays.AutocompleteItem{
		Label:      "fmt",
		InsertText: "fmt",
		HasEdit:    true,
		Edit:       overlays.AutocompleteEdit{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 2},
	})

	if got, want := string(e.Buffer.Line(0)), "fmt"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestApplyCompletionWithoutEditInsertsAtCursor(t *testing.T) {
	// Servers that send only insertText still rely on cursor insertion.
	e := editorWithBuffer(t, "fm", 0, 2)

	e.applyCompletion(overlays.AutocompleteItem{Label: "t", InsertText: "t"})

	if got, want := string(e.Buffer.Line(0)), "fmt"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestApplyCompletionPreservesSurroundingText(t *testing.T) {
	e := editorWithBuffer(t, "x := fm + y", 0, 7)

	e.applyCompletion(overlays.AutocompleteItem{
		Label:      "fmt",
		InsertText: "fmt",
		HasEdit:    true,
		Edit:       overlays.AutocompleteEdit{StartLine: 0, StartCol: 5, EndLine: 0, EndCol: 7},
	})

	if got, want := string(e.Buffer.Line(0)), "x := fmt + y"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestApplyCompletionRejectsStaleRange(t *testing.T) {
	tests := []struct {
		name string
		edit overlays.AutocompleteEdit
	}{
		{"line past end of buffer", overlays.AutocompleteEdit{StartLine: 9, EndLine: 9, EndCol: 2}},
		{"column past end of line", overlays.AutocompleteEdit{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 99}},
		{"inverted range", overlays.AutocompleteEdit{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 1}},
		{"negative column", overlays.AutocompleteEdit{StartLine: 0, StartCol: -1, EndLine: 0, EndCol: 2}},
		{"range does not cover cursor", overlays.AutocompleteEdit{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := editorWithBuffer(t, "fm\nzz", 0, 2)

			// A stale range must degrade to cursor insertion rather than
			// corrupting an unrelated region of the buffer.
			e.applyCompletion(overlays.AutocompleteItem{
				Label:      "fmt",
				InsertText: "t",
				HasEdit:    true,
				Edit:       tc.edit,
			})

			if got, want := string(e.Buffer.Line(0)), "fmt"; got != want {
				t.Errorf("line 0 = %q, want %q (stale edit should fall back to insertion)", got, want)
			}
			if got, want := string(e.Buffer.Line(1)), "zz"; got != want {
				t.Errorf("line 1 = %q, want %q (unrelated line must be untouched)", got, want)
			}
		})
	}
}

func TestAutocompleteSelectAtHonoursTextEdit(t *testing.T) {
	// The mouse path must apply completions identically to the keyboard path.
	e := editorWithBuffer(t, "fm", 0, 2)
	e.ShowAutocomplete([]overlays.AutocompleteItem{{
		Label:      "fmt",
		InsertText: "fmt",
		HasEdit:    true,
		Edit:       overlays.AutocompleteEdit{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 2},
	}})

	cmd, inserted := e.AutocompleteSelectAt(0)
	if !inserted {
		t.Fatal("AutocompleteSelectAt(0) = false, want true")
	}
	// The mouse path must refresh highlighting exactly as the keyboard path does.
	if cmd == nil {
		t.Error("AutocompleteSelectAt returned no retokenize command")
	}
	if got, want := string(e.Buffer.Line(0)), "fmt"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}
