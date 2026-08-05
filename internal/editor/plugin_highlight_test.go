package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/text"
	"teak/internal/ui"
)

var benchmarkPluginHighlightViewSink string

func BenchmarkEditorViewWithoutPluginHighlights(b *testing.B) {
	const lineCount = 4096
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("x\n", lineCount)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(80, 24)
	ed.Viewport.ScrollY = lineCount - 24

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkPluginHighlightViewSink = ed.View()
	}
}

func TestPluginHighlightRangesAreVersionBoundAndDeterministic(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\nworld"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.ReplacePluginHighlights(9, []HighlightRange{{Line: 1, StartCol: 0, EndCol: 5}})
	ed.ReplacePluginHighlights(3, []HighlightRange{{Line: 0, StartCol: 0, EndCol: 5}})
	got := ed.PluginHighlightRanges()
	if len(got) != 2 || got[0].Namespace != 3 || got[1].Namespace != 9 {
		t.Fatalf("PluginHighlightRanges() = %#v, want namespace order 3,9", got)
	}
	buf.InsertAtCursor([]byte("!"))
	if got := ed.PluginHighlightRanges(); len(got) != 0 {
		t.Fatalf("stale PluginHighlightRanges() = %#v, want empty", got)
	}
}

func TestPluginHighlightProjectionReturnsOnlySparseVisibleLines(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("line\n", 100)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.ReplacePluginHighlights(9, []HighlightRange{
		{Line: 98, StartCol: 2, EndCol: 4},
		{Line: 50, StartCol: 0, EndCol: 4},
	})
	ed.ReplacePluginHighlights(3, []HighlightRange{
		{Line: 99, StartCol: 0, EndCol: 4},
		{Line: 0, StartCol: 0, EndCol: 4},
		{Line: 98, StartCol: 0, EndCol: 2},
	})

	got := ed.pluginHighlightRangesForProjection([]int{0, 98, 99}, 0, 99)
	if len(got) != 4 {
		t.Fatalf("visible plugin projection = %#v, want four ranges", got)
	}
	want := []struct {
		namespace int
		line      int
		start     int
	}{
		{namespace: 3, line: 0, start: 0},
		{namespace: 3, line: 98, start: 0},
		{namespace: 9, line: 98, start: 2},
		{namespace: 3, line: 99, start: 0},
	}
	for i, expected := range want {
		if got[i].Namespace != expected.namespace || got[i].Line != expected.line || got[i].StartCol != expected.start {
			t.Fatalf("visible plugin projection[%d] = %#v, want namespace=%d line=%d start=%d", i, got[i], expected.namespace, expected.line, expected.start)
		}
	}
}

func TestReplacePluginHighlightsPreservesCopiedEditor(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.ReplacePluginHighlights(1, []HighlightRange{{Line: 0, EndCol: 1}})
	previous := ed

	ed.ReplacePluginHighlights(2, []HighlightRange{{Line: 0, StartCol: 1, EndCol: 2}})
	if got := ed.PluginHighlightRanges(); len(got) != 2 {
		t.Fatalf("updated editor ranges = %#v, want two namespaces", got)
	}
	got := previous.PluginHighlightRanges()
	if len(got) != 1 || got[0].Namespace != 1 {
		t.Fatalf("copied editor ranges changed through shared map: %#v", got)
	}
}

func TestReplacePluginHighlightsDoesNotReviveStaleNamespaces(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.ReplacePluginHighlights(1, []HighlightRange{{Line: 0, EndCol: 1}})
	buf.InsertAtCursor([]byte("!"))
	if got := ed.PluginHighlightRanges(); got != nil {
		t.Fatalf("stale plugin highlights = %#v, want nil", got)
	}

	ed.ReplacePluginHighlights(2, []HighlightRange{{Line: 0, StartCol: 1, EndCol: 2}})
	got := ed.PluginHighlightRanges()
	if len(got) != 1 || got[0].Namespace != 2 {
		t.Fatalf("replacement revived stale namespaces: %#v", got)
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

func BenchmarkEditorViewFourThousandPluginHighlights(b *testing.B) {
	const lineCount = 4096
	buf := text.NewBufferFromBytes([]byte(strings.Repeat("x\n", lineCount)))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(80, 24)
	ed.Viewport.ScrollY = lineCount - 24
	for namespace := 1; namespace <= 8; namespace++ {
		ranges := make([]HighlightRange, 512)
		for i := range ranges {
			line := (namespace-1)*len(ranges) + i
			ranges[i] = HighlightRange{Namespace: namespace, Line: line, EndCol: 1}
		}
		ed.ReplacePluginHighlights(namespace, ranges)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkPluginHighlightViewSink = ed.View()
	}
}

func BenchmarkReplacePluginHighlightsAtRequestLimit(b *testing.B) {
	buf := text.NewBufferFromBytes([]byte("x\n"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	for namespace := 1; namespace <= 7; namespace++ {
		ed.ReplacePluginHighlights(namespace, []HighlightRange{{Line: namespace, EndCol: 1}})
	}
	ranges := make([]HighlightRange, 512)
	for i := range ranges {
		ranges[i] = HighlightRange{Line: len(ranges) - i, EndCol: 1}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ed.ReplacePluginHighlights(8, ranges)
	}
}
