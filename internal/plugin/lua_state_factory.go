package plugin

import lua "github.com/yuin/gopher-lua"

// luaStateFactory creates isolated Lua states for plugins. Gopher-Lua does not
// expose a complete, supported way to reset all globals, registry entries, and
// loaded code in an LState, so states are deliberately never reused.
type luaStateFactory struct{}

func newLuaStateFactory() *luaStateFactory {
	return &luaStateFactory{}
}

// sandboxedLibs are the standard libraries plugins may use. Deliberately
// excluded: io and os (filesystem and process access), package (module loading
// from disk), debug (introspection that defeats the resource budgets), and
// coroutine and channel (concurrency the budget accounting does not model).
var sandboxedLibs = []struct {
	name string
	open lua.LGFunction
}{
	{lua.BaseLibName, lua.OpenBase},
	{lua.StringLibName, lua.OpenString},
	{lua.TabLibName, lua.OpenTable},
	{lua.MathLibName, lua.OpenMath},
}

// unsafeBaseGlobals are installed by OpenBase but reintroduce exactly what the
// excluded libraries are meant to withhold: loading and executing code from
// disk or from a string. They are removed after the library is opened.
var unsafeBaseGlobals = []string{
	"dofile",
	"loadfile",
	"load",
	"loadstring",
	"require",
	"module",
}

// Get creates a fresh, isolated Lua state.
//
// Plugins previously ran with no standard library at all, which is stricter
// than the sandbox needs and broke ordinary code: without string.format a
// plugin cannot build a status line, and without pcall it cannot catch its own
// errors. The README's own quickstart calls string.format and therefore failed
// on its first run. Open the pure-computation libraries and keep withholding
// everything that touches the filesystem, processes, or the Go runtime.
func (f *luaStateFactory) Get() *lua.LState {
	L := lua.NewState(lua.Options{
		RegistrySize:        1024 * 20,
		RegistryMaxSize:     1024 * 80,
		RegistryGrowStep:    32,
		CallStackSize:       120,
		MinimizeStackMemory: true,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})

	for _, lib := range sandboxedLibs {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	for _, name := range unsafeBaseGlobals {
		L.SetGlobal(name, lua.LNil)
	}
	return L
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
