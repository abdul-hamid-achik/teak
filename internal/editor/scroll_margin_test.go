package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func scrollMarginTestEditor(t *testing.T, height, margin int) Editor {
	t.Helper()
	var content []byte
	for i := 0; i < 30; i++ {
		content = append(content, []byte("line of text\n")...)
	}
	buf := text.NewBufferFromBytes(content)
	cfg := DefaultConfig()
	cfg.ScrollMargin = margin
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(50, height)
	return ed
}

func TestEnsureCursorVisibleRespectsScrollMarginMovingDown(t *testing.T) {
	ed := scrollMarginTestEditor(t, 10, 2)

	ed.Buffer.SetCursor(text.Position{Line: 10, Col: 0})
	ed.EnsureCursorVisible()

	// Height 10 with margin 2: the cursor must land at least two rows above
	// the bottom edge. Minimal scroll puts it on visual row 7 (ScrollY 3).
	if ed.Viewport.ScrollY != 3 {
		t.Fatalf("ScrollY = %d, want 3 (cursor two rows above the bottom edge)", ed.Viewport.ScrollY)
	}
}

func TestEnsureCursorVisibleRespectsScrollMarginMovingUp(t *testing.T) {
	ed := scrollMarginTestEditor(t, 10, 2)
	ed.Viewport.ScrollY = 12

	ed.Buffer.SetCursor(text.Position{Line: 12, Col: 0})
	ed.EnsureCursorVisible()

	// Cursor on line 12 with margin 2: scroll up so the cursor sits two rows
	// below the top edge (ScrollY 10).
	if ed.Viewport.ScrollY != 10 {
		t.Fatalf("ScrollY = %d, want 10 (cursor two rows below the top edge)", ed.Viewport.ScrollY)
	}
}

func TestEnsureCursorVisibleMarginClampedAtDocumentStart(t *testing.T) {
	ed := scrollMarginTestEditor(t, 10, 4)

	ed.Viewport.ScrollY = 5
	ed.Buffer.SetCursor(text.Position{Line: 1, Col: 0})
	ed.EnsureCursorVisible()

	// The margin cannot push ScrollY negative at the top of the document.
	if ed.Viewport.ScrollY != 0 {
		t.Fatalf("ScrollY = %d, want 0 at the document start", ed.Viewport.ScrollY)
	}
}

func TestEnsureCursorVisibleMarginCannotExceedHalfViewport(t *testing.T) {
	ed := scrollMarginTestEditor(t, 6, 5) // margin larger than height/2

	ed.Buffer.SetCursor(text.Position{Line: 10, Col: 0})
	ed.EnsureCursorVisible()

	// Degenerate margins clamp to height/2-1 (=2); the cursor still ends up
	// inside the viewport rather than scrolling to an impossible offset.
	row := 10 - ed.Viewport.ScrollY
	if row < 0 || row >= 6 {
		t.Fatalf("cursor row %d outside viewport after oversized margin", row)
	}
}
