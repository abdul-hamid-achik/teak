package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestTabStopsMapRawAndVisualColumns(t *testing.T) {
	raw := []byte("a\tb")
	if got := displayColumn(raw, 2, 4); got != 4 {
		t.Fatalf("displayColumn(after tab) = %d, want 4", got)
	}
	if got := expandTabsForDisplay(raw, 4); got != "a   b" {
		t.Fatalf("expandTabsForDisplay() = %q, want %q", got, "a   b")
	}
	if got := byteColumnAtDisplay(raw, 1, 4); got != 1 {
		t.Fatalf("byteColumnAtDisplay(tab start) = %d, want 1", got)
	}
	if got := byteColumnAtDisplay(raw, 3, 4); got != 2 {
		t.Fatalf("byteColumnAtDisplay(tab end) = %d, want 2", got)
	}
}

func TestEditorTabsRenderAndMouseRoundTrip(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("a\tb"))
	cfg := DefaultConfig()
	cfg.TabSize = 4
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.SetSize(20, 3)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 2})

	x, y := ed.CursorPosition()
	if got, want := x, ed.effectiveGutterWidth()+4; got != want {
		t.Fatalf("cursor x = %d, want %d", got, want)
	}
	if got := ed.screenToBuffer(x, y); got != ed.Buffer.Cursor {
		t.Fatalf("screenToBuffer(%d,%d) = %#v, want %#v", x, y, got, ed.Buffer.Cursor)
	}
	if got := ansi.Strip(ed.View()); !strings.Contains(got, "a   b") {
		t.Fatalf("rendered view %q does not contain display-expanded tab", got)
	}
}

func TestTabExpansionPreservesTokenAndSelectionRendering(t *testing.T) {
	theme := ui.DefaultTheme()
	v := Viewport{TabSize: 4, Width: 20, Height: 1}
	tokens := []highlight.StyledToken{
		{Text: "a\t", Style: lipgloss.NewStyle().Bold(true)},
		{Text: "b", Style: lipgloss.NewStyle().Italic(true)},
	}
	if got := ansi.Strip(v.renderLineWithTokens(tokens, false, 8, theme)); !strings.HasPrefix(got, "a   b") {
		t.Fatalf("token rendering = %q, want display-expanded tab", got)
	}

	buf := text.NewBufferFromBytes([]byte("a\tb"))
	buf.SetSelection(text.Position{Line: 0, Col: 1}, text.Position{Line: 0, Col: 2})
	got := v.Render(buf, theme, nil, nil, nil)
	if want := theme.Selection.Render("   "); !strings.Contains(got, want) {
		t.Fatalf("tab selection is not rendered as selected spaces:\n%s", got)
	}
	if selected := string(buf.SelectedText()); selected != "\t" {
		t.Fatalf("SelectedText() = %q, want raw tab", selected)
	}
}

func TestTabsParticipateInWordWrapAndHitTesting(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("a\tb"))
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 4, 4)
	if got := wrap.LineRows(0); got != 2 {
		t.Fatalf("LineRows() = %d, want 2", got)
	}
	v := Viewport{TabSize: 4}
	if got := v.ScreenToBufferPositionWrap(0, 1, buf, 0, wrap); got != (text.Position{Line: 0, Col: 2}) {
		t.Fatalf("wrapped click on b = %#v, want byte column 2", got)
	}
}

func TestSliceDisplayColumnsHandlesInvalidUTF8(t *testing.T) {
	raw := string([]byte{0xff})
	if got := sliceDisplayColumns(raw, 0, 1); got != raw {
		t.Fatalf("sliceDisplayColumns() = %q, want original invalid byte", got)
	}
}

func TestNarrowWrapClipsWideTabToTextWidth(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("\t"))
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 1, 4)
	v := Viewport{TabSize: 4, Width: 8, Height: 1}

	rendered := v.RenderWithWrap(buf, ui.DefaultTheme(), nil, nil, nil, wrap)
	maxWidth := v.GutterWidth + wrap.Width()
	if got := ansi.StringWidth(rendered); got > maxWidth {
		t.Fatalf("wrapped tab width = %d, want <= %d", got, maxWidth)
	}

	_, displayCol := wrappedPositionWithTabs("\t", 1, 1, 4)
	if displayCol > wrap.Width() {
		t.Fatalf("cursor display column = %d, want <= wrap width %d", displayCol, wrap.Width())
	}
}

func TestEditorWrappedTabCursorRoundTrip(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("a\tb"))
	cfg := DefaultConfig()
	cfg.WordWrap = true
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.Wrap = NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 4, cfg.TabSize)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 2})

	x, y := ed.CursorPosition()
	if y != 1 {
		t.Fatalf("wrapped tab cursor row = %d, want 1", y)
	}
	if got := ed.screenToBuffer(x, y); got != ed.Buffer.Cursor {
		t.Fatalf("screenToBuffer(%d, %d) = %#v, want %#v", x, y, got, ed.Buffer.Cursor)
	}

	ed.Viewport.ensureCursorVisible(ed.Buffer, ed.Buffer.Cursor, 4)
	if ed.Viewport.ScrollX != 1 {
		t.Fatalf("ScrollX = %d, want 1 at display column 4", ed.Viewport.ScrollX)
	}
}

func TestTabsKeepSelectionStylingWhenWrapped(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("a\tb"))
	buf.SetSelection(text.Position{Line: 0, Col: 1}, text.Position{Line: 0, Col: 2})
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 4, 4)
	v := Viewport{TabSize: 4, Height: 2}
	got := v.RenderWithWrap(buf, ui.DefaultTheme(), nil, nil, nil, wrap)
	if want := ui.DefaultTheme().Selection.Render("   "); !strings.Contains(got, want) {
		t.Fatalf("wrapped tab selection is not styled:\n%s", got)
	}
}
