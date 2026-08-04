package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestDiagnosticHighlightsUnderlineSingleLineRange(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("let x = 1\nlet y = 2\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Diagnostics = []Diagnostic{
		{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 5, Severity: 1, Message: "undefined"},
	}

	highlights := ed.diagnosticHighlights()
	if len(highlights) != 1 {
		t.Fatalf("diagnosticHighlights() = %d ranges, want 1", len(highlights))
	}
	h := highlights[0]
	if h.Line != 0 || h.StartCol != 4 || h.EndCol != 5 {
		t.Fatalf("highlight = line %d cols %d-%d, want line 0 cols 4-5", h.Line, h.StartCol, h.EndCol)
	}
	theme := ui.DefaultTheme()
	if got, want := h.Style.Render("x"), theme.DiagError.Render("x"); got != want {
		t.Errorf("error highlight render = %q, want the DiagError style %q", got, want)
	}
}

func TestDiagnosticHighlightsUseSeverityStyles(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("aaa\nbbb\nccc\nddd\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Diagnostics = []Diagnostic{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Severity: 1},
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 1, Severity: 2},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 1, Severity: 3},
		{StartLine: 3, StartCol: 0, EndLine: 3, EndCol: 1, Severity: 4},
	}

	theme := ui.DefaultTheme()
	want := []string{
		theme.DiagError.Render("x"),
		theme.DiagWarning.Render("x"),
		theme.DiagInfo.Render("x"),
		theme.DiagHint.Render("x"),
	}
	highlights := ed.diagnosticHighlights()
	if len(highlights) != 4 {
		t.Fatalf("diagnosticHighlights() = %d ranges, want 4", len(highlights))
	}
	for i, h := range highlights {
		if got := h.Style.Render("x"); got != want[i] {
			t.Errorf("severity %d highlight render = %q, want %q", i+1, got, want[i])
		}
	}
}

func TestDiagnosticHighlightsSpanMultipleLines(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("start line\nmiddle line\nend line\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Diagnostics = []Diagnostic{
		{StartLine: 0, StartCol: 6, EndLine: 2, EndCol: 3, Severity: 2},
	}

	highlights := ed.diagnosticHighlights()
	if len(highlights) != 3 {
		t.Fatalf("diagnosticHighlights() = %d ranges, want 3 (one per covered line)", len(highlights))
	}
	// First line: from StartCol to end of line ("start line" is 10 bytes).
	if highlights[0].Line != 0 || highlights[0].StartCol != 6 || highlights[0].EndCol != 10 {
		t.Errorf("first line range = %d:%d-%d, want 0:6-10", highlights[0].Line, highlights[0].StartCol, highlights[0].EndCol)
	}
	// Middle line: whole line ("middle line" is 11 bytes without the newline).
	if highlights[1].Line != 1 || highlights[1].StartCol != 0 || highlights[1].EndCol != 11 {
		t.Errorf("middle line range = %d:%d-%d, want 1:0-11", highlights[1].Line, highlights[1].StartCol, highlights[1].EndCol)
	}
	// Last line: from column 0 to EndCol.
	if highlights[2].Line != 2 || highlights[2].StartCol != 0 || highlights[2].EndCol != 3 {
		t.Errorf("last line range = %d:%d-%d, want 2:0-3", highlights[2].Line, highlights[2].StartCol, highlights[2].EndCol)
	}
}

func TestDiagnosticHighlightsBoundedToViewport(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("aaa\nbbb\nccc\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 1)
	ed.Viewport.ScrollY = 2
	ed.Diagnostics = []Diagnostic{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Severity: 1},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 1, Severity: 1},
	}

	highlights := ed.diagnosticHighlights()
	if len(highlights) != 1 {
		t.Fatalf("diagnosticHighlights() with scrolled viewport = %d ranges, want 1", len(highlights))
	}
	if highlights[0].Line != 2 {
		t.Fatalf("highlight on line %d, want line 2", highlights[0].Line)
	}
}

func TestDiagnosticHighlightsEmptyWithoutDiagnostics(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("aaa\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	if highlights := ed.diagnosticHighlights(); len(highlights) != 0 {
		t.Fatalf("diagnosticHighlights() without diagnostics = %d ranges, want 0", len(highlights))
	}
}

func TestViewRendersDiagnosticUnderlines(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("let x = 1\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	plain := ed.View()

	ed.Diagnostics = []Diagnostic{
		{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 5, Severity: 1, Message: "undefined"},
	}
	withDiag := ed.View()
	if withDiag == plain {
		t.Fatal("view with diagnostics identical to plain view — ranges are not underlined")
	}
}
