package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTerminalPasteDoesNotModifyEditor(t *testing.T) {
	m := newViewTestModel(t, false)
	m.showTerminal = true
	m.setFocus(FocusTerminal)
	before := m.activeEditor().Buffer.Version()
	updatedAny, _ := m.Update(tea.PasteMsg{Content: "echo terminal-only\n"})
	updated := updatedAny.(Model)
	if updated.activeEditor().Buffer.Version() != before {
		t.Fatal("terminal paste modified the editor buffer")
	}
}

func TestHiddenTerminalKeepsOneOutputListener(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("requires a Unix PTY")
	}
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf started\nread answer\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", path)
	m := newViewTestModel(t, false)
	start := m.toggleTerminalPanel()
	updatedAny, listen := m.Update(start())
	m = updatedAny.(Model)
	if listen == nil {
		t.Fatalf("terminal failed to start: %v", m.terminal.Error())
	}
	m.toggleTerminalPanel() // hide before consuming the first frame
	updatedAny, next := m.Update(listen())
	m = updatedAny.(Model)
	if next == nil {
		t.Fatal("hidden terminal stopped listening")
	}
	if m.showTerminal {
		t.Fatal("output made the hidden terminal visible")
	}
	if duplicate := m.toggleTerminalPanel(); duplicate != nil {
		t.Fatal("reopening scheduled a duplicate reader")
	}
}

func TestTerminalStartupIsAsynchronous(t *testing.T) {
	m := newViewTestModel(t, false)
	cmd := m.toggleTerminalPanel()
	if cmd == nil {
		t.Fatal("opening terminal must schedule process startup")
	}
	if !m.terminal.Running() {
		t.Fatal("pending startup must prevent a duplicate launch")
	}
	m.toggleTerminalPanel()
	if cmd := m.toggleTerminalPanel(); cmd != nil {
		t.Fatal("reopening during startup launched another shell")
	}
}

func TestFindInEditorDoesNotCaptureTerminalCursor(t *testing.T) {
	m := newViewTestModel(t, false)
	m.activeEditor().ShowFind()
	m.showTerminal = true
	m.setFocus(FocusTerminal)
	if m.editorInputCaptured() {
		t.Fatal("find in another pane hides the terminal cursor")
	}
	m.showHelp = true
	if !m.editorInputCaptured() {
		t.Fatal("help must still hide the terminal cursor")
	}
}
