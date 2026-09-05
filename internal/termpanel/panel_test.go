package termpanel

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"teak/internal/ui"
	"testing"
	"time"
)

func TestApplyOutputDropsStaleGeneration(t *testing.T) {
	m := New(ui.NordTheme(), ".")
	m.SetSize(20, 5)
	m.generation = 2
	f := &terminalFrame{content: "stale", width: 20, height: 4}
	m.ApplyOutput(OutputMsg{Generation: 1, frame: f})
	if strings.Contains(m.View(), "stale") {
		t.Fatal("stale session frame applied")
	}
	f.content = "fresh"
	m.ApplyOutput(OutputMsg{Generation: 2, frame: f})
	if !strings.Contains(m.View(), "fresh") {
		t.Fatal("current session frame dropped")
	}
}

func TestKeysDuringStartupReachTheNewShell(t *testing.T) {
	r, s := newFixtureTerminal(t)
	m := New(ui.NordTheme(), ".")
	m.starting = true
	m.WriteKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.ApplyStarted(StartedMsg{Generation: m.generation, terminal: r})
	select {
	case got := <-s.writes:
		if got != "x" {
			t.Fatalf("startup input=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("input typed while starting was lost")
	}
}
func TestListenDoesNotDuplicatePendingRead(t *testing.T) {
	r, _ := newFixtureTerminal(t)
	m := New(ui.NordTheme(), ".")
	m.terminal = r
	first := m.Listen()
	if first == nil || m.Listen() != nil {
		t.Fatal("must have exactly one pending session read")
	}
	m.ApplyOutput(first().(OutputMsg))
	if m.Listen() == nil {
		t.Fatal("read not rearmed after current result")
	}
}
func TestViewRendersTitle(t *testing.T) {
	m := New(ui.NordTheme(), ".")
	m.SetSize(40, 6)
	if got := m.View(); !strings.Contains(got, "Terminal") {
		t.Fatalf("view=%q", got)
	}
}
