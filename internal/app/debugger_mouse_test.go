package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/debugger"
)

// awaitDebugZone polls until a zone is registered: m.View() scans its own
// output through a worker goroutine, so an immediate Get can miss it.
func awaitDebugZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if z := zone.Get(id); z != nil && !z.IsZero() {
			return z
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("zone %q was never registered", id)
	return nil
}

func TestDebuggerPanelMouseZonesAreClickable(t *testing.T) {
	zone.NewGlobal()
	m := newViewTestModel(t, true)
	m.width = 120
	m.height = 30
	root := m.rootDir
	// The active editor is main.go; the breakpoint targets a different file
	// that is not open yet, so the click must produce an open command.
	addDirtyEditor(t, &m, "main.go", "package main\n\nfunc main() {}\n", "package main\n\nfunc main() {}\n")
	otherFile := filepath.Join(root, "other.go")
	if err := os.WriteFile(otherFile, []byte("package main\n\nfunc other() {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(other.go): %v", err)
	}
	m.sidebarTab = SidebarDebugger
	m.setFocus(FocusDebugger)
	m.debuggerPanel.SetBreakpoints([]debugger.Breakpoint{
		{FilePath: otherFile, Line: 2, Enabled: true, Verified: true},
	})
	m.relayout()

	// m.View() scans its own zones internally; poll until they land.
	_ = m.View()
	startZone := awaitDebugZone(t, "debug-ctl-start")
	bpZone := awaitDebugZone(t, "debug-bp-0")

	// Clicking the breakpoint row opens the file at the breakpoint line.
	bpClick := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: bpZone.StartX + 1, Y: bpZone.StartY})
	updatedAny, cmd := m.Update(bpClick)
	m = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("clicking a breakpoint row did not open the file")
	}
	// The pending cursor transfers into the file-load request at load start.
	var sawLoad bool
	for _, req := range m.pendingFileLoads {
		if req.Path == otherFile && req.Cursor != nil && req.Cursor.Line == 2 {
			sawLoad = true
		}
	}
	if !sawLoad {
		t.Fatalf("no pending load for %s with cursor line 2: %+v", otherFile, m.pendingFileLoads)
	}

	// Clicking the start button dispatches a debug start for the .go buffer.
	_ = startZone
	updatedAny, cmd = m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      startZone.StartX + 1,
		Y:      startZone.StartY,
	}))
	m = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("clicking the start debugging button did not dispatch a debug start")
	}
	if m.debugLifecycle.pending {
		// The lifecycle flag proves the click took the real start path, not a
		// no-op. Reset it so cleanup does not wait on a debugger.
		m.debugLifecycle.pending = false
	}
}
