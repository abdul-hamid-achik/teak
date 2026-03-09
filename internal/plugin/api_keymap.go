package plugin

import lua "github.com/yuin/gopher-lua"

// registerKeymapAPI registers the keymap.* API functions.
func registerKeymapAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), keymapAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), keymapAPIFunctions))
	L.Push(mod)
}

var keymapAPIFunctions = map[string]lua.LGFunction{
	"set":       keymapSet,
	"unset":     keymapUnset,
	"get":       keymapGet,
	"clear":     keymapClear,
	"which_key": keymapWhichKey,
}

// keymap.set(mode, keys, action, opts?)
// mode: "n" (normal), "i" (insert), "v" (visual), "a" (all)
func keymapSet(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	// Action can be a string (command) or function
	action := L.Get(3)

	var opts map[string]interface{}
	if L.GetTop() >= 4 {
		opts = luaTableToGoMap(L.Get(4).(*lua.LTable))
	}

	// Register keybinding
	registerKeybinding(mode, keys, action, opts)

	return 0
}

// keymap.unset(mode, keys)
func keymapUnset(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	unregisterKeybinding(mode, keys)

	return 0
}

// keymap.get(mode, keys) -> action
func keymapGet(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	action := getKeybinding(mode, keys)
	if action == nil {
		L.Push(lua.LNil)
	} else {
		pushAction(L, action)
	}
	return 1
}

// keymap.clear(mode?)
func keymapClear(L *lua.LState) int {
	mode := ""
	if L.GetTop() >= 1 {
		mode = L.CheckString(1)
	}

	clearKeybindings(mode)

	return 0
}

// keymap.which_key(keys: string) -> description
func keymapWhichKey(L *lua.LState) int {
	keys := L.CheckString(1)

	desc := getKeybindingDescription(keys)
	if desc == "" {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LString(desc))
	}
	return 1
}

// Helper functions (stubs - would need integration with app)
func registerKeybinding(mode, keys string, action lua.LValue, opts map[string]interface{}) {
}

func unregisterKeybinding(mode, keys string) {
}

func getKeybinding(mode, keys string) interface{} {
	return nil
}

func clearKeybindings(mode string) {
}

func getKeybindingDescription(keys string) string {
	return ""
}

func pushAction(L *lua.LState, action interface{}) {
	L.Push(lua.LString("action"))
}

func luaTableToGoMap(t *lua.LTable) map[string]interface{} {
	result := make(map[string]interface{})
	t.ForEach(func(key, value lua.LValue) {
		if strKey, ok := key.(lua.LString); ok {
			result[string(strKey)] = value
		}
	})
	return result
}
