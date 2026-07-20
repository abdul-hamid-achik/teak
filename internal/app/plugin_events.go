package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/plugin"
	"teak/internal/text"
)

type pluginEventMsg struct {
	Events []plugin.EventContext
}

// pluginDispatchResultMsg is produced by a worker tea.Cmd after Lua has run
// against an isolated runtime snapshot. Update applies only its recorded
// effects, preserving Bubble Tea's single-writer model.
type pluginDispatchResultMsg struct {
	Runtime *pluginAsyncRuntime
	Err     error
}

type pluginKeyDispatchResultMsg struct {
	Runtime *pluginAsyncRuntime
	Handled bool
	Pending bool
	Err     error
}

// pluginLoadResultMsg transfers ownership of a fully initialized Lua manager
// back to the Bubble Tea update loop. Loading executes user supplied code and
// must never happen while NewModel is constructing the first frame.
type pluginLoadResultMsg struct {
	Generation uint64
	Manager    *plugin.Manager
	Err        error
}

func pluginLoadCmd(dir string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		mgr, err := plugin.NewManager(dir)
		if err == nil {
			err = mgr.LoadAllPlugins()
		}
		return pluginLoadResultMsg{Generation: generation, Manager: mgr, Err: err}
	}
}

func pluginEventCmd(events ...plugin.EventContext) tea.Cmd {
	if len(events) == 0 {
		return nil
	}
	copied := append([]plugin.EventContext(nil), events...)
	return func() tea.Msg {
		return pluginEventMsg{Events: copied}
	}
}

func pluginEventDispatchCmd(mgr *plugin.Manager, runtime *pluginAsyncRuntime, events []plugin.EventContext) tea.Cmd {
	copied := append([]plugin.EventContext(nil), events...)
	return func() tea.Msg {
		var firstErr error
		for _, event := range copied {
			if event.Event == "" {
				continue
			}
			if err := mgr.DispatchEvent(runtime, event.Event, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return pluginDispatchResultMsg{Runtime: runtime, Err: firstErr}
	}
}

func pluginKeyDispatchCmd(mgr *plugin.Manager, runtime *pluginAsyncRuntime, mode, keys string) tea.Cmd {
	return func() tea.Msg {
		handled, pending, err := mgr.DispatchKey(runtime, mode, keys)
		return pluginKeyDispatchResultMsg{Runtime: runtime, Handled: handled, Pending: pending, Err: err}
	}
}

func (m *Model) triggerPluginEvents(events ...plugin.EventContext) tea.Cmd {
	if m.pluginMgr == nil || len(events) == 0 {
		return nil
	}
	// This command only posts an event message. Lua itself runs in the next
	// worker command, after Update has returned; callbacks never execute in a
	// rendering or input-routing frame.
	return pluginEventCmd(events...)
}

func (m *Model) enrichPluginEventContext(ctx plugin.EventContext) plugin.EventContext {
	if ctx.FilePath == "" || m.rootDir == "" || ctx.RelativePath != "" {
		return ctx
	}
	rel, err := filepath.Rel(m.rootDir, ctx.FilePath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ctx
	}
	ctx.RelativePath = filepath.ToSlash(rel)
	return ctx
}

func (m *Model) pluginEvent(event, path string) plugin.EventContext {
	if path == "" {
		switch event {
		case plugin.EventBufRead, plugin.EventBufEnter, plugin.EventBufLeave, plugin.EventBufWrite, plugin.EventBufNew, plugin.EventBufDelete, plugin.EventFileType:
			return plugin.EventContext{}
		}
	}
	return m.enrichPluginEventContext(plugin.EventContext{
		Event:    event,
		FilePath: path,
	})
}

func (m *Model) triggerEditorAutocmds(path string, prevVersion, newVersion int, prevCursor, newCursor text.Position) tea.Cmd {
	if path == "" && prevVersion == newVersion && prevCursor == newCursor {
		return nil
	}
	var events []plugin.EventContext
	if newVersion != prevVersion {
		events = append(events, m.pluginEvent(plugin.EventTextChanged, path))
	}
	if newCursor != prevCursor {
		events = append(events, m.pluginEvent(plugin.EventCursorMoved, path))
	}
	return m.triggerPluginEvents(events...)
}
