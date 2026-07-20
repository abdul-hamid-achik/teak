package app

import "teak/internal/editor"

// rebuildBreakpointGutter converts the model's sorted breakpoint entries into
// the lookup used by the viewport. It runs only when a file's breakpoints
// change; rendering then shares the already-projected options instead of
// rebuilding a map for every View call.
func (m *Model) rebuildBreakpointGutter(path string) {
	if m.debugGutterBreakpoints == nil {
		m.debugGutterBreakpoints = make(map[string]*editor.GutterOpts)
	}

	entries := m.breakpoints[path]
	execLine := -1
	if m.currentExecFile == path {
		execLine = m.currentExecLine
	}
	if len(entries) == 0 && execLine < 0 {
		delete(m.debugGutterBreakpoints, path)
		return
	}

	breakpoints := make(map[int]editor.BreakpointState, len(entries))
	for _, breakpoint := range entries {
		if breakpoint.Enabled {
			breakpoints[breakpoint.Line] = editor.BPActive
		} else {
			breakpoints[breakpoint.Line] = editor.BPDisabled
		}
	}
	m.debugGutterBreakpoints[path] = &editor.GutterOpts{Breakpoints: breakpoints, ExecLine: execLine}
}

// projectDebugGutterForEditor updates one editor after a state transition.
// It is deliberately never called from View: rendering remains a pure read of
// already-projected state. The map is cached per file and shared read-only by
// the viewport, so typing and frame rendering do not scale with every
// breakpoint in a document.
func (m *Model) projectDebugGutterForEditor(index int) {
	if index < 0 || index >= len(m.editors) {
		return
	}

	ed := &m.editors[index]
	path := ed.Buffer.FilePath
	gutter := m.debugGutterBreakpoints[path]
	if gutter == nil && (len(m.breakpoints[path]) > 0 || m.currentExecFile == path && m.currentExecLine >= 0) {
		// State can be seeded by a restored session or an in-package caller.
		// Recover lazily at this Update-time boundary, never during rendering.
		m.rebuildBreakpointGutter(path)
		gutter = m.debugGutterBreakpoints[path]
	}

	if gutter == nil {
		ed.DebugGutter = nil
		return
	}
	if ed.DebugGutter == gutter {
		return
	}
	ed.DebugGutter = gutter
}

// projectDebugGutterForPath refreshes open editors that display a changed
// breakpoint or execution location. Normal files have a unique buffer owner,
// but updating all matching editors keeps this invariant valid for restored
// legacy sessions too.
func (m *Model) projectDebugGutterForPath(path string) {
	for i := range m.editors {
		if m.editors[i].Buffer.FilePath == path {
			m.projectDebugGutterForEditor(i)
		}
	}
}

// setExecutionLocation is the sole state transition for the execution
// marker. It clears the previous file and updates the new file outside View.
func (m *Model) setExecutionLocation(path string, line int) {
	previousPath := m.currentExecFile
	m.currentExecFile = path
	m.currentExecLine = line
	m.refreshExecutionGutter(previousPath)
	if path != previousPath {
		m.refreshExecutionGutter(path)
	}
}

// refreshExecutionGutter changes only the execution-line scalar in cached
// options. It preserves the breakpoint map and then points matching editors
// at the cache entry (or clears them when neither marker is present).
func (m *Model) refreshExecutionGutter(path string) {
	if path == "" {
		return
	}

	gutter := m.debugGutterBreakpoints[path]
	if gutter == nil {
		if len(m.breakpoints[path]) == 0 && m.currentExecFile != path {
			return
		}
		m.rebuildBreakpointGutter(path)
		gutter = m.debugGutterBreakpoints[path]
	}
	if gutter != nil {
		execLine := -1
		if m.currentExecFile == path {
			execLine = m.currentExecLine
		}
		gutter.ExecLine = execLine
		if len(gutter.Breakpoints) == 0 && execLine < 0 {
			delete(m.debugGutterBreakpoints, path)
		}
	}
	m.projectDebugGutterForPath(path)
}

// activateTab centralizes active-editor projection for keyboard, mouse and
// programmatic tab switches. TabBar owns the visible tab-strip scroll state.
func (m *Model) activateTab(index int) bool {
	if index < 0 || index >= len(m.editors) || index >= len(m.tabBar.Tabs) {
		return false
	}
	m.activeTab = index
	m.tabBar.SetActive(index)
	m.projectDebugGutterForEditor(index)
	return true
}
