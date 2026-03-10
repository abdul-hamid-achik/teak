package plugin

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
	"teak/internal/text"
)

// Runtime exposes the current app/editor state to plugin APIs during dispatch.
// Methods operate on the active editor and current app model.
type Runtime interface {
	BufferText() (string, error)
	SetBufferText(string) error
	BufferCursor() (text.Position, error)
	SetBufferCursor(text.Position) error
	BufferSelection() (*text.Selection, error)
	InsertText(string) error
	DeleteSelection() error
	BufferLine(int) (string, error)
	BufferLineCount() (int, error)
	SaveBuffer() error
	BufferFilePath() (string, error)
	BufferDirty() (bool, error)
	Mode() string
	TabCount() int
	ActiveTab() int
	SetActiveTab(int) error
	OpenFile(string) error
	CloseTab(int) error
	NextTab()
	PrevTab()
	Width() int
	Height() int
	Status() string
	SetStatus(string)
	FeedKeys(string) error
	ShowPanel(string) error
	HidePanel(string) error
	TogglePanel(string) error
	Notify(string, string)
}

var pluginRuntimes = struct {
	mu     sync.RWMutex
	states map[*lua.LState][]Runtime
}{
	states: make(map[*lua.LState][]Runtime),
}

func setRuntimeForState(L *lua.LState, runtime Runtime) {
	pluginRuntimes.mu.Lock()
	defer pluginRuntimes.mu.Unlock()
	if runtime == nil {
		delete(pluginRuntimes.states, L)
		return
	}
	pluginRuntimes.states[L] = append(pluginRuntimes.states[L], runtime)
}

func getRuntimeFromContext(L *lua.LState) Runtime {
	pluginRuntimes.mu.RLock()
	defer pluginRuntimes.mu.RUnlock()
	stack := pluginRuntimes.states[L]
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

func clearRuntimeForState(L *lua.LState) {
	pluginRuntimes.mu.Lock()
	defer pluginRuntimes.mu.Unlock()
	stack := pluginRuntimes.states[L]
	if len(stack) <= 1 {
		delete(pluginRuntimes.states, L)
		return
	}
	pluginRuntimes.states[L] = stack[:len(stack)-1]
}

// SetRuntime installs a runtime bridge for all currently loaded plugins.
func (m *Manager) SetRuntime(runtime Runtime) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, plugin := range m.plugins {
		setRuntimeForState(plugin.State, runtime)
	}
}

// ClearRuntime removes the runtime bridge from all currently loaded plugins.
func (m *Manager) ClearRuntime() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, plugin := range m.plugins {
		clearRuntimeForState(plugin.State)
	}
}
