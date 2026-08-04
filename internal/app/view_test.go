package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/text"
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

func TestFindWidgetDoesNotClipStatusBar(t *testing.T) {
	m := newViewTestModel(t, false)
	baseEditorHeight := m.activeEditor().Viewport.Height

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = updated.(Model)
	if ed := m.activeEditor(); ed == nil || !ed.IsFindVisible() {
		t.Fatal("ctrl+f did not open the find widget")
	}

	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if len(lines) != m.height {
		t.Fatalf("view = %d lines with the find widget open, want exactly %d (the status bar must not be clipped)", len(lines), m.height)
	}
	if got := m.activeEditor().Viewport.Height; got != baseEditorHeight-1 {
		t.Fatalf("editor viewport height with find open = %d, want %d", got, baseEditorHeight-1)
	}

	// Closing the widget restores the full text area.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if ed := m.activeEditor(); ed.IsFindVisible() {
		t.Fatal("escape did not close the find widget")
	}
	if got := m.activeEditor().Viewport.Height; got != baseEditorHeight {
		t.Fatalf("editor viewport height after closing find = %d, want %d", got, baseEditorHeight)
	}
	v = m.View()
	if lines = strings.Split(v.Content, "\n"); len(lines) != m.height {
		t.Fatalf("view = %d lines after closing find, want %d", len(lines), m.height)
	}
}

func TestClipboardCopyResultEmitsOSC52(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "a.txt", "x\n", "x\n")
	editorID := m.activeEditor().ID()

	updatedAny, cmd := m.handleClipboardCopyResult(editor.ClipboardCopyResultMsg{
		EditorID:       editorID,
		FallbackStored: true,
		OSC52Sequence:  "\x1b]52;c;aG9sYQ==\x07",
	})
	m = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("no emit command for the OSC 52 sequence")
	}
	if !strings.Contains(m.status, "OSC 52") {
		t.Fatalf("status = %q, want the terminal-clipboard notice", m.status)
	}
}

func TestStatusBarShowsDiagnosticUnderCursor(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "main.go", "let x = 1\n", "let x = 1\n")
	m.editors[m.activeTab].Diagnostics = []editor.Diagnostic{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Severity: 1, Message: "undefined: x"},
	}
	m.editors[m.activeTab].Buffer.SetCursor(text.Position{Line: 0, Col: 0})

	bar := m.renderStatusBar()
	if !strings.Contains(bar, "undefined: x") {
		t.Fatalf("status bar = %q, want the diagnostic under the cursor surfaced", bar)
	}

	// An explicit status message wins over the diagnostic echo.
	m.status = "Saved"
	bar = m.renderStatusBar()
	if !strings.Contains(bar, "Saved") || strings.Contains(bar, "undefined: x") {
		t.Fatalf("status bar = %q, want the explicit status to take precedence", bar)
	}
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

func TestModelViewSingleRowRendersSafely(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 1
	m.height = 1
	m.relayout()

	v := m.View()
	if v.Content != "" {
		t.Fatalf("single-row content = %q, want empty minimal view", v.Content)
	}
}

func TestModelViewTinyTerminalMatrixDoesNotPanic(t *testing.T) {
	for width := 0; width <= 12; width++ {
		for height := 0; height <= 6; height++ {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := newViewTestModel(t, true)
				m.width = width
				m.height = height
				m.relayout()
				_ = m.View()
			})
		}
	}
}

func TestModelViewCompactHeightNeverExceedsTerminalRows(t *testing.T) {
	for _, showTree := range []bool{false, true} {
		for height := 2; height <= 3; height++ {
			t.Run(fmt.Sprintf("tree-%t-height-%d", showTree, height), func(t *testing.T) {
				m := newViewTestModel(t, showTree)
				m.width = 12
				m.height = height
				m.relayout()

				view := m.View()
				content := view.Content
				rows := 0
				if content != "" {
					rows = strings.Count(content, "\n") + 1
				}
				if rows > height {
					t.Fatalf("rendered rows = %d, exceed terminal height %d:\n%q", rows, height, ansi.Strip(content))
				}
				if height == 3 && showTree && view.Cursor != nil {
					localX, _ := m.activeEditor().CursorPosition()
					if view.Cursor.X != localX {
						t.Fatalf("compact cursor X = %d, want editor-local %d with hidden sidebar", view.Cursor.X, localX)
					}
				}
			})
		}
	}
}

func TestCompactRelayoutUsesFullEditorWidth(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width = 120
	m.height = 3
	m.showAgent = true
	m.relayout()

	if got := m.tabBar.Width; got != m.width {
		t.Fatalf("compact tab width = %d, want terminal width %d", got, m.width)
	}
	if got := m.activeEditor().Viewport.Width; got != m.width {
		t.Fatalf("compact editor width = %d, want terminal width %d", got, m.width)
	}
}

func TestTreeLayoutNeverExceedsTinyViewportWidth(t *testing.T) {
	for width := 1; width <= 24; width++ {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			m := newViewTestModel(t, true)
			m.width = width
			m.height = 8
			m.relayout()

			if got, limit := m.treeWidth(), max(0, width-2); got > limit {
				t.Fatalf("treeWidth() = %d, exceeds available sidebar width %d", got, limit)
			}

			for _, line := range strings.Split(m.View().Content, "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("rendered line width = %d, exceeds viewport %d: %q", got, width, line)
				}
			}
		})
	}
}

func TestSidebarTabsUseASCIIWithoutNerdFont(t *testing.T) {
	t.Setenv("TEAK_NO_NERD_FONT", "1")
	m := newViewTestModel(t, true)
	m.width = 80
	m.relayout()

	bar := ansi.Strip(m.sidebarTabBar())
	if !strings.Contains(bar, " F ") || !strings.Contains(bar, " G ") {
		t.Fatalf("ASCII sidebar tabs missing from %q", bar)
	}
	if strings.Contains(bar, "\uf413") || strings.Contains(bar, "\ue725") {
		t.Fatalf("Nerd Font sidebar tab present in %q", bar)
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

func TestModelViewDoesNotMutateDebugGutter(t *testing.T) {
	m := newViewTestModel(t, false)
	path := m.activeEditor().Buffer.FilePath
	m.breakpoints[path] = []breakpointEntry{{Line: 0, Enabled: true}}
	m.currentExecFile = path
	m.currentExecLine = 0
	original := &editor.GutterOpts{
		Breakpoints: map[int]editor.BreakpointState{1: editor.BPDisabled},
		ExecLine:    1,
	}
	m.activeEditor().DebugGutter = original

	_ = m.View()

	gutter := m.activeEditor().DebugGutter
	if gutter != original {
		t.Fatalf("View replaced DebugGutter = %p, want unchanged %p", gutter, original)
	}
}
