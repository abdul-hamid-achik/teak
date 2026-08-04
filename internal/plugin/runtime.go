package plugin

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
	"teak/internal/text"
)

// Runtime exposes the current app/editor state to plugin APIs during dispatch.
// Synchronous runtimes apply immediately; asynchronous runtimes replay each
// callback's logical tab/focus sequence in order before committing mutations.
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
	// NewBuffer creates and focuses a new untitled editor buffer, returning its
	// 1-based tab/buffer number.
	NewBuffer() (int, error)
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
	NewFloat(UIFloatOptions) (int, error)
	CloseFloat(int) error
	SetHighlights(UIHighlightRequest) error
	ClearHighlights(int) error
	RequestConfirm(UIConfirmRequest) error
	RequestInput(UIInputRequest) error
	RequestSelect(UISelectRequest) error
	Notify(string, string)
}

// UIConfirmRequest describes a non-blocking confirmation dialog. The callback
// is resumed by the manager after the user chooses an option or dismisses the
// dialog; the original Lua dispatch never waits for terminal input.
type UIConfirmRequest struct {
	Message    string
	Options    []string
	CallbackID uint64
}

// UIConfirmResult is passed to a callback as option, one-based index, and
// accepted boolean. A dismissal has Accepted=false, Index=0, and an empty
// Option.
type UIConfirmResult struct {
	Option   string
	Index    int
	Accepted bool
}

// UIInputRequest describes a non-blocking single-line text prompt. The
// callback is resumed with the entered value and an accepted flag.
type UIInputRequest struct {
	Prompt       string
	InitialValue string
	CallbackID   uint64
}

// UIInputResult is passed to an input callback. A dismissal has Accepted=false
// and preserves no user-entered value.
type UIInputResult struct {
	Value    string
	Accepted bool
}

// UISelectRequest describes a non-blocking fuzzy selector. The callback is
// resumed with the selected option, its one-based index, and an accepted flag.
type UISelectRequest struct {
	Prompt     string
	Options    []string
	CallbackID uint64
}

// UISelectResult is passed to a selector callback. Escape produces an empty
// option, index zero, and Accepted=false.
type UISelectResult struct {
	Option   string
	Index    int
	Accepted bool
}

// UIFloatOptions describes a bounded, read-only floating panel.
type UIFloatOptions struct {
	Title   string
	Content string
	Width   int
	Height  int
}

// UIHighlight describes a 0-based byte range in the active buffer. Empty
// style fields inherit the current editor foreground/background.
type UIHighlight struct {
	Line       int
	StartCol   int
	EndCol     int
	Foreground string
	Background string
	Bold       bool
	Underline  bool
}

// UIHighlightRequest replaces one namespace for the active buffer.
type UIHighlightRequest struct {
	Namespace  int
	Highlights []UIHighlight
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
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.setRuntimeLocked(runtime)
}

func (m *Manager) setRuntimeLocked(runtime Runtime) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, plugin := range m.plugins {
		setRuntimeForState(plugin.State, runtime)
	}
}

// ClearRuntime removes the runtime bridge from all currently loaded plugins.
func (m *Manager) ClearRuntime() {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.clearRuntimeLocked()
}

func (m *Manager) clearRuntimeLocked() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, plugin := range m.plugins {
		clearRuntimeForState(plugin.State)
	}
}

// DispatchKey serializes a key callback with its runtime bridge installed.
// It is intended for a tea.Cmd: all model-facing work must be recorded by the
// Runtime implementation and applied later on the Bubble Tea update goroutine.
func (m *Manager) DispatchKey(runtime Runtime, mode, keys string) (handled bool, pending bool, err error) {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.setRuntimeLocked(runtime)
	defer m.clearRuntimeLocked()
	return m.handleKeyLocked(mode, keys)
}

// DispatchEvent serializes an event callback with its runtime bridge installed.
func (m *Manager) DispatchEvent(runtime Runtime, event string, ctx EventContext) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.setRuntimeLocked(runtime)
	defer m.clearRuntimeLocked()
	return m.triggerEventLocked(event, ctx)
}
