package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/editor/overlays"
	"teak/internal/text"
)

func TestLSPOverlayPlacementStaysInsideEditorBody(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 50
	m.height = 12
	m.relayout()
	ed := m.activeEditor()
	ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "候補🎉", Detail: "詳細", InsertText: "candidate"}})

	p, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView())
	if !ok {
		t.Fatal("lspOverlayPlacement() = not visible")
	}
	body := m.mouseLayout().editorBody
	if p.x < body.x || p.y < body.y || p.x+p.width > body.x+body.width || p.y+p.height > body.y+body.height {
		t.Fatalf("placement %#v escapes editor body %#v", p, body)
	}

	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "候補🎉") {
		t.Fatalf("model view does not render autocomplete: %q", ansi.Strip(view))
	}
	for _, line := range strings.Split(view, "\n") {
		if got := ansi.StringWidth(line); got > m.width {
			t.Fatalf("rendered width = %d, terminal width = %d: %q", got, m.width, line)
		}
	}
}

func TestLSPOverlayPlacementUsesSpaceAboveCursorWhenNeeded(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width = 60
	m.height = 8
	m.relayout()
	ed := m.activeEditor()
	ed.Buffer = text.NewBufferFromBytes([]byte("0\n1\n2\n3\n4\n5"))
	ed.Buffer.SetCursor(text.Position{Line: 5})
	ed.EnsureCursorVisible()
	ed.ShowHover("one\ntwo\nthree")

	p, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView())
	if !ok {
		t.Fatal("lspOverlayPlacement() = not visible")
	}
	_, cursorY := ed.CursorPosition()
	body := m.mouseLayout().editorBody
	if p.y+p.height > body.y+cursorY {
		t.Fatalf("overlay %#v overlaps bottom cursor at y=%d; body=%#v", p, body.y+cursorY, body)
	}
}

func TestLSPOverlayIsSuppressedWhenEditorHasNoFreeRow(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width = 40
	m.height = 3
	m.relayout()
	ed := m.activeEditor()
	ed.ShowHover("hidden in one-row editor")

	if _, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView()); ok {
		t.Fatal("lsp overlay should not replace the sole editor row")
	}
}

func TestLSPOverlayClickDoesNotEditTextBehindPopup(t *testing.T) {
	m := newViewTestModel(t, false)
	ed := m.activeEditor()
	ed.ShowAutocomplete([]overlays.AutocompleteItem{{Label: "completion", InsertText: "completion"}})
	p, ok := m.lspOverlayPlacement(ed, ed.LSPOverlayView())
	if !ok {
		t.Fatal("lspOverlayPlacement() = not visible")
	}
	before := ed.Buffer.Cursor

	updatedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: p.x, Y: p.y}))
	updated := updatedAny.(Model)
	if got := updated.activeEditor().Buffer.Cursor; got != before {
		t.Fatalf("click on popup moved cursor to %#v, want %#v", got, before)
	}
	if !updated.activeEditor().IsAutocompleteVisible() {
		t.Fatal("click on keyboard-only autocomplete unexpectedly dismissed it")
	}
}

func BenchmarkLSPOverlayPlacement(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	cfg.UI.ShowTree = true
	m, err := NewModel("", b.TempDir(), cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(m.cleanup)
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	ed := editor.New(buf, m.theme, editor.DefaultConfig())
	m.editors = []editor.Editor{ed}
	m.tabBar.Tabs = nil
	m.width, m.height, m.showTree = 120, 40, true
	m.relayout()
	m.activeEditor().ShowAutocomplete([]overlays.AutocompleteItem{{Label: "候補🎉", Detail: "func", InsertText: "candidate"}})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, ok := m.currentLSPOverlayPlacement(); !ok {
			b.Fatal("placement unexpectedly hidden")
		}
	}
}
