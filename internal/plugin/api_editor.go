package plugin

import (
	"fmt"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type editorCommandRegistry struct {
	mu     sync.RWMutex
	states map[*lua.LState]map[string]*lua.LFunction
}

var pluginCommands = editorCommandRegistry{
	states: make(map[*lua.LState]map[string]*lua.LFunction),
}

// registerEditorAPI registers the editor.* API functions.
func registerEditorAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), editorAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), editorAPIFunctions))
	L.Push(mod)
}

var editorAPIFunctions = map[string]lua.LGFunction{
	"command":        editorCommand,
	"feed_keys":      editorFeedKeys,
	"get_mode":       editorGetMode,
	"get_tab_count":  editorGetTabCount,
	"get_active_tab": editorGetActiveTab,
	"set_active_tab": editorSetActiveTab,
	"open_file":      editorOpenFile,
	"close_tab":      editorCloseTab,
	"next_tab":       editorNextTab,
	"prev_tab":       editorPrevTab,
	"get_width":      editorGetWidth,
	"get_height":     editorGetHeight,
	"get_status":     editorGetStatus,
	"set_status":     editorSetStatus,
	"echo":           editorEcho,
	"echo_error":     editorEchoError,
	"echo_warning":   editorEchoWarning,
	"echo_info":      editorEchoInfo,
}

// editor.command(name: string, fn?: function)
// With two args it registers a command. With one arg it executes it.
func editorCommand(L *lua.LState) int {
	name := L.CheckString(1)

	if L.GetTop() >= 2 {
		fn := L.CheckFunction(2)
		commands := getCommandsFromContext(L)
		if commands == nil {
			commands = make(map[string]*lua.LFunction)
			setCommandsInContext(L, commands)
		}
		commands[name] = fn
		return 0
	}

	if err := executeEditorCommand(L, name); err != nil {
		L.RaiseError("editor.command failed: %v", err)
	}
	return 0
}

// editor.feed_keys(keys: string)
func editorFeedKeys(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.feed_keys")
	if err := runtime.FeedKeys(L.CheckString(1)); err != nil {
		L.RaiseError("editor.feed_keys failed: %v", err)
		return 0
	}
	return 0
}

// editor.get_mode() -> string
func editorGetMode(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_mode")
	L.Push(lua.LString(runtime.Mode()))
	return 1
}

// editor.get_tab_count() -> number
func editorGetTabCount(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_tab_count")
	L.Push(lua.LNumber(runtime.TabCount()))
	return 1
}

// editor.get_active_tab() -> number
func editorGetActiveTab(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_active_tab")
	L.Push(lua.LNumber(runtime.ActiveTab() + 1))
	return 1
}

// editor.set_active_tab(tab: number)
func editorSetActiveTab(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.set_active_tab")
	if err := runtime.SetActiveTab(L.CheckInt(1) - 1); err != nil {
		L.RaiseError("editor.set_active_tab failed: %v", err)
		return 0
	}
	return 0
}

// editor.open_file(path: string) -> boolean, error?
func editorOpenFile(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.open_file")
	if err := runtime.OpenFile(L.CheckString(1)); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

// editor.close_tab(tab: number?)
func editorCloseTab(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.close_tab")
	tab := -1
	if L.GetTop() >= 1 {
		tab = L.CheckInt(1) - 1
	}
	if err := runtime.CloseTab(tab); err != nil {
		L.RaiseError("editor.close_tab failed: %v", err)
		return 0
	}
	return 0
}

// editor.next_tab()
func editorNextTab(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.next_tab")
	runtime.NextTab()
	return 0
}

// editor.prev_tab()
func editorPrevTab(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.prev_tab")
	runtime.PrevTab()
	return 0
}

// editor.get_width() -> number
func editorGetWidth(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_width")
	L.Push(lua.LNumber(runtime.Width()))
	return 1
}

// editor.get_height() -> number
func editorGetHeight(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_height")
	L.Push(lua.LNumber(runtime.Height()))
	return 1
}

// editor.get_status() -> string
func editorGetStatus(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.get_status")
	L.Push(lua.LString(runtime.Status()))
	return 1
}

// editor.set_status(text: string)
func editorSetStatus(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.set_status")
	runtime.SetStatus(L.CheckString(1))
	return 0
}

// editor.echo(text: string)
func editorEcho(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.echo")
	runtime.SetStatus(L.CheckString(1))
	return 0
}

// editor.echo_error(text: string)
func editorEchoError(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.echo_error")
	runtime.SetStatus("Error: " + L.CheckString(1))
	return 0
}

// editor.echo_warning(text: string)
func editorEchoWarning(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.echo_warning")
	runtime.SetStatus("Warning: " + L.CheckString(1))
	return 0
}

// editor.echo_info(text: string)
func editorEchoInfo(L *lua.LState) int {
	runtime := requireRuntime(L, "editor.echo_info")
	runtime.SetStatus("Info: " + L.CheckString(1))
	return 0
}

// Helper functions for context management
func getCommandsFromContext(L *lua.LState) map[string]*lua.LFunction {
	pluginCommands.mu.RLock()
	defer pluginCommands.mu.RUnlock()
	return pluginCommands.states[L]
}

func setCommandsInContext(L *lua.LState, commands map[string]*lua.LFunction) {
	pluginCommands.mu.Lock()
	defer pluginCommands.mu.Unlock()
	pluginCommands.states[L] = commands
}

func executeEditorCommand(L *lua.LState, name string) error {
	pluginCommands.mu.RLock()
	fn := pluginCommands.states[L][name]
	pluginCommands.mu.RUnlock()
	if fn == nil {
		return fmt.Errorf("command %q not found", name)
	}
	return L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
}

func clearCommandsForState(L *lua.LState) {
	pluginCommands.mu.Lock()
	defer pluginCommands.mu.Unlock()
	delete(pluginCommands.states, L)
}
