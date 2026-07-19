package plugin

import lua "github.com/yuin/gopher-lua"

// luaStateFactory creates isolated Lua states for plugins. Gopher-Lua does not
// expose a complete, supported way to reset all globals, registry entries, and
// loaded code in an LState, so states are deliberately never reused.
type luaStateFactory struct{}

func newLuaStateFactory() *luaStateFactory {
	return &luaStateFactory{}
}

// Get creates a fresh, isolated Lua state.
func (f *luaStateFactory) Get() *lua.LState {
	return lua.NewState(lua.Options{
		RegistrySize:        1024 * 20,
		RegistryMaxSize:     1024 * 80,
		RegistryGrowStep:    32,
		CallStackSize:       120,
		MinimizeStackMemory: true,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})
}

// Put clears Go-side registries and permanently closes the state. It is safe
// to call after a setup or teardown failure; callers must not use L afterward.
func (f *luaStateFactory) Put(L *lua.LState) {
	if L == nil {
		return
	}

	clearKeybindingsForState(L)
	clearCommandsForState(L)
	clearAutocommandsForState(L)
	clearRuntimeForState(L)
	L.Close()
}

// Close exists for manager lifecycle symmetry. Returned states are already
// closed by Put, and loaded states are closed by Manager.Shutdown.
func (f *luaStateFactory) Close() {}
