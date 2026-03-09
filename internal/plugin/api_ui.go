package plugin

import lua "github.com/yuin/gopher-lua"

// registerUIAPI registers the ui.* API functions.
func registerUIAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), uiAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), uiAPIFunctions))
	L.Push(mod)
}

var uiAPIFunctions = map[string]lua.LGFunction{
	"new_buffer":       uiNewBuffer,
	"show_panel":       uiShowPanel,
	"hide_panel":       uiHidePanel,
	"toggle_panel":     uiTogglePanel,
	"new_float":        uiNewFloat,
	"close_float":      uiCloseFloat,
	"set_highlights":   uiSetHighlights,
	"clear_highlights": uiClearHighlights,
	"input":            uiInput,
	"confirm":          uiConfirm,
	"notify":           uiNotify,
}

// ui.new_buffer() -> bufnr
func uiNewBuffer(L *lua.LState) int {
	L.Push(lua.LNumber(1))
	return 1
}

// ui.show_panel(name: string)
func uiShowPanel(L *lua.LState) int {
	name := L.CheckString(1)
	_ = name
	return 0
}

// ui.hide_panel(name: string)
func uiHidePanel(L *lua.LState) int {
	name := L.CheckString(1)
	_ = name
	return 0
}

// ui.toggle_panel(name: string)
func uiTogglePanel(L *lua.LState) int {
	name := L.CheckString(1)
	_ = name
	return 0
}

// ui.new_float(opts: table) -> float_id
func uiNewFloat(L *lua.LState) int {
	opts := L.CheckTable(1)
	_ = opts
	L.Push(lua.LNumber(1))
	return 1
}

// ui.close_float(float_id: number)
func uiCloseFloat(L *lua.LState) int {
	floatID := L.CheckInt(1)
	_ = floatID
	return 0
}

// ui.set_highlights(ns_id, highlights: table)
func uiSetHighlights(L *lua.LState) int {
	nsID := L.CheckInt(1)
	highlights := L.CheckTable(2)
	_ = nsID
	_ = highlights
	return 0
}

// ui.clear_highlights(ns_id?)
func uiClearHighlights(L *lua.LState) int {
	nsID := -1
	if L.GetTop() >= 1 {
		nsID = L.CheckInt(1)
	}
	_ = nsID
	return 0
}

// ui.input(prompt: string) -> string
func uiInput(L *lua.LState) int {
	prompt := L.CheckString(1)
	_ = prompt
	L.Push(lua.LString(""))
	return 1
}

// ui.confirm(message: string, options: table) -> boolean
func uiConfirm(L *lua.LState) int {
	message := L.CheckString(1)
	options := L.CheckTable(2)
	_ = message
	_ = options
	L.Push(lua.LTrue)
	return 1
}

// ui.notify(message: string, level: string?)
func uiNotify(L *lua.LState) int {
	message := L.CheckString(1)
	level := "info"
	if L.GetTop() >= 2 {
		level = L.CheckString(2)
	}
	_ = message
	_ = level
	return 0
}
