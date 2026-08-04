package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
