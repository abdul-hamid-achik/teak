package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestTriggerCompletionOnIdentifier(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt.Pr"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 6 // After "fmt.Pr"
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command for identifier character")
	}
}

func TestTriggerCompletionOnDot(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt."))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 4 // After "fmt."
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command for '.' character")
	}
}

func TestTriggerCompletionOnColon(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("pkg::"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 4 // After "pkg:"
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command for ':' character")
	}
}

func TestTriggerCompletionNoLSP(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt.P"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 5
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = false // No LSP

	cmd := ed.TriggerCompletion()
	if cmd != nil {
		t.Error("TriggerCompletion should return nil when LSP is not enabled")
	}
}

func TestTriggerCompletionNoFilePath(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt.P"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 5
	// No file path

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd != nil {
		t.Error("TriggerCompletion should return nil when no file path")
	}
}

func TestTriggerCompletionAtLineStart(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("P"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 1 // After "P"
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command at line start")
	}
}

func TestTriggerCompletionOnWhitespace(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt.P "))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 7 // After space
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd != nil {
		t.Error("TriggerCompletion should return nil for whitespace")
	}
}

func TestTriggerCompletionOnNewline(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("fmt.P\n"))
	buf.Cursor.Line = 1
	buf.Cursor.Col = 0 // At start of new line
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd != nil {
		t.Error("TriggerCompletion should return nil at start of empty line")
	}
}

func TestTriggerCompletionUnderscore(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("my_var"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 6 // After "my_var"
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command for underscore")
	}
}

func TestTriggerCompletionNumber(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("var123"))
	buf.Cursor.Line = 0
	buf.Cursor.Col = 6 // After "var123"
	buf.FilePath = "test.go"

	ed := New(buf, ui.DefaultTheme(), Config{})
	ed.HasLSP = true

	cmd := ed.TriggerCompletion()
	if cmd == nil {
		t.Error("TriggerCompletion should return command for number")
	}
}
