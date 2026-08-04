package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func TestStatusColumnCountsRunesNotBytes(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("éx\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(40, 5)

	// é is two bytes but one display column; the cursor after it sits at
	// byte offset 2 yet must be reported as column 2.
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 2})
	if got := ed.StatusColumn(); got != 2 {
		t.Fatalf("StatusColumn() = %d, want 2 (rune column, not byte offset 3)", got)
	}
}

func TestStatusColumnExpandsTabs(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("\tz\n"))
	cfg := DefaultConfig()
	cfg.TabSize = 4
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(40, 5)

	// The tab occupies display columns 1-4; the cursor after it is column 5.
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 1})
	if got := ed.StatusColumn(); got != 5 {
		t.Fatalf("StatusColumn() = %d, want 5 with tab size 4", got)
	}
}
