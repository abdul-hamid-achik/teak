package plugin

import (
	"fmt"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type keymapBinding struct {
	action      lua.LValue
	description string
}

type keymapRegistry struct {
	mu     sync.RWMutex
	states map[*lua.LState]map[string]map[string]keymapBinding
}

var pluginKeymaps = keymapRegistry{
	states: make(map[*lua.LState]map[string]map[string]keymapBinding),
}

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
// Common modes are "n" (editor keys), "a" (all dispatch contexts), and
// app focus modes such as "tree", "git", "problems", "debugger", and "agent".
// Other mode strings are stored as-is and depend on the app dispatching them.
func keymapSet(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	// Action can be a string (command) or function
	action := L.Get(3)

	var opts map[string]interface{}
	if L.GetTop() >= 4 {
		table, ok := L.Get(4).(*lua.LTable)
		if !ok {
			L.ArgError(4, "expected table")
			return 0
		}
		opts = luaTableToGoMap(table)
	}

	if err := registerKeybinding(L, mode, keys, action, opts); err != nil {
		L.RaiseError("keymap.set failed: %v", err)
	}

	return 0
}

// keymap.unset(mode, keys)
func keymapUnset(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	unregisterKeybinding(L, mode, keys)

	return 0
}

// keymap.get(mode, keys) -> action
func keymapGet(L *lua.LState) int {
	mode := L.CheckString(1)
	keys := L.CheckString(2)

	action := getKeybinding(L, mode, keys)
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

	clearKeybindings(L, mode)

	return 0
}

// keymap.which_key(keys: string) -> description
func keymapWhichKey(L *lua.LState) int {
	keys := L.CheckString(1)

	desc := getKeybindingDescription(L, keys)
	if desc == "" {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LString(desc))
	}
	return 1
}

func registerKeybinding(L *lua.LState, mode, keys string, action lua.LValue, opts map[string]interface{}) error {
	if mode == "" || keys == "" {
		return fmt.Errorf("mode and keys are required")
	}
	description := ""
	if opts != nil {
		if desc, ok := opts["desc"].(lua.LString); ok {
			description = string(desc)
		}
	}

	pluginKeymaps.mu.Lock()
	defer pluginKeymaps.mu.Unlock()

	modeBindings := pluginKeymaps.ensureStateMode(L, mode)
	modeBindings[keys] = keymapBinding{
		action:      action,
		description: description,
	}
	return nil
}

func unregisterKeybinding(L *lua.LState, mode, keys string) {
	pluginKeymaps.mu.Lock()
	defer pluginKeymaps.mu.Unlock()

	stateBindings := pluginKeymaps.states[L]
	if stateBindings == nil {
		return
	}
	modeBindings := stateBindings[mode]
	if modeBindings == nil {
		return
	}
	delete(modeBindings, keys)
	if len(modeBindings) == 0 {
		delete(stateBindings, mode)
	}
	if len(stateBindings) == 0 {
		delete(pluginKeymaps.states, L)
	}
}

func getKeybinding(L *lua.LState, mode, keys string) lua.LValue {
	pluginKeymaps.mu.RLock()
	defer pluginKeymaps.mu.RUnlock()

	binding, exact, _ := matchKeybindingLocked(pluginKeymaps.states[L], mode, keys)
	if exact {
		return binding.action
	}
	return nil
}

func clearKeybindings(L *lua.LState, mode string) {
	pluginKeymaps.mu.Lock()
	defer pluginKeymaps.mu.Unlock()

	if mode == "" {
		delete(pluginKeymaps.states, L)
		return
	}

	stateBindings := pluginKeymaps.states[L]
	if stateBindings == nil {
		return
	}
	delete(stateBindings, mode)
	if len(stateBindings) == 0 {
		delete(pluginKeymaps.states, L)
	}
}

func clearKeybindingsForState(L *lua.LState) {
	clearKeybindings(L, "")
}

func matchKeybinding(L *lua.LState, mode, keys string) (binding keymapBinding, exact bool, prefix bool) {
	pluginKeymaps.mu.RLock()
	defer pluginKeymaps.mu.RUnlock()
	return matchKeybindingLocked(pluginKeymaps.states[L], mode, keys)
}

func getKeybindingDescription(L *lua.LState, keys string) string {
	pluginKeymaps.mu.RLock()
	defer pluginKeymaps.mu.RUnlock()

	stateBindings := pluginKeymaps.states[L]
	if stateBindings == nil {
		return ""
	}
	for _, modeBindings := range stateBindings {
		if binding, ok := modeBindings[keys]; ok && binding.description != "" {
			return binding.description
		}
	}
	return ""
}

func pushAction(L *lua.LState, action lua.LValue) {
	L.Push(action)
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

func (r *keymapRegistry) ensureStateMode(L *lua.LState, mode string) map[string]keymapBinding {
	stateBindings := r.states[L]
	if stateBindings == nil {
		stateBindings = make(map[string]map[string]keymapBinding)
		r.states[L] = stateBindings
	}
	modeBindings := stateBindings[mode]
	if modeBindings == nil {
		modeBindings = make(map[string]keymapBinding)
		stateBindings[mode] = modeBindings
	}
	return modeBindings
}

func matchKeybindingLocked(stateBindings map[string]map[string]keymapBinding, mode, keys string) (binding keymapBinding, exact bool, prefix bool) {
	if stateBindings == nil {
		return keymapBinding{}, false, false
	}
	checkModes := []string{mode}
	if mode != "a" {
		checkModes = append(checkModes, "a")
	}
	for _, keyMode := range checkModes {
		for boundKeys, boundBinding := range stateBindings[keyMode] {
			switch {
			case boundKeys == keys:
				return boundBinding, true, false
			case strings.HasPrefix(boundKeys, keys):
				prefix = true
			}
		}
	}
	return keymapBinding{}, false, prefix
}
