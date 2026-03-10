package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/plugin"
	"teak/internal/text"
)

type pluginEventMsg struct {
	Events []plugin.EventContext
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

func (m *Model) triggerPluginEvents(events ...plugin.EventContext) tea.Cmd {
	if m.pluginMgr == nil || len(events) == 0 {
		return nil
	}

	runtime := newPluginRuntime(m)
	m.pluginMgr.SetRuntime(runtime)
	defer m.pluginMgr.ClearRuntime()

	var firstErr error
	for _, event := range events {
		ctx := m.enrichPluginEventContext(event)
		if ctx.Event == "" {
			continue
		}
		if err := m.pluginMgr.TriggerEvent(ctx.Event, ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		m.status = fmt.Sprintf("Plugin event error: %v", firstErr)
	}
	return runtime.command()
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
