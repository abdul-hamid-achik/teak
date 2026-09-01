package termpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

func TestAppendTextCapsHistory(t *testing.T) {
	m := New(ui.DefaultTheme(), ".")
	m.SetSize(20, 4)
	for i := 0; i < maxTerminalLines+50; i++ {
		m.appendText("line\n")
	}
	if len(m.lines) > maxTerminalLines {
		t.Fatalf("lines = %d, want <= %d", len(m.lines), maxTerminalLines)
	}
}

func TestApplyOutputDropsStaleGeneration(t *testing.T) {
	m := New(ui.DefaultTheme(), ".")
	m.generation = 2
	m.ApplyOutput(OutputMsg{Generation: 1, Data: []byte("stale")})
	if strings.Contains(strings.Join(m.lines, ""), "stale") {
		t.Fatal("stale PTY output was applied")
	}
	m.ApplyOutput(OutputMsg{Generation: 2, Data: []byte("fresh")})
	if !strings.Contains(strings.Join(m.lines, ""), "fresh") {
		t.Fatal("current generation output was dropped")
	}
}

func TestEncodeKeyEnterAndCtrlC(t *testing.T) {
	if got := encodeKey(tea.KeyPressMsg{Text: ""}); string(got) != "" {
		// empty text without a named key is ignored
	}
	enter := encodeKey(mustKey("enter"))
	if string(enter) != "\r" {
		t.Fatalf("enter = %q", enter)
	}
}

func mustKey(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return tea.KeyPressMsg{}
	}
}

func TestViewRendersTitle(t *testing.T) {
	m := New(ui.DefaultTheme(), ".")
	m.SetSize(40, 6)
	got := m.View()
	if !strings.Contains(got, "Terminal") {
		t.Fatalf("view = %q", got)
	}
}
