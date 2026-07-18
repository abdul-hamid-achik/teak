package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/editor"
)

func useViewTestZone(t *testing.T) {
	t.Helper()

	// BubbleZone exposes one package-global manager. Keep these tests
	// non-parallel and restore the previous manager when each test finishes.
	previous := zone.DefaultManager
	fresh := zone.New()
	zone.DefaultManager = fresh
	t.Cleanup(func() {
		fresh.Close()
		zone.DefaultManager = previous
	})
}

func newViewTestModel(t *testing.T, showTree bool) Model {
	t.Helper()
	useViewTestZone(t)

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	cfg.UI.ShowTree = showTree

	m := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	m.width = 120
	m.height = 40
	m.showTree = showTree
	m.focus = FocusEditor
	m.relayout()
	return m
}

func TestModelViewZeroSize(t *testing.T) {
	v := (Model{}).View()

	if v.Content != "" {
		t.Fatalf("Content = %q, want empty", v.Content)
	}
	if v.Cursor != nil {
		t.Fatal("Cursor != nil, want no cursor")
	}
	if v.AltScreen {
		t.Fatal("AltScreen = true, want false")
	}
}

func TestModelViewEditorTerminalContract(t *testing.T) {
	m := newViewTestModel(t, false)
	m.gitBranch = "feature/render"
	m.status = "Saved"

	localX, localY := m.activeEditor().CursorPosition()
	v := m.View()

	if !v.AltScreen {
		t.Error("AltScreen = false, want true")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want %v", v.MouseMode, tea.MouseModeCellMotion)
	}
	if v.Cursor == nil {
		t.Fatal("Cursor = nil, want editor cursor")
	}
	if v.Cursor.X != localX || v.Cursor.Y != localY+1 {
		t.Errorf(
			"cursor = (%d,%d), want (%d,%d)",
			v.Cursor.X,
			v.Cursor.Y,
			localX,
			localY+1,
		)
	}
	if v.Cursor.Shape != tea.CursorBar {
		t.Errorf("Cursor.Shape = %v, want %v", v.Cursor.Shape, tea.CursorBar)
	}
	if !v.Cursor.Blink {
		t.Error("Cursor.Blink = false, want true")
	}

	plain := ansi.Strip(v.Content)
	for _, want := range []string{
		"F1 Help",
		"feature/render",
		"Saved",
		"Ln 1, Col 1",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered content does not contain %q", want)
		}
	}
}

func TestModelViewTreeOffsetsCursor(t *testing.T) {
	m := newViewTestModel(t, true)
	localX, localY := m.activeEditor().CursorPosition()

	v := m.View()

	if v.Cursor == nil {
		t.Fatal("Cursor = nil, want editor cursor")
	}
	wantX := localX + m.treeWidth() + 1
	wantY := localY + 1
	if v.Cursor.X != wantX || v.Cursor.Y != wantY {
		t.Errorf(
			"cursor = (%d,%d), want (%d,%d)",
			v.Cursor.X,
			v.Cursor.Y,
			wantX,
			wantY,
		)
	}
}

func TestModelViewHelpOverlaySuppressesCursor(t *testing.T) {
	m := newViewTestModel(t, false)
	m.showHelp = true
	m.helpM.SetSize(m.width, m.height)

	v := m.View()

	if v.Cursor != nil {
		t.Fatal("Cursor != nil, want help overlay to suppress it")
	}
	if !strings.Contains(ansi.Strip(v.Content), "Keyboard Shortcuts") {
		t.Error("rendered content does not contain help overlay")
	}
}

func TestModelViewProjectsDebugGutter(t *testing.T) {
	m := newViewTestModel(t, false)
	path := m.activeEditor().Buffer.FilePath
	m.breakpoints[path] = []breakpointEntry{{Line: 0, Enabled: true}}
	m.currentExecFile = path
	m.currentExecLine = 0

	_ = m.View()

	gutter := m.activeEditor().DebugGutter
	if gutter == nil {
		t.Fatal("DebugGutter = nil, want projected debugger state")
	}
	if got := gutter.Breakpoints[0]; got != editor.BPActive {
		t.Errorf("Breakpoints[0] = %v, want %v", got, editor.BPActive)
	}
	if gutter.ExecLine != 0 {
		t.Errorf("ExecLine = %d, want 0", gutter.ExecLine)
	}

	m.breakpoints[path] = nil
	m.currentExecFile = ""
	m.currentExecLine = -1

	_ = m.View()

	if m.activeEditor().DebugGutter != nil {
		t.Fatal("DebugGutter != nil after debugger state was cleared")
	}
}
