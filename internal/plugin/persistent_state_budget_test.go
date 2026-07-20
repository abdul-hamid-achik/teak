package plugin

import (
	"errors"
	"strconv"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestPersistentStateBudgetHandlesCycles(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()

	cycle := L.NewTable()
	cycle.RawSetString("self", cycle)
	L.SetGlobal("cycle", cycle)

	stats, err := checkPluginPersistentStateBudget(L)
	if err != nil {
		t.Fatalf("checkPluginPersistentStateBudget() error = %v", err)
	}
	if stats.Nodes == 0 || stats.Entries == 0 {
		t.Fatalf("checkPluginPersistentStateBudget() stats = %+v, want a non-empty traversal", stats)
	}
}

func TestPersistentStateBudgetRejectsOversizedLuaRegistryRoot(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()

	retained := L.NewTable()
	for i := 1; i <= maxPluginPersistentTableSlots+1; i++ {
		retained.RawSetInt(i, lua.LNumber(i))
	}
	L.G.Registry.RawSetString("retained-by-registry", retained)

	_, err := checkPluginPersistentStateBudget(L)
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("checkPluginPersistentStateBudget() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	var budgetErr *PersistentStateBudgetError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("checkPluginPersistentStateBudget() error = %T, want *PersistentStateBudgetError", err)
	}
	if budgetErr.Limit != "table slots" {
		t.Fatalf("PersistentStateBudgetError.Limit = %q, want table slots", budgetErr.Limit)
	}
}

func TestPersistentStateBudgetIncludesGoOwnedCallbackRoots(t *testing.T) {
	tests := []struct {
		name   string
		retain func(*lua.LState, *lua.LFunction)
		clear  func(*lua.LState)
	}{
		{
			name: "keymap",
			retain: func(L *lua.LState, callback *lua.LFunction) {
				pluginKeymaps.mu.Lock()
				pluginKeymaps.states[L] = map[string]map[string]keymapBinding{"n": {"x": {action: callback}}}
				pluginKeymaps.mu.Unlock()
			},
			clear: clearKeybindingsForState,
		},
		{
			name: "command",
			retain: func(L *lua.LState, callback *lua.LFunction) {
				pluginCommands.mu.Lock()
				pluginCommands.states[L] = map[string]*lua.LFunction{"retained": callback}
				pluginCommands.mu.Unlock()
			},
			clear: clearCommandsForState,
		},
		{
			name: "autocmd",
			retain: func(L *lua.LState, callback *lua.LFunction) {
				pluginAutocommands.mu.Lock()
				pluginAutocommands.states[L] = map[string][]Autocommand{"BufWrite": {{Event: "BufWrite", Callback: callback}}}
				pluginAutocommands.mu.Unlock()
			},
			clear: clearAutocommandsForState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := newLuaStateFactory().Get()
			defer L.Close()
			defer tt.clear(L)

			retained := L.NewTable()
			for i := 1; i <= maxPluginPersistentTableSlots+1; i++ {
				retained.RawSetInt(i, lua.LNumber(i))
			}
			callback := L.NewClosure(func(*lua.LState) int { return 0 }, retained)
			tt.retain(L, callback)

			_, err := checkPluginPersistentStateBudget(L)
			if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
				t.Fatalf("checkPluginPersistentStateBudget() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
			}
		})
	}
}

func TestPluginManagerQuarantinesCallbackThatExceedsPersistentStateBudget(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "persistent-growth", `
function grow() end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	retainOversizedGlobalForTest(t, mgr, "persistent-growth")

	err = mgr.CallPlugin("persistent-growth", "grow")
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("CallPlugin() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after persistent-state budget failure = %d, want 0", got)
	}
}

func TestPluginManagerQuarantinesErroringCallbackThatRetainsOversizedState(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "persistent-growth-error", `
function grow_then_fail()
  error("intentional callback error")
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	retainOversizedGlobalForTest(t, mgr, "persistent-growth-error")

	err = mgr.CallPlugin("persistent-growth-error", "grow_then_fail")
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("CallPlugin() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after callback error and budget failure = %d, want 0", got)
	}
}

func TestPluginManagerRejectsSetupStateRetainedOnlyByKeymap(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "keymap-retention", `
function setup()
  local retained = {}
  for i = 1, `+strconv.Itoa(maxPluginPersistentTableSlots+1)+` do
    retained[i] = i
  end
  keymap.set("n", "ctrl+r", function()
    return retained[1]
  end)
end
`)

	err = mgr.LoadPlugin(dir)
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("LoadPlugin() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after setup budget failure = %d, want 0", got)
	}
}

func TestPluginManagerQuarantinesKeymapCallbackThatExceedsPersistentStateBudget(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "keymap-growth", `
function setup()
  keymap.set("n", "ctrl+m", function() end)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	retainOversizedGlobalForTest(t, mgr, "keymap-growth")

	handled, pending, err := mgr.HandleKey("n", "ctrl+m")
	if !handled || pending {
		t.Fatalf("HandleKey() handled=%v pending=%v, want true false", handled, pending)
	}
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("HandleKey() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after keymap budget failure = %d, want 0", got)
	}
}

func TestPluginManagerQuarantinesAutocmdThatExceedsPersistentStateBudget(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "autocmd-growth", `
function setup()
  autocmd.register("BufWrite", function() end)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	retainOversizedGlobalForTest(t, mgr, "autocmd-growth")

	err = mgr.TriggerEvent(EventBufWrite, EventContext{})
	if !errors.Is(err, ErrPluginPersistentStateBudgetExceeded) {
		t.Fatalf("TriggerEvent() error = %v, want ErrPluginPersistentStateBudgetExceeded", err)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after autocmd budget failure = %d, want 0", got)
	}
}

func BenchmarkPersistentStateBudgetTypicalPlugin(b *testing.B) {
	L := benchmarkPersistentState(b, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := checkPluginPersistentStateBudget(L); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPersistentStateBudgetNearEntryLimit(b *testing.B) {
	// Leave room for the built-in API tables that every loaded plugin retains.
	L := benchmarkPersistentState(b, maxPluginPersistentStateEntries-1024)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := checkPluginPersistentStateBudget(L); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkPersistentState(b *testing.B, entries int) *lua.LState {
	b.Helper()
	L := newLuaStateFactory().Get()
	b.Cleanup(L.Close)
	newManagerAPIRegistryForTest().RegisterInState(L)

	table := L.NewTable()
	for i := 1; i <= entries; i++ {
		table.RawSetInt(i, lua.LNumber(i))
	}
	L.SetGlobal("retained", table)
	if _, err := checkPluginPersistentStateBudget(L); err != nil {
		b.Fatalf("benchmark setup state is outside budget: %v", err)
	}
	return L
}

func newManagerAPIRegistryForTest() *APIRegistry {
	registry := NewAPIRegistry()
	registry.Register("buffer", registerBufferAPI)
	registry.Register("editor", registerEditorAPI)
	registry.Register("keymap", registerKeymapAPI)
	registry.Register("autocmd", registerAutocmdAPI)
	registry.Register("ui", registerUIAPI)
	return registry
}

// retainOversizedGlobalForTest constructs the already-retained graph through
// Go rather than spending the production 35 ms callback budget on a Lua loop.
// That keeps this test about persistent-state quarantine under `go test -race`
// instead of nondeterministically testing the independent CPU deadline first.
func retainOversizedGlobalForTest(t *testing.T, mgr *Manager, pluginName string) {
	t.Helper()
	mgr.luaMu.Lock()
	defer mgr.luaMu.Unlock()
	mgr.mu.RLock()
	loaded := mgr.plugins[pluginName]
	mgr.mu.RUnlock()
	if loaded == nil || loaded.State == nil {
		t.Fatalf("plugin %q is not loaded", pluginName)
	}
	retained := loaded.State.NewTable()
	for i := 1; i <= maxPluginPersistentTableSlots+1; i++ {
		retained.RawSetInt(i, lua.LNumber(i))
	}
	loaded.State.SetGlobal("retained", retained)
}
