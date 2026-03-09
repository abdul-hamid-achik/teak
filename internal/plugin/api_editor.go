package plugin

import lua "github.com/yuin/gopher-lua"

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

// editor.command(name: string, fn: function)
func editorCommand(L *lua.LState) int {
	name := L.CheckString(1)
	fn := L.CheckFunction(2)

	// Store command in registry for later execution
	commands := getCommandsFromContext(L)
	if commands == nil {
		commands = make(map[string]lua.LFunction)
		setCommandsInContext(L, commands)
	}
	commands[name] = *fn

	return 0
}

// editor.feed_keys(keys: string)
func editorFeedKeys(L *lua.LState) int {
	keys := L.CheckString(1)
	// Queue keys to be fed to editor (would need integration with app)
	_ = keys
	return 0
}

// editor.get_mode() -> string
func editorGetMode(L *lua.LState) int {
	L.Push(lua.LString("normal"))
	return 1
}

// editor.get_tab_count() -> number
func editorGetTabCount(L *lua.LState) int {
	L.Push(lua.LNumber(1))
	return 1
}

// editor.get_active_tab() -> number
func editorGetActiveTab(L *lua.LState) int {
	L.Push(lua.LNumber(1))
	return 1
}

// editor.set_active_tab(tab: number)
func editorSetActiveTab(L *lua.LState) int {
	_ = L.CheckInt(1)
	return 0
}

// editor.open_file(path: string) -> boolean, error?
func editorOpenFile(L *lua.LState) int {
	_ = L.CheckString(1)
	L.Push(lua.LTrue)
	return 1
}

// editor.close_tab(tab: number?)
func editorCloseTab(L *lua.LState) int {
	return 0
}

// editor.next_tab()
func editorNextTab(L *lua.LState) int {
	return 0
}

// editor.prev_tab()
func editorPrevTab(L *lua.LState) int {
	return 0
}

// editor.get_width() -> number
func editorGetWidth(L *lua.LState) int {
	L.Push(lua.LNumber(80))
	return 1
}

// editor.get_height() -> number
func editorGetHeight(L *lua.LState) int {
	L.Push(lua.LNumber(24))
	return 1
}

// editor.get_status() -> string
func editorGetStatus(L *lua.LState) int {
	L.Push(lua.LString(""))
	return 1
}

// editor.set_status(text: string)
func editorSetStatus(L *lua.LState) int {
	_ = L.CheckString(1)
	return 0
}

// editor.echo(text: string)
func editorEcho(L *lua.LState) int {
	text := L.CheckString(1)
	_ = text
	return 0
}

// editor.echo_error(text: string)
func editorEchoError(L *lua.LState) int {
	text := L.CheckString(1)
	_ = text
	return 0
}

// editor.echo_warning(text: string)
func editorEchoWarning(L *lua.LState) int {
	text := L.CheckString(1)
	_ = text
	return 0
}

// editor.echo_info(text: string)
func editorEchoInfo(L *lua.LState) int {
	text := L.CheckString(1)
	_ = text
	return 0
}

// Helper functions for context management
func getCommandsFromContext(L *lua.LState) map[string]lua.LFunction {
	return nil
}

func setCommandsInContext(L *lua.LState, commands map[string]lua.LFunction) {
}
