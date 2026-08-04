package debugger

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// awaitZone polls until a zone is registered; Scan applies marks through a
// worker goroutine, so an immediate Get can miss them.
func awaitZone(t *testing.T, id string) *zone.ZoneInfo {
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

func TestClickedBreakpointResolvesZone(t *testing.T) {
	zone.NewGlobal()
	m := New(ui.DefaultTheme())
	m.SetSize(40, 20)
	m.SetBreakpoints([]Breakpoint{{FilePath: "/tmp/a.go", Line: 2, Enabled: true, Verified: true}})

	zone.Scan(m.BreakpointView())
	z := awaitZone(t, "debug-bp-0")

	msg := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: z.StartX + 1, Y: z.StartY})
	if got := m.ClickedBreakpoint(msg); got != 0 {
		t.Fatalf("ClickedBreakpoint = %d, want 0", got)
	}
}

func TestClickedControlResolvesStartZone(t *testing.T) {
	zone.NewGlobal()
	m := New(ui.DefaultTheme())
	m.SetSize(40, 20)

	zone.Scan(m.renderInactive())
	z := awaitZone(t, "debug-ctl-start")

	msg := tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: z.StartX + 1, Y: z.StartY})
	control, ok := m.ClickedControl(msg)
	if !ok || control != DebugStart {
		t.Fatalf("ClickedControl = %q/%v, want start/true", control, ok)
	}
}
