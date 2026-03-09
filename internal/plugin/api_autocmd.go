package plugin

import (
	lua "github.com/yuin/gopher-lua"
	"sync"
)

// Event types
const (
	EventBufRead     = "BufRead"
	EventBufEnter    = "BufEnter"
	EventBufLeave    = "BufLeave"
	EventBufWrite    = "BufWrite"
	EventBufNew      = "BufNew"
	EventBufDelete   = "BufDelete"
	EventInsertEnter = "InsertEnter"
	EventInsertLeave = "InsertLeave"
	EventTextChanged = "TextChanged"
	EventCursorMoved = "CursorMoved"
	EventFileType    = "FileType"
	EventVimEnter    = "VimEnter"
	EventVimLeave    = "VimLeave"
)

// Autocommand represents a registered autocommand.
type Autocommand struct {
	Event    string
	Callback *lua.LFunction
	Pattern  string // File pattern (e.g., "*.go")
	Group    string // Augroup name
	Once     bool   // Run only once
}

var (
	autocommandsMu sync.RWMutex
	autocommands   = make(map[string][]Autocommand)
)

// registerAutocmdAPI registers the autocmd.* API functions.
func registerAutocmdAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), autocmdAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), autocmdAPIFunctions))
	L.Push(mod)
}

var autocmdAPIFunctions = map[string]lua.LGFunction{
	"register":   autocmdRegister,
	"unregister": autocmdUnregister,
	"clear":      autocmdClear,
	"list":       autocmdList,
}

// autocmd.register(event, callback, opts?)
func autocmdRegister(L *lua.LState) int {
	event := L.CheckString(1)
	callback := L.CheckFunction(2)

	var opts map[string]interface{}
	if L.GetTop() >= 3 {
		opts = luaTableToGoMap(L.Get(3).(*lua.LTable))
	}

	ac := Autocommand{
		Event:    event,
		Callback: callback,
	}

	if pattern, ok := opts["pattern"].(string); ok {
		ac.Pattern = pattern
	}
	if group, ok := opts["group"].(string); ok {
		ac.Group = group
	}
	if once, ok := opts["once"].(bool); ok {
		ac.Once = once
	}

	autocommandsMu.Lock()
	autocommands[event] = append(autocommands[event], ac)
	autocommandsMu.Unlock()

	return 0
}

// autocmd.unregister(event, callback?)
func autocmdUnregister(L *lua.LState) int {
	event := L.CheckString(1)

	autocommandsMu.Lock()
	defer autocommandsMu.Unlock()

	if _, ok := autocommands[event]; ok {
		// Remove all callbacks for this event if no callback specified
		if L.GetTop() < 2 {
			delete(autocommands, event)
		} else {
			// Remove specific callback (simplified - would need better matching in production)
			delete(autocommands, event)
		}
	}

	return 0
}

// autocmd.clear(event?)
func autocmdClear(L *lua.LState) int {
	event := ""
	if L.GetTop() >= 1 {
		event = L.CheckString(1)
	}

	autocommandsMu.Lock()
	defer autocommandsMu.Unlock()

	if event == "" {
		autocommands = make(map[string][]Autocommand)
	} else {
		delete(autocommands, event)
	}

	return 0
}

// autocmd.list(event?) -> table
func autocmdList(L *lua.LState) int {
	event := ""
	if L.GetTop() >= 1 {
		event = L.CheckString(1)
	}

	autocommandsMu.RLock()
	defer autocommandsMu.RUnlock()

	result := L.NewTable()
	if event == "" {
		for ev, cmds := range autocommands {
			cmdList := L.NewTable()
			for i, cmd := range cmds {
				cmdList.RawSetInt(i+1, lua.LString(cmd.Event))
			}
			result.RawSetString(ev, cmdList)
		}
	} else {
		if cmds, ok := autocommands[event]; ok {
			for i, cmd := range cmds {
				result.RawSetInt(i+1, lua.LString(cmd.Event))
			}
		}
	}

	L.Push(result)
	return 1
}

// TriggerEvent triggers all autocommands for an event.
// Note: This requires proper integration with the app to get the Lua state.
// For now, this is a placeholder.
func TriggerEvent(event string) {
	// Placeholder - would need proper Lua state management
	_ = event
}
