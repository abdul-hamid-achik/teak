package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/overlay"
	"teak/internal/plugin"

	"github.com/charmbracelet/log"
)

type requestQuitMsg struct{}
type shutdownCompleteMsg struct{}

// QuitFilter routes terminal-level quit and interrupt messages through Model
// so unsaved buffers and subsystem cleanup receive the same treatment as
// Ctrl+Q. A quit produced by finalizeQuit is allowed through because that
// model has already recorded user approval.
func QuitFilter(model tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case tea.QuitMsg, tea.InterruptMsg:
		var approved bool
		switch current := model.(type) {
		case Model:
			approved = current.modelState != nil && current.quitApproved
		case *Model:
			approved = current != nil && current.modelState != nil && current.quitApproved
		}
		if !approved {
			return requestQuitMsg{}
		}
	}
	return msg
}

// requestQuit is the single entry point for user-initiated quit requests.
// Destructive shutdown is deferred until there are no dirty buffers or the
// user explicitly confirms how those buffers should be handled.
func (m Model) requestQuit() (Model, tea.Cmd) {
	m.cancelActiveEditorDrag()
	var dirtyNames []string
	for i, ed := range m.editors {
		if !ed.Buffer.Dirty() {
			continue
		}
		name := filepath.Base(ed.Buffer.FilePath)
		if name == "." || ed.Buffer.FilePath == "" {
			name = m.tabBar.Tabs[i].Label
		}
		dirtyNames = append(dirtyNames, name)
	}

	if len(dirtyNames) == 0 {
		return m.finalizeQuit()
	}

	message := fmt.Sprintf("You have %d unsaved file(s):", len(dirtyNames))
	m.unsavedConfirm = overlay.NewConfirm(
		"Unsaved Changes", message, dirtyNames,
		[]overlay.Button{
			{Label: "Save All & Quit", Style: m.primaryButtonStyle(), Action: SaveAllAndQuitMsg{}},
			{Label: "Quit Without Saving", Style: m.dangerButtonStyle(), Action: QuitWithoutSavingMsg{}},
			{Label: "Cancel", Action: overlay.ButtonAction{Label: "Cancel"}},
		}, m.theme,
	)
	return m, nil
}

// finalizeQuit performs terminal cleanup after the quit decision is settled.
func (m Model) finalizeQuit() (Model, tea.Cmd) {
	if m.quitApproved {
		return m, tea.Quit
	}
	if m.shutdownStarted {
		return m, nil
	}
	m.shutdownStarted = true
	m.status = "Shutting down…"
	// Stop rendering an active spinner while asynchronous teardown is still
	// producing the final terminal frame. The ACP manager is stopped by the
	// cleanup command below, so the panel must not advertise a live session
	// during that interval.
	m.agentPanel.SetConnected(false)
	// A final snapshot must be ordered after an active autosave. Replacing the
	// queued snapshot here makes shutdown latest-wins, so an older writer can
	// never overwrite the state captured at exit. session.Save has no
	// cancellation boundary; a filesystem write that never returns will delay
	// teardown rather than risk letting an older snapshot win. Ordinary save
	// errors are handled by handleSessionSaveResult, which still continues
	// shutdown.
	if state, ok := m.sessionSnapshot(); ok {
		recovery, dropped := m.recoveryPrepsCounted()
		if dropped > 0 {
			m.status = fmt.Sprintf("%d unsaved buffer(s) skipped (over 4 MiB recovery limit)", dropped)
		}
		if m.sessionSaves.inFlight {
			m.sessionSaves.queued = &state
			m.sessionSaves.queuedRecovery = recovery
			return m, nil
		}
		return m.startSessionSave(state, recovery)
	}
	return m, m.shutdownCmd()
}

// shutdownCmd performs the resource teardown only after the final session
// write has completed. It intentionally runs outside Update because protocol
// shutdown and watcher close may block.
func (m Model) shutdownCmd() tea.Cmd {
	pluginMgr := m.pluginMgr
	pluginRuntime := newPluginAsyncRuntime(m)
	vimLeave := m.pluginEvent(plugin.EventVimLeave, "")
	state := m.modelState
	return func() tea.Msg {
		// VimLeave is the one lifecycle callback that must run before its Lua
		// state is closed. It deliberately has no model write-back during
		// shutdown; any UI effects are meaningless once the terminal exits.
		if pluginMgr != nil {
			if err := pluginMgr.DispatchEvent(pluginRuntime, vimLeave.Event, vimLeave); err != nil {
				log.Warn("plugin VimLeave failed", "err", err)
			}
		}
		cleanupModelState(state)
		return shutdownCompleteMsg{}
	}
}
