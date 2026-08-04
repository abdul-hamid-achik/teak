package plugin

import (
	"slices"

	lua "github.com/yuin/gopher-lua"
)

// These versions describe the public Lua contract, not the Teak binary
// version. A plugin can use them to fail clearly when it needs a newer API.
const (
	PluginAPIVersion = 1
	// Event API v2 removes InsertEnter/InsertLeave, which Teak never
	// dispatched because it has no modal insert mode. Older manifests remain
	// loadable, but feature detection exposes the corrected vocabulary.
	PluginEventAPIVersion = 2
)

// stablePluginEvents is the event vocabulary promised by the public plugin
// API. The legacy EventVim* constants remain internal compatibility details;
// they are intentionally not advertised as part of Teak's non-Vim API.
var stablePluginEvents = []string{
	EventBufRead,
	EventBufEnter,
	EventBufLeave,
	EventBufWrite,
	EventBufNew,
	EventBufDelete,
	EventTextChanged,
	EventCursorMoved,
	EventFileType,
}

// stablePluginUICapabilities lists ui.* functions whose runtime contract is
// implemented for both synchronous and replayable asynchronous dispatch. The
// module itself remains available while unfinished interactive widgets are
// deliberately excluded from this capability list.
var stablePluginUICapabilities = []string{
	"clear_highlights",
	"close_float",
	"confirm",
	"hide_panel",
	"input",
	"new_buffer",
	"new_float",
	"notify",
	"select",
	"set_highlights",
	"show_panel",
	"toggle_panel",
}

// registerTeakAPI registers the stable metadata and capability API.
func registerTeakAPI(registry *APIRegistry) APIFunc {
	return func(L *lua.LState) {
		mod := L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
			"api_version":       teakAPIVersion,
			"event_version":     teakEventVersion,
			"capabilities":      teakCapabilities(registry),
			"has_capability":    teakHasCapability(registry),
			"ui_capabilities":   teakUICapabilities,
			"has_ui_capability": teakHasUICapability,
			"events":            teakEvents,
			"has_event":         teakHasEvent,
		})
		L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
			"api_version":       teakAPIVersion,
			"event_version":     teakEventVersion,
			"capabilities":      teakCapabilities(registry),
			"has_capability":    teakHasCapability(registry),
			"ui_capabilities":   teakUICapabilities,
			"has_ui_capability": teakHasUICapability,
			"events":            teakEvents,
			"has_event":         teakHasEvent,
		}))
		L.Push(mod)
	}
}

func teakAPIVersion(L *lua.LState) int {
	L.Push(lua.LNumber(PluginAPIVersion))
	return 1
}

func teakEventVersion(L *lua.LState) int {
	L.Push(lua.LNumber(PluginEventAPIVersion))
	return 1
}

func teakCapabilities(registry *APIRegistry) lua.LGFunction {
	return func(L *lua.LState) int {
		result := L.NewTable()
		for _, name := range registry.Names() {
			// `teak` is the metadata namespace, not a capability plugin code
			// should use as a feature module.
			if name == "teak" {
				continue
			}
			result.Append(lua.LString(name))
		}
		L.Push(result)
		return 1
	}
}

func teakHasCapability(registry *APIRegistry) lua.LGFunction {
	return func(L *lua.LState) int {
		name := L.CheckString(1)
		for _, capability := range registry.Names() {
			if capability == name && capability != "teak" {
				L.Push(lua.LTrue)
				return 1
			}
		}
		L.Push(lua.LFalse)
		return 1
	}
}

func teakUICapabilities(L *lua.LState) int {
	result := L.NewTable()
	for _, name := range stablePluginUICapabilities {
		result.Append(lua.LString(name))
	}
	L.Push(result)
	return 1
}

func teakHasUICapability(L *lua.LState) int {
	name := L.CheckString(1)
	L.Push(lua.LBool(slices.Contains(stablePluginUICapabilities, name)))
	return 1
}

func teakEvents(L *lua.LState) int {
	events := append([]string(nil), stablePluginEvents...)
	slices.Sort(events)
	result := L.NewTable()
	for _, event := range events {
		result.Append(lua.LString(event))
	}
	L.Push(result)
	return 1
}

func teakHasEvent(L *lua.LState) int {
	name := L.CheckString(1)
	L.Push(lua.LBool(isStablePluginEvent(name)))
	return 1
}

func isStablePluginEvent(name string) bool {
	return slices.Contains(stablePluginEvents, name)
}

// isKnownPluginEvent includes the two legacy lifecycle events that are still
// dispatched internally. They remain accepted for existing plugins, but are
// intentionally absent from teak.events()/teak.has_event() because they are
// not part of the stable non-Vim contract.
func isKnownPluginEvent(name string) bool {
	if isStablePluginEvent(name) {
		return true
	}
	return name == EventVimEnter || name == EventVimLeave
}
