package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

func findLayoutTestEditor(t *testing.T) Editor {
	t.Helper()
	buf := NewBufferFromBytesHelper([]byte("hello\nworld\nagain\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	return ed
}

// NewBufferFromBytesHelper avoids importing the text package name collision
// in this small test file.
func NewBufferFromBytesHelper(data []byte) *text.Buffer {
	return text.NewBufferFromBytes(data)
}

func TestCursorPositionOffsetsForFindWidget(t *testing.T) {
	ed := findLayoutTestEditor(t)
	ed.Buffer.SetCursor(text.Position{Line: 1, Col: 2})

	_, yBefore := ed.CursorPosition()
	ed.ShowFind()
	if !ed.IsFindVisible() {
		t.Fatal("ShowFind did not make the widget visible")
	}
	_, yAfter := ed.CursorPosition()

	if yAfter != yBefore+1 {
		t.Fatalf("cursor y with find open = %d, want %d (widget row shifts text down)", yAfter, yBefore+1)
	}

	ed.HideFind()
	_, yClosed := ed.CursorPosition()
	if yClosed != yBefore {
		t.Fatalf("cursor y after closing find = %d, want %d", yClosed, yBefore)
	}
}

func TestMouseClickOnFindWidgetRowDoesNotMoveCursor(t *testing.T) {
	ed := findLayoutTestEditor(t)
	ed.Buffer.SetCursor(text.Position{Line: 2, Col: 1})
	ed.ShowFind()

	// Row 0 of the editor view is the find widget while it is open; a click
	// there must not relocate the text cursor.
	got, _ := ed.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 0})
	if got.Buffer.Cursor != (text.Position{Line: 2, Col: 1}) {
		t.Fatalf("cursor after clicking the find row = %v, want unchanged {2 1}", got.Buffer.Cursor)
	}
}

func TestMouseClickOffsetsForFindWidgetRow(t *testing.T) {
	withFind := findLayoutTestEditor(t)
	withFind.ShowFind()
	// Click on view row 1 with the widget open: row 1 is the first text row.
	gotFind, _ := withFind.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 30, Y: 1})

	plain := findLayoutTestEditor(t)
	gotPlain, _ := plain.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 30, Y: 0})

	if gotFind.Buffer.Cursor.Line != gotPlain.Buffer.Cursor.Line {
		t.Fatalf("click row 1 with find open landed on line %d, want line %d (same as row 0 without find)",
			gotFind.Buffer.Cursor.Line, gotPlain.Buffer.Cursor.Line)
	}
}
