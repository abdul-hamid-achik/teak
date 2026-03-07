package app

import (
	"testing"

	"teak/internal/dap"
	"teak/internal/lsp"
)

// TestMessageTypes ensures all message types compile and can be instantiated
func TestMessageTypes(t *testing.T) {
	// LSP messages
	_ = lspMsg{msg: nil}
	_ = lspLocationPickerMsg{Location: lsp.Location{}}
	_ = lspSymbolPickerMsg{Symbol: lsp.DocumentSymbol{}}

	// DAP messages
	_ = dapMsg{msg: nil}
	_ = debugStateMsg{Frames: []dap.StackFrame{}, Variables: []dap.Variable{}}

	// ACP messages
	_ = acpMsg{msg: nil}
	_ = toggleAgentMsg{}
	_ = focusAgentMsg{}
	_ = agentCancelMsg{}
	_ = agentModelPickerSelectMsg{ModelId: "test"}
	_ = agentFilePickerSelectMsg{Path: "test.go"}
	_ = agentWriteErrorMsg{Path: "test.go", Err: nil}

	// Search messages
	_ = FileListMsg{Files: []string{}}

	// Git messages
	_ = toggleTreeMsg{}
	_ = toggleGitMsg{}
	_ = toggleProblemsMsg{}

	// Editor messages
	_ = hoverTriggerMsg{}

	// Session messages
	_ = sessionAutoSaveMsg{}

	// Command palette messages
	_ = commandPaletteMsg{}
	_ = SaveAllAndQuitMsg{}
	_ = QuitWithoutSavingMsg{}

	// UI messages
	_ = goToLineMsg{}
	_ = reopenTabMsg{}
	_ = openSettingsMsg{}
	_ = showHelpMsg{}
	_ = debugStartMsg{}

	// File watcher messages
	_ = FileChangedMsg{Path: "test.go", Data: nil}
	_ = TreeChangedMsg{Dir: "."}

	// Editor trigger messages
	_ = RequestCompletionCmd{}
	_ = RetokenizeMsg{Version: 1, ViewportOnly: false}
	_ = TokenizeCompleteMsg{Version: 1, Lines: nil, Partial: false}
	_ = BreakpointClickMsg{Line: 0}
}

// TestMessageFields verifies message struct fields are accessible
func TestMessageFields(t *testing.T) {
	tests := []struct {
		name string
		msg  any
	}{
		{"lspMsg", lspMsg{msg: "test"}},
		{"agentFilePickerSelectMsg", agentFilePickerSelectMsg{Path: "test.go"}},
		{"agentWriteErrorMsg", agentWriteErrorMsg{Path: "test.go", Err: nil}},
		{"FileListMsg", FileListMsg{Files: []string{"a.go", "b.go"}}},
		{"FileChangedMsg", FileChangedMsg{Path: "test.go", Data: []byte("test")}},
		{"TreeChangedMsg", TreeChangedMsg{Dir: "/project"}},
		{"RetokenizeMsg", RetokenizeMsg{Version: 1, ViewportOnly: true}},
		{"TokenizeCompleteMsg", TokenizeCompleteMsg{Version: 1, Partial: false}},
		{"BreakpointClickMsg", BreakpointClickMsg{Line: 42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg == nil {
				t.Error("message should not be nil")
			}
		})
	}
}
