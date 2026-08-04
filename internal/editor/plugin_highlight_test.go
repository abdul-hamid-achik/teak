package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestPluginHighlightRangesAreVersionBoundAndDeterministic(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\nworld"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.PluginHighlights = map[int][]HighlightRange{
		9: {{Namespace: 9, Line: 1, StartCol: 0, EndCol: 5}},
		3: {{Namespace: 3, Line: 0, StartCol: 0, EndCol: 5}},
	}
	ed.PluginHighlightVersion = buf.Version()
	got := ed.PluginHighlightRanges()
	if len(got) != 2 || got[0].Namespace != 3 || got[1].Namespace != 9 {
		t.Fatalf("PluginHighlightRanges() = %#v, want namespace order 3,9", got)
	}
	buf.InsertAtCursor([]byte("!"))
	if got := ed.PluginHighlightRanges(); len(got) != 0 {
		t.Fatalf("stale PluginHighlightRanges() = %#v, want empty", got)
	}
}

func TestViewportRendersPluginHighlightRange(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	theme := ui.DefaultTheme()
	viewport := Viewport{Width: 80, Height: 4}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#88c0d0")).Bold(true)
	got := viewport.RenderHighlights(buf, theme, nil, nil, nil, []HighlightRange{{
		Namespace: 1, Line: 0, StartCol: 6, EndCol: 11, Style: style,
	}})
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("highlight render lost text: %q", got)
	}
	if want := style.Background(theme.CursorLine.GetBackground()).Render("world"); !strings.Contains(got, want) {
		t.Fatalf("highlight render does not contain styled range %q:\n%s", want, got)
	}
}

func TestViewportPluginHighlightHonorsHorizontalScroll(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("prefix target suffix"))
	theme := ui.DefaultTheme()
	viewport := Viewport{Width: 20, Height: 2, ScrollX: 7}
	got := ansi.Strip(viewport.RenderHighlights(buf, theme, nil, nil, nil, []HighlightRange{{
		Namespace: 1, Line: 0, StartCol: 7, EndCol: 13,
	}}))
	if !strings.Contains(got, "target") {
		t.Fatalf("scrolled render lost highlighted text: %q", got)
	}
	if strings.Contains(got, "prefix") {
		t.Fatalf("scrolled render still contains hidden prefix: %q", got)
	}
}
