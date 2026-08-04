package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func pluginCallbackIDForTest(t *testing.T, mgr *Manager, name string) uint64 {
	t.Helper()
	mgr.luaMu.Lock()
	defer mgr.luaMu.Unlock()
	mgr.mu.RLock()
	state := mgr.plugins[name].State
	mgr.mu.RUnlock()
	callback, ok := state.GetGlobal("callback").(*lua.LFunction)
	if !ok {
		t.Fatal("callback global is not a Lua function")
	}
	id, err := registerUIConfirmCallback(state, callback)
	if err != nil {
		t.Fatalf("registerUIConfirmCallback() error = %v", err)
	}
	return id
}

func TestDispatchUIConfirmConsumesCallbackExactlyOnce(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	dir := writeBudgetPlugin(t, mgr.pluginDir, "confirm", `
function callback(option, index, accepted)
  result = option .. ":" .. tostring(index) .. ":" .. tostring(accepted)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	id := pluginCallbackIDForTest(t, mgr, "confirm")
	if err := mgr.DispatchUIConfirm(nil, id, UIConfirmResult{Option: "Deploy", Index: 1, Accepted: true}); err != nil {
		t.Fatalf("DispatchUIConfirm() error = %v", err)
	}
	p, err := mgr.GetPlugin("confirm")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.State.GetGlobal("result").String(); got != "Deploy:1:true" {
		t.Fatalf("callback result = %q, want Deploy:1:true", got)
	}
	if err := mgr.DispatchUIConfirm(nil, id, UIConfirmResult{}); err == nil {
		t.Fatal("DispatchUIConfirm() reused a consumed callback")
	}
}

func TestUnloadPluginDiscardsPendingUIConfirmCallback(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := writeBudgetPlugin(t, mgr.pluginDir, "confirm", "function callback() end")
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	id := pluginCallbackIDForTest(t, mgr, "confirm")
	if err := mgr.UnloadPlugin("confirm"); err != nil {
		t.Fatalf("UnloadPlugin() error = %v", err)
	}
	if err := mgr.DispatchUIConfirm(nil, id, UIConfirmResult{}); err == nil {
		t.Fatal("DispatchUIConfirm() used a callback after plugin unload")
	}
	mgr.Shutdown()
}

func TestDispatchUIInputConsumesCallbackExactlyOnce(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	dir := writeBudgetPlugin(t, mgr.pluginDir, "input", `
function callback(value, accepted)
  result = value .. ":" .. tostring(accepted)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	id := pluginCallbackIDForTest(t, mgr, "input")
	if err := mgr.DispatchUIInput(nil, id, UIInputResult{Value: "feature/x", Accepted: true}); err != nil {
		t.Fatalf("DispatchUIInput() error = %v", err)
	}
	p, err := mgr.GetPlugin("input")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.State.GetGlobal("result").String(); got != "feature/x:true" {
		t.Fatalf("callback result = %q, want feature/x:true", got)
	}
	if err := mgr.DispatchUIInput(nil, id, UIInputResult{}); err == nil {
		t.Fatal("DispatchUIInput() reused a consumed callback")
	}
}

func TestDispatchUISelectConsumesCallbackExactlyOnce(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	dir := writeBudgetPlugin(t, mgr.pluginDir, "select", `
function callback(option, index, accepted)
  result = option .. ":" .. tostring(index) .. ":" .. tostring(accepted)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}
	id := pluginCallbackIDForTest(t, mgr, "select")
	if err := mgr.DispatchUISelect(nil, id, UISelectResult{Option: "two", Index: 2, Accepted: true}); err != nil {
		t.Fatalf("DispatchUISelect() error = %v", err)
	}
	p, err := mgr.GetPlugin("select")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.State.GetGlobal("result").String(); got != "two:2:true" {
		t.Fatalf("callback result = %q, want two:2:true", got)
	}
	if err := mgr.DispatchUISelect(nil, id, UISelectResult{}); err == nil {
		t.Fatal("DispatchUISelect() reused a consumed callback")
	}
}
