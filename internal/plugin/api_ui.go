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

func raiseUIUnsupported(L *lua.LState, apiName string) int {
	L.RaiseError("%s is not wired into the app yet", apiName)
	return 0
}

// ui.new_buffer() -> bufnr
func uiNewBuffer(L *lua.LState) int {
	return raiseUIUnsupported(L, "ui.new_buffer")
}

// ui.show_panel(name: string)
func uiShowPanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.show_panel")
	if err := runtime.ShowPanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.show_panel failed: %v", err)
	}
	return 0
}

// ui.hide_panel(name: string)
func uiHidePanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.hide_panel")
	if err := runtime.HidePanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.hide_panel failed: %v", err)
	}
	return 0
}

// ui.toggle_panel(name: string)
func uiTogglePanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.toggle_panel")
	if err := runtime.TogglePanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.toggle_panel failed: %v", err)
	}
	return 0
}

// ui.new_float(opts: table) -> float_id
func uiNewFloat(L *lua.LState) int {
	L.CheckTable(1)
	return raiseUIUnsupported(L, "ui.new_float")
}

// ui.close_float(float_id: number)
func uiCloseFloat(L *lua.LState) int {
	L.CheckInt(1)
	return raiseUIUnsupported(L, "ui.close_float")
}

// ui.set_highlights(ns_id, highlights: table)
func uiSetHighlights(L *lua.LState) int {
	L.CheckInt(1)
	L.CheckTable(2)
	return raiseUIUnsupported(L, "ui.set_highlights")
}

// ui.clear_highlights(ns_id?)
func uiClearHighlights(L *lua.LState) int {
	if L.GetTop() >= 1 {
		L.CheckInt(1)
	}
	return raiseUIUnsupported(L, "ui.clear_highlights")
}

// ui.input(prompt: string) -> string
func uiInput(L *lua.LState) int {
	L.CheckString(1)
	return raiseUIUnsupported(L, "ui.input")
}

// ui.confirm(message: string, options: table) -> boolean
func uiConfirm(L *lua.LState) int {
	L.CheckString(1)
	L.CheckTable(2)
	return raiseUIUnsupported(L, "ui.confirm")
}

// ui.notify(message: string, level: string?)
func uiNotify(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.notify")
	message := L.CheckString(1)
	level := ""
	if L.GetTop() >= 2 {
		level = L.CheckString(2)
	}
	runtime.Notify(message, level)
	return 0
}
