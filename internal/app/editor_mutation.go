package app

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
	"teak/internal/text"
)

// syncEditorStateAfterUpdate reconciles the app-level state that accompanies
// every editor buffer mutation. Keeping this in one place prevents
// programmatic edits from bypassing dirty indicators, preview pinning, LSP
// synchronization, or plugin autocmds.
func (m *Model) syncEditorStateAfterUpdate(tabIndex, prevVersion int, prevCursor text.Position) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) {
		return nil
	}

	ed := &m.editors[tabIndex]
	if tabIndex < len(m.tabBar.Tabs) {
		m.tabBar.Tabs[tabIndex].Dirty = ed.Buffer.Dirty()
		if ed.Buffer.Dirty() {
			m.tabBar.PinTab(tabIndex)
		}
	}

	var cmds []tea.Cmd
	if m.lspMgr != nil && ed.Buffer.Version() != prevVersion && ed.Buffer.FilePath != "" {
		if client := m.lspMgr.ClientForFile(ed.Buffer.FilePath); client != nil {
			cmds = append(cmds, m.notifyLSPChange(client, ed))
		}
	}

	cmds = append(cmds, m.triggerEditorAutocmds(
		ed.Buffer.FilePath,
		prevVersion,
		ed.Buffer.Version(),
		prevCursor,
		ed.Buffer.Cursor,
	))
	return tea.Batch(cmds...)
}

// LSP document notifications always execute as commands. A blocked language
// server stdin must never delay Bubble Tea's Update loop, and rope
// materialization stays inside the command instead of Update.
func lspDidChangeCmd(mgr *lsp.Manager, path string, version int, snapshot *text.Rope, budgets ...*lspMaterializationBudget) tea.Cmd {
	if mgr == nil {
		return nil
	}
	generation := mgr.DocumentGeneration(path)
	budget := sharedLSPMaterializations
	if len(budgets) > 0 {
		budget = lspMaterializationsFor(budgets[0])
	}
	return func() tea.Msg {
		if snapshot == nil || mgr.DocumentGeneration(path) != generation {
			return nil
		}
		if content, ok := budget.materializeSnapshotIf(snapshot, nil, func() bool {
			return mgr.DocumentGeneration(path) == generation
		}); ok {
			mgr.ChangeDocument(path, generation, version, content)
		}
		return nil
	}
}

func lspDidSaveCmd(mgr *lsp.Manager, path string) tea.Cmd {
	if mgr == nil {
		return nil
	}
	generation := mgr.DocumentGeneration(path)
	return func() tea.Msg { mgr.SaveDocument(path, generation); return nil }
}
