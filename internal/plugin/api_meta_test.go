package plugin

import (
	"reflect"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestTeakMetadataAPI(t *testing.T) {
	registry := NewAPIRegistry()
	registry.Register("buffer", registerBufferAPI)
	registry.Register("editor", registerEditorAPI)
	registry.Register("keymap", registerKeymapAPI)
	registry.Register("autocmd", registerAutocmdAPI)
	registry.Register("ui", registerUIAPI)
	registry.Register("teak", registerTeakAPI(registry))

	L := newLuaStateFactory().Get()
	defer L.Close()
	registry.RegisterInState(L)

	const source = `
assert(teak.api_version() == 1)
assert(teak.event_version() == 2)
assert(teak.has_capability("buffer"))
assert(teak.has_capability("ui"))
assert(not teak.has_capability("teak"))
assert(not teak.has_capability("vim"))
assert(teak.has_ui_capability("new_buffer"))
assert(teak.has_ui_capability("notify"))
assert(teak.has_ui_capability("confirm"))
assert(teak.has_ui_capability("input"))
assert(teak.has_ui_capability("select"))
	assert(teak.has_ui_capability("new_float"))
	assert(teak.has_ui_capability("set_highlights"))
	assert(teak.has_ui_capability("clear_highlights"))
assert(teak.has_event("BufWrite"))
assert(not teak.has_event("InsertEnter"))
assert(not teak.has_event("InsertLeave"))
assert(not teak.has_event("VimEnter"))
assert(not teak.has_event("TypoEvent"))
capabilities = teak.capabilities()
ui_capabilities = teak.ui_capabilities()
events = teak.events()
`
	if err := L.DoString(source); err != nil {
		t.Fatalf("metadata API script failed: %v", err)
	}

	if got := luaStrings(L.GetGlobal("capabilities")); !reflect.DeepEqual(got, []string{"autocmd", "buffer", "editor", "keymap", "ui"}) {
		t.Fatalf("capabilities = %v", got)
	}
	if got := luaStrings(L.GetGlobal("ui_capabilities")); !reflect.DeepEqual(got, []string{
		"clear_highlights", "close_float", "confirm", "hide_panel", "input", "new_buffer", "new_float", "notify", "select", "set_highlights", "show_panel", "toggle_panel",
	}) {
		t.Fatalf("ui_capabilities = %v", got)
	}
	if got := luaStrings(L.GetGlobal("events")); !reflect.DeepEqual(got, []string{
		"BufDelete", "BufEnter", "BufLeave", "BufNew", "BufRead", "BufWrite",
		"CursorMoved", "FileType", "TextChanged",
	}) {
		t.Fatalf("events = %v", got)
	}
}

func TestManagerExposesStableMetadataToPlugins(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "metadata", `
function api_contract()
  return teak.api_version(), teak.event_version(), teak.has_capability("buffer"), teak.has_capability("vim")
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}

	mgr.luaMu.Lock()
	defer mgr.luaMu.Unlock()
	mgr.mu.RLock()
	state := mgr.plugins["metadata"].State
	mgr.mu.RUnlock()
	if err := state.CallByParam(lua.P{Fn: state.GetGlobal("api_contract"), NRet: 4, Protect: true}); err != nil {
		t.Fatalf("api_contract() error = %v", err)
	}
	vim := state.Get(-1)
	buffer := state.Get(-2)
	eventVersion := state.Get(-3)
	apiVersion := state.Get(-4)
	state.Pop(4)
	if apiVersion != lua.LNumber(1) || eventVersion != lua.LNumber(2) || buffer != lua.LTrue || vim != lua.LFalse {
		t.Fatalf("api_contract() = %v, %v, %v, %v", apiVersion, eventVersion, buffer, vim)
	}
}

func luaStrings(value lua.LValue) []string {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	result := make([]string, 0, table.Len())
	table.ForEach(func(_, value lua.LValue) {
		if stringValue, ok := value.(lua.LString); ok {
			result = append(result, string(stringValue))
		}
	})
	return result
}
