package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor/overlays"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestHoverDismissesOnCursorNavigation(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})
	ed.ShowHover("func hello()")

	if !ed.hover.Visible {
		t.Fatal("hover not visible after ShowHover")
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if ed.hover.Visible {
		t.Fatal("hover still visible after moving the cursor")
	}
	if ed.Buffer.Cursor.Col != 4 {
		t.Fatalf("cursor col = %d, want 4 (navigation must still move the cursor)", ed.Buffer.Cursor.Col)
	}
}

func TestHoverSurvivesNonNavigationKeys(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})
	ed.ShowHover("func hello()")

	// A non-navigation, non-editing key must not dismiss the popup on its own;
	// typing edits hide it through the edit path and esc dismisses on purpose.
	ed, _ = ed.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if !ed.hover.Visible {
		t.Fatal("hover dismissed by a non-navigation key")
	}
}

// A left-click in the text area moves the cursor, and the hover popup is
// anchored to a buffer position; it must not linger over the new location.
func TestHoverDismissesOnMouseClickInTextArea(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\nsecond line\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})
	ed.ShowHover("func hello()")

	clickX := ed.effectiveGutterWidth() + 1
	ed, _ = ed.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: clickX, Y: 1}))
	if ed.hover.Visible {
		t.Fatal("hover still visible after a left click in the text area")
	}
	if ed.Buffer.Cursor.Line != 1 {
		t.Fatalf("click must still move the cursor: line = %d, want 1", ed.Buffer.Cursor.Line)
	}
}

// Shift-click extends the selection, which moves its head away from the
// anchored popup position; hover and signature help must follow the same
// rule as autocomplete and hide.
func TestHoverAndSignatureDismissOnShiftClick(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\nsecond line\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})
	ed.ShowHover("func hello()")
	ed.ShowSignatureHelp(&overlays.SignatureData{
		Signatures: []overlays.SignatureInfo{{Label: "hello(a, b)"}},
	})

	clickX := ed.effectiveGutterWidth() + 1
	ed, _ = ed.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, Mod: tea.ModShift, X: clickX, Y: 1}))
	if ed.hover.Visible {
		t.Fatal("hover still visible after a shift click")
	}
	if ed.signatureHelp.Visible {
		t.Fatal("signature help still visible after a shift click")
	}
}

// Right-click moves the cursor when nothing is selected, so an anchored
// popup must be dismissed before the context menu opens.
func TestHoverDismissesOnCursorMovingRightClick(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\nsecond line\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 10)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 5})
	ed.ShowHover("func hello()")

	clickX := ed.effectiveGutterWidth() + 1
	ed, _ = ed.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: clickX, Y: 1}))
	if ed.hover.Visible {
		t.Fatal("hover still visible after a cursor-moving right click")
	}
	if !ed.IsContextMenuVisible() {
		t.Fatal("right click must still open the context menu")
	}
}
