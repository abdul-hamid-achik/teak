package app

import (
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
)

// BenchmarkModelViewWithManyBreakpoints guards the render hot path against
// rebuilding a whole-document breakpoint lookup on every frame. Breakpoint
// projection belongs to Update-time state transitions, not View.
func BenchmarkModelViewWithManyBreakpoints(b *testing.B) {
	previousZone := zone.DefaultManager
	freshZone := zone.New()
	zone.DefaultManager = freshZone
	b.Cleanup(func() {
		freshZone.Close()
		zone.DefaultManager = previousZone
	})

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	cfg.UI.ShowTree = false
	m, err := NewModel("", b.TempDir(), cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(m.cleanup)
	m.welcome = nil
	m.width = 120
	m.height = 40
	m.focus = FocusEditor
	m.activeEditor().Buffer.FilePath = "many.go"
	m.tabBar.Tabs[m.activeTab].FilePath = "many.go"
	m.relayout()
	path := m.activeEditor().Buffer.FilePath
	entries := make([]breakpointEntry, 10_000)
	for i := range entries {
		entries[i] = breakpointEntry{Line: i * 2, Enabled: i%2 == 0}
	}
	m.breakpoints[path] = entries
	m.currentExecFile = path
	m.currentExecLine = 4
	m.rebuildBreakpointGutter(path)
	m.projectDebugGutterForEditor(m.activeTab)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		view := m.View()
		if view.Content == "" {
			b.Fatal("View returned empty content")
		}
	}
}
