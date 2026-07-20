package app

import (
	"testing"

	"teak/internal/dap"
	"teak/internal/editor"
)

func TestBreakpointProjectionHappensOnStateChangeNotView(t *testing.T) {
	m := newViewTestModel(t, false)
	path := m.activeEditor().Buffer.FilePath

	_ = m.toggleBreakpoint(path, 3)
	gutter := m.activeEditor().DebugGutter
	if gutter == nil || gutter.Breakpoints[3] != editor.BPActive {
		t.Fatalf("active breakpoint gutter = %#v, want line 3 active", gutter)
	}

	_ = m.View()
	if got := m.activeEditor().DebugGutter; got != gutter {
		t.Fatal("View replaced the already-projected debug gutter")
	}

	_ = m.toggleBreakpoint(path, 3)
	gutter = m.activeEditor().DebugGutter
	if gutter == nil || gutter.Breakpoints[3] != editor.BPDisabled {
		t.Fatalf("disabled breakpoint gutter = %#v, want line 3 disabled", gutter)
	}

	_ = m.toggleBreakpoint(path, 3)
	if m.activeEditor().DebugGutter != nil {
		t.Fatal("removing the final breakpoint left a gutter projection behind")
	}
}

func TestDebugGutterProjectsWhenSwitchingTabs(t *testing.T) {
	m := newViewTestModel(t, false)
	firstPath := m.activeEditor().Buffer.FilePath
	second := addDirtyEditor(t, &m, "other.go", "package other\n", "package other\n")
	if second != 1 {
		t.Fatalf("second tab = %d, want 1", second)
	}

	_ = m.toggleBreakpoint(firstPath, 2)
	if m.activeTab != second {
		t.Fatalf("active tab changed to %d while toggling inactive file breakpoint, want %d", m.activeTab, second)
	}
	if m.editors[second].DebugGutter != nil {
		t.Fatal("unrelated active tab unexpectedly received another file's gutter")
	}

	updatedAny, _ := m.Update(SwitchTabMsg{Index: 0})
	updated := updatedAny.(Model)
	gutter := updated.activeEditor().DebugGutter
	if gutter == nil || gutter.Breakpoints[2] != editor.BPActive {
		t.Fatalf("switched tab gutter = %#v, want active breakpoint at line 2", gutter)
	}

	_ = updated.View()
	if updated.activeEditor().DebugGutter != gutter {
		t.Fatal("View replaced the tab-switch gutter projection")
	}
}

func TestDebugGutterProjectsWhenActiveEditorIsRecreated(t *testing.T) {
	m := newViewTestModel(t, false)
	path := m.activeEditor().Buffer.FilePath
	_ = m.toggleBreakpoint(path, 1)
	want := m.activeEditor().DebugGutter
	if want == nil {
		t.Fatal("missing initial gutter projection")
	}

	recreated := editor.New(m.activeEditor().Buffer, m.theme, m.activeEditor().Config)
	m.setEditor(m.activeTab, recreated)

	if got := m.activeEditor().DebugGutter; got != want {
		t.Fatalf("recreated editor gutter = %p, want shared projection %p", got, want)
	}
}

func TestDebugStateProjectsExecutionMarkerBeforeRendering(t *testing.T) {
	m := newViewTestModel(t, false)
	path := m.activeEditor().Buffer.FilePath

	updatedAny, _ := m.Update(debugStateMsg{
		Frames: []dap.StackFrame{{Source: dap.Source{Path: path}, Line: 5}},
	})
	updated := updatedAny.(Model)
	gutter := updated.activeEditor().DebugGutter
	if gutter == nil || gutter.ExecLine != 4 {
		t.Fatalf("execution gutter = %#v, want line 4 before View", gutter)
	}

	_ = updated.View()
	if updated.activeEditor().DebugGutter != gutter {
		t.Fatal("View replaced the DAP execution projection")
	}
}
