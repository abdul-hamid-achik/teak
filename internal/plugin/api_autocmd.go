package plugin

import (
	"path"
	"path/filepath"
	"slices"
	"sync"

	lua "github.com/yuin/gopher-lua"
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
	Pattern  string // File pattern (for example, "*.go")
	Group    string // Augroup name
	Once     bool   // Run only once
}

// EventContext describes the app state for an autocmd callback.
type EventContext struct {
	Event        string
	FilePath     string
	RelativePath string
}

type autocmdRegistry struct {
	mu     sync.RWMutex
	states map[*lua.LState]map[string][]Autocommand
}

var pluginAutocommands = autocmdRegistry{
	states: make(map[*lua.LState]map[string][]Autocommand),
}

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

	cmd := Autocommand{
		Event:    event,
		Callback: callback,
	}

	if L.GetTop() >= 3 {
		opts := L.CheckTable(3)
		opts.ForEach(func(key, value lua.LValue) {
			k, ok := key.(lua.LString)
			if !ok {
				return
			}
			switch string(k) {
			case "pattern":
				if pattern, ok := value.(lua.LString); ok {
					cmd.Pattern = string(pattern)
				}
			case "group":
				if group, ok := value.(lua.LString); ok {
					cmd.Group = string(group)
				}
			case "once":
				cmd.Once = lua.LVAsBool(value)
			}
		})
	}

	pluginAutocommands.mu.Lock()
	defer pluginAutocommands.mu.Unlock()

	stateEvents := pluginAutocommands.ensureStateLocked(L)
	stateEvents[event] = append(stateEvents[event], cmd)
	return 0
}

// autocmd.unregister(event, callback?)
func autocmdUnregister(L *lua.LState) int {
	event := L.CheckString(1)
	var callback *lua.LFunction
	if L.GetTop() >= 2 {
		callback = L.CheckFunction(2)
	}

	pluginAutocommands.mu.Lock()
	defer pluginAutocommands.mu.Unlock()

	stateEvents := pluginAutocommands.states[L]
	if stateEvents == nil {
		return 0
	}
	if callback == nil {
		delete(stateEvents, event)
	} else {
		cmds := stateEvents[event]
		filtered := cmds[:0]
		for _, cmd := range cmds {
			if cmd.Callback != callback {
				filtered = append(filtered, cmd)
			}
		}
		if len(filtered) == 0 {
			delete(stateEvents, event)
		} else {
			stateEvents[event] = filtered
		}
	}
	if len(stateEvents) == 0 {
		delete(pluginAutocommands.states, L)
	}
	return 0
}

// autocmd.clear(event?)
func autocmdClear(L *lua.LState) int {
	event := ""
	if L.GetTop() >= 1 {
		event = L.CheckString(1)
	}

	pluginAutocommands.mu.Lock()
	defer pluginAutocommands.mu.Unlock()

	if event == "" {
		delete(pluginAutocommands.states, L)
		return 0
	}

	stateEvents := pluginAutocommands.states[L]
	if stateEvents == nil {
		return 0
	}
	delete(stateEvents, event)
	if len(stateEvents) == 0 {
		delete(pluginAutocommands.states, L)
	}
	return 0
}

// autocmd.list(event?) -> table
func autocmdList(L *lua.LState) int {
	event := ""
	if L.GetTop() >= 1 {
		event = L.CheckString(1)
	}

	pluginAutocommands.mu.RLock()
	defer pluginAutocommands.mu.RUnlock()

	result := L.NewTable()
	stateEvents := pluginAutocommands.states[L]
	if stateEvents == nil {
		L.Push(result)
		return 1
	}

	appendCmd := func(cmd Autocommand) {
		entry := L.NewTable()
		L.SetField(entry, "event", lua.LString(cmd.Event))
		L.SetField(entry, "pattern", lua.LString(cmd.Pattern))
		L.SetField(entry, "group", lua.LString(cmd.Group))
		L.SetField(entry, "once", lua.LBool(cmd.Once))
		result.Append(entry)
	}

	if event != "" {
		for _, cmd := range stateEvents[event] {
			appendCmd(cmd)
		}
		L.Push(result)
		return 1
	}

	events := make([]string, 0, len(stateEvents))
	for name := range stateEvents {
		events = append(events, name)
	}
	// Stable ordering makes tests and plugin behavior deterministic.
	slices.Sort(events)
	for _, name := range events {
		for _, cmd := range stateEvents[name] {
			appendCmd(cmd)
		}
	}
	L.Push(result)
	return 1
}

func (r *autocmdRegistry) ensureStateLocked(L *lua.LState) map[string][]Autocommand {
	stateEvents := r.states[L]
	if stateEvents == nil {
		stateEvents = make(map[string][]Autocommand)
		r.states[L] = stateEvents
	}
	return stateEvents
}

func clearAutocommandsForState(L *lua.LState) {
	pluginAutocommands.mu.Lock()
	defer pluginAutocommands.mu.Unlock()
	delete(pluginAutocommands.states, L)
}

func triggerAutocommandsForState(L *lua.LState, ctx EventContext) error {
	pluginAutocommands.mu.RLock()
	stateEvents := pluginAutocommands.states[L]
	if stateEvents == nil {
		pluginAutocommands.mu.RUnlock()
		return nil
	}
	candidates := append([]Autocommand(nil), stateEvents[ctx.Event]...)
	pluginAutocommands.mu.RUnlock()

	var firstErr error
	var onceCallbacks []*lua.LFunction
	for _, cmd := range candidates {
		if !matchesAutocmdPattern(cmd.Pattern, ctx) {
			continue
		}
		if err := callAutocommand(L, cmd, ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		if cmd.Once {
			onceCallbacks = append(onceCallbacks, cmd.Callback)
		}
	}

	if len(onceCallbacks) > 0 {
		removeOnceAutocommands(L, ctx.Event, onceCallbacks)
	}

	return firstErr
}

func callAutocommand(L *lua.LState, cmd Autocommand, ctx EventContext) error {
	eventTable := L.NewTable()
	L.SetField(eventTable, "event", lua.LString(ctx.Event))
	if ctx.FilePath != "" {
		L.SetField(eventTable, "file", lua.LString(ctx.FilePath))
	}
	if ctx.RelativePath != "" {
		L.SetField(eventTable, "relative_path", lua.LString(ctx.RelativePath))
	}
	return L.CallByParam(lua.P{
		Fn:      cmd.Callback,
		NRet:    0,
		Protect: true,
	}, eventTable)
}

func removeOnceAutocommands(L *lua.LState, event string, callbacks []*lua.LFunction) {
	pluginAutocommands.mu.Lock()
	defer pluginAutocommands.mu.Unlock()

	stateEvents := pluginAutocommands.states[L]
	if stateEvents == nil {
		return
	}
	cmds := stateEvents[event]
	if len(cmds) == 0 {
		return
	}
	filtered := cmds[:0]
	for _, cmd := range cmds {
		if containsAutocmdCallback(callbacks, cmd.Callback) {
			continue
		}
		filtered = append(filtered, cmd)
	}
	if len(filtered) == 0 {
		delete(stateEvents, event)
	} else {
		stateEvents[event] = filtered
	}
	if len(stateEvents) == 0 {
		delete(pluginAutocommands.states, L)
	}
}

func containsAutocmdCallback(callbacks []*lua.LFunction, callback *lua.LFunction) bool {
	for _, fn := range callbacks {
		if fn == callback {
			return true
		}
	}
	return false
}

func matchesAutocmdPattern(pattern string, ctx EventContext) bool {
	if pattern == "" {
		return true
	}
	pattern = filepath.ToSlash(pattern)
	for _, candidate := range autocmdMatchCandidates(ctx) {
		ok, err := path.Match(pattern, candidate)
		if err == nil && ok {
			return true
		}
	}
	return false
}

func autocmdMatchCandidates(ctx EventContext) []string {
	candidates := make([]string, 0, 4)
	add := func(value string) {
		if value == "" {
			return
		}
		value = filepath.ToSlash(value)
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
		base := path.Base(value)
		if base != value {
			for _, existing := range candidates {
				if existing == base {
					return
				}
			}
			candidates = append(candidates, base)
		}
	}
	add(ctx.RelativePath)
	add(ctx.FilePath)
	return candidates
}
