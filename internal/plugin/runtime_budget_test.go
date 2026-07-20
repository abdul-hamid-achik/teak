package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeBudgetPlugin(t *testing.T, root, name, source string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte("name = \""+name+"\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua): %v", err)
	}
	return dir
}

func TestPluginManagerLoadPluginTimesOutInfiniteTopLevelCode(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "loop", "while true do end")
	start := time.Now()
	err = mgr.LoadPlugin(dir)
	if !errors.Is(err, ErrExecutionBudgetExceeded) {
		t.Fatalf("LoadPlugin() error = %v, want ErrExecutionBudgetExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > pluginLoadBudget+500*time.Millisecond {
		t.Fatalf("LoadPlugin() took %s, budget is %s", elapsed, pluginLoadBudget)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins = %d, want 0", got)
	}
}

func TestPluginManagerQuarantinesInfiniteKeyActionWithoutLeakingGoroutines(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "key-loop", `
function setup()
  keymap.set("n", "ctrl+g", function() while true do end end)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin(): %v", err)
	}
	before := runtime.NumGoroutine()
	start := time.Now()
	handled, pending, err := mgr.HandleKey("n", "ctrl+g")
	if !handled || pending {
		t.Fatalf("HandleKey() handled=%v pending=%v, want true false", handled, pending)
	}
	if !errors.Is(err, ErrExecutionBudgetExceeded) {
		t.Fatalf("HandleKey() error = %v, want ErrExecutionBudgetExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > pluginActionBudget+500*time.Millisecond {
		t.Fatalf("HandleKey() took %s, budget is %s", elapsed, pluginActionBudget)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after timeout = %d, want 0", got)
	}
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines grew from %d to %d", before, after)
	}
}

func TestPluginManagerQuarantinesInfiniteAutocommand(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "event-loop", `
function setup()
  autocmd.register("BufWrite", function() while true do end end)
end
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin(): %v", err)
	}
	start := time.Now()
	err = mgr.TriggerEvent(EventBufWrite, EventContext{})
	if !errors.Is(err, ErrExecutionBudgetExceeded) {
		t.Fatalf("TriggerEvent() error = %v, want ErrExecutionBudgetExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > pluginAutocmdBudget+500*time.Millisecond {
		t.Fatalf("TriggerEvent() took %s, budget is %s", elapsed, pluginAutocmdBudget)
	}
	if got := len(mgr.ListPlugins()); got != 0 {
		t.Fatalf("loaded plugins after timeout = %d, want 0", got)
	}
}
