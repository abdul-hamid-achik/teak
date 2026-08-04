package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/highlight"
)

// reconcileTreeTransfer updates in-memory identities after a filesystem move
// or rename has committed. The buffer remains the source of truth, so dirty
// edits survive the move and the next save writes to the new path.
func (m *Model) reconcileTreeTransfer(source, destination string, targetIsDir bool) tea.Cmd {
	if m == nil || source == "" || destination == "" {
		return nil
	}
	var cmds []tea.Cmd
	if m.pendingCursor != nil {
		if newPath, ok := relocatedTreePath(source, destination, m.pendingCursor.Path, targetIsDir); ok {
			m.pendingCursor.Path = newPath
		}
	}
	for index := range m.editors {
		oldPath := m.editors[index].Buffer.FilePath
		newPath, ok := relocatedTreePath(source, destination, oldPath, targetIsDir)
		if !ok || filepath.Clean(oldPath) == filepath.Clean(newPath) {
			continue
		}

		oldEditorID := m.editors[index].ID()
		m.reconcileTreeDiagnosticPath(oldPath, newPath)
		m.reconcileTreeEditorPath(index, oldPath, newPath)
		if cmd := m.reconcileTreeFileLoad(index, oldEditorID, oldPath, newPath); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.watcher != nil {
			m.watcher.UnwatchFile(oldPath)
			m.watcher.WatchFile(newPath)
		}
		m.clearClosedExternalPathState(oldPath)
		if m.lspMgr != nil {
			m.lspMgr.CloseDocument(oldPath)
			if cmd := m.lspDidOpen(m.editors[index].Buffer); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	if targetIsDir && m.watcher != nil {
		m.watcher.WatchDir(destination)
	}
	cmds = append(cmds, m.reconcileTreeBreakpoints(source, destination, targetIsDir)...)
	return tea.Batch(cmds...)
}

// reconcileTreeFileLoad retires a load command that still points at the old
// identity and starts a fresh read at the committed destination. A completed
// old-path command is intentionally ignored by handleFileLoaded, so without
// this restart a move during an async open could leave a permanent placeholder
// tab.
func (m *Model) reconcileTreeFileLoad(index int, oldEditorID uint64, oldPath, newPath string) tea.Cmd {
	if index < 0 || index >= len(m.editors) || len(m.pendingFileLoads) == 0 {
		return nil
	}
	found := false
	for requestID, request := range m.pendingFileLoads {
		if request.EditorID != oldEditorID || filepath.Clean(request.Path) != filepath.Clean(oldPath) {
			continue
		}
		if request.Cancel != nil {
			request.Cancel()
		}
		delete(m.pendingFileLoads, requestID)
		found = true
	}
	if !found {
		return nil
	}
	return m.startFileLoad(newPath, m.editors[index], false, nil)
}

func (m *Model) reconcileTreeDiagnosticPath(oldPath, newPath string) {
	m.ensureDiagnosticIndexes()
	oldSeverity, hadOldSeverity := m.fileDiagnostics[oldPath]
	if hadOldSeverity {
		m.adjustDirectoryDiagnostics(oldPath, oldSeverity, -1)
		delete(m.fileDiagnostics, oldPath)
		delete(m.treeDiagnostics, oldPath)
		m.fileDiagnostics[newPath] = oldSeverity
		m.treeDiagnostics[newPath] = oldSeverity
		m.adjustDirectoryDiagnostics(newPath, oldSeverity, 1)
	} else {
		delete(m.fileDiagnostics, oldPath)
		delete(m.treeDiagnostics, oldPath)
	}
	m.problemsPanel.RelocateFilePath(oldPath, newPath)
	if m.coordinator != nil {
		if coordinator := m.coordinator.GetLSPCoordinator(); coordinator != nil {
			coordinator.RelocateFilePath(oldPath, newPath)
		}
	}
}

func (m *Model) reconcileTreeEditorPath(index int, oldPath, newPath string) {
	if index < 0 || index >= len(m.editors) {
		return
	}
	ed := &m.editors[index]
	ed.Buffer.FilePath = newPath
	ed.Config.CommentPrefix = editor.CommentPrefixForFile(newPath)
	if index < len(m.tabBar.Tabs) {
		m.tabBar.Tabs[index].Label = filepath.Base(newPath)
		m.tabBar.Tabs[index].FilePath = newPath
		m.tabBar.Tabs[index].Dirty = ed.Buffer.Dirty()
		m.tabBar.Tabs[index].DiagSeverity = m.fileDiagnostics[newPath]
	}

	if filepath.Ext(oldPath) == filepath.Ext(newPath) && ed.Highlighter != nil {
		return
	}
	replacement := editor.New(ed.Buffer, m.theme, ed.Config)
	replacement.SetSize(ed.Viewport.Width, ed.Viewport.Height)
	replacement.HasLSP = ed.HasLSP
	replacement.Diagnostics = append([]editor.Diagnostic(nil), ed.Diagnostics...)
	highlighter := highlight.New(newPath, m.theme)
	highlighter.TokenizePrefix(ed.Buffer.Bytes(), 60)
	replacement.Highlighter = highlighter
	m.setEditor(index, replacement)
}

func (m *Model) reconcileTreeBreakpoints(source, destination string, targetIsDir bool) []tea.Cmd {
	var cmds []tea.Cmd
	type breakpointMove struct {
		oldPath string
		newPath string
		entries []breakpointEntry
	}
	var moves []breakpointMove
	for oldPath, entries := range m.breakpoints {
		newPath, ok := relocatedTreePath(source, destination, oldPath, targetIsDir)
		if !ok || oldPath == newPath {
			continue
		}
		moves = append(moves, breakpointMove{oldPath: oldPath, newPath: newPath, entries: entries})
	}
	for _, move := range moves {
		oldPath, newPath := move.oldPath, move.newPath
		delete(m.breakpoints, oldPath)
		m.breakpoints[newPath] = move.entries
		delete(m.debugGutterBreakpoints, oldPath)
		m.rebuildBreakpointGutter(newPath)
		if m.debugMgr != nil && m.debugMgr.IsRunning() {
			// The old path must be cleared at the adapter before the new path is
			// installed. The closures read the map when executed, after the move.
			cmds = append(cmds, m.sendBreakpointsToDAP(oldPath), m.sendBreakpointsToDAP(newPath))
		}
	}
	oldExecPath := m.currentExecFile
	if newPath, ok := relocatedTreePath(source, destination, oldExecPath, targetIsDir); ok {
		delete(m.debugGutterBreakpoints, oldExecPath)
		m.currentExecFile = newPath
		m.rebuildBreakpointGutter(newPath)
	}
	m.syncDebuggerBreakpoints()
	return cmds
}
