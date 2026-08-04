package app

import (
	"strings"
	"testing"

	"teak/internal/lsp"
)

func TestServerExitedWithoutManagerReportsOnly(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	m.lspMgr = nil

	updatedAny, cmd := m.handleServerExited(lsp.ServerExitedMsg{Command: "gopls"})
	m = updatedAny.(Model)
	if cmd != nil {
		t.Fatal("unexpected restart command without an LSP manager")
	}
	if !strings.Contains(m.status, "gopls") {
		t.Fatalf("status = %q, want the exited server named", m.status)
	}
}

func TestServerExitedStopsAfterCrashCap(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	m.lspRestarts = map[string]int{"gopls": maxLSPRestarts}

	updatedAny, cmd := m.handleServerExited(lsp.ServerExitedMsg{Command: "gopls"})
	m = updatedAny.(Model)
	if cmd != nil {
		t.Fatal("restart command emitted past the crash cap")
	}
	if !strings.Contains(m.status, "not restarting") {
		t.Fatalf("status = %q, want the crash-cap notice", m.status)
	}
}

func TestRestartLanguageServerCommandRegistered(t *testing.T) {
	m := newInputRoutingTestModel(t)
	found := false
	for _, cmd := range m.commandRegistry() {
		if cmd.ID == "restart_lsp" {
			found = true
		}
	}
	if !found {
		t.Fatal("restart_lsp missing from the palette registry")
	}
}

func TestRestartLanguageServerNoManager(t *testing.T) {
	m := newInputRoutingTestModel(t)
	addDirtyEditor(t, &m, "main.go", "package main\n", "package main\n")
	m.lspMgr = nil

	updatedAny, cmd := m.handleCommandPaletteAction(restartLspMsg{})
	m = updatedAny.(Model)
	if cmd != nil {
		t.Fatal("restart emitted a command without an LSP manager")
	}
}
