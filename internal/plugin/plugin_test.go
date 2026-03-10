package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestPluginManagerNew(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	if mgr == nil {
		t.Fatal("Manager should not be nil")
	}

	if mgr.plugins == nil {
		t.Fatal("Plugins map should be initialized")
	}

	if mgr.luaPool == nil {
		t.Fatal("Lua pool should be initialized")
	}
}

func TestPluginManagerLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	// Should not error when no plugins exist
	err = mgr.LoadAllPlugins()
	if err != nil {
		t.Errorf("LoadAllPlugins should not error when no plugins exist: %v", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(plugins))
	}
}

func TestPluginManagerLoadInvalidPlugin(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	// Create a directory without plugin.toml
	pluginDir := filepath.Join(dir, "invalid-plugin")
	if err := os.Mkdir(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}

	err = mgr.LoadPlugin(pluginDir)
	if err == nil {
		t.Error("LoadPlugin should error for invalid plugin")
	}
}

func TestPluginManagerLoadValidPlugin(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	// Create a valid plugin
	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}

	// Write plugin.toml
	configContent := `name = "test-plugin"
version = "1.0.0"
description = "Test plugin"
main = "init.lua"
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write plugin.toml: %v", err)
	}

	// Write minimal init.lua
	initContent := `-- Test plugin
return {}
`
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initContent), 0o644); err != nil {
		t.Fatalf("Failed to write init.lua: %v", err)
	}

	err = mgr.LoadPlugin(pluginDir)
	if err != nil {
		t.Errorf("LoadPlugin failed: %v", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 1 {
		t.Errorf("Expected 1 plugin, got %d", len(plugins))
	}

	plugin, err := mgr.GetPlugin("test-plugin")
	if err != nil {
		t.Errorf("GetPlugin failed: %v", err)
	}

	if plugin.Name != "test-plugin" {
		t.Errorf("Expected plugin name 'test-plugin', got '%s'", plugin.Name)
	}
}

func TestPluginManagerUnloadPlugin(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	// Create and load a plugin
	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}

	configContent := `name = "test-plugin"
version = "1.0.0"
main = "init.lua"
`
	os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte(configContent), 0o644)
	os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(`return {}`), 0o644)

	mgr.LoadPlugin(pluginDir)

	// Unload the plugin
	err = mgr.UnloadPlugin("test-plugin")
	if err != nil {
		t.Errorf("UnloadPlugin failed: %v", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("Expected 0 plugins after unload, got %d", len(plugins))
	}
}

func TestPluginManagerUnloadNonExistent(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	err = mgr.UnloadPlugin("non-existent")
	if err == nil {
		t.Error("UnloadPlugin should error for non-existent plugin")
	}
}

func TestPluginManagerShutdownUnloadsPlugins(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"test-plugin\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write plugin.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte("function setup() end\n"), 0o644); err != nil {
		t.Fatalf("Failed to write init.lua: %v", err)
	}

	if err := mgr.LoadAllPlugins(); err != nil {
		t.Fatalf("LoadAllPlugins() error = %v", err)
	}
	if len(mgr.ListPlugins()) != 1 {
		t.Fatalf("expected 1 loaded plugin, got %d", len(mgr.ListPlugins()))
	}

	mgr.Shutdown()

	if len(mgr.ListPlugins()) != 0 {
		t.Fatalf("expected plugins to be unloaded on shutdown, got %d", len(mgr.ListPlugins()))
	}
}

func TestPluginManagerHandleKeyExecutesCommand(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"test-plugin\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write plugin.toml: %v", err)
	}
	initLua := `
function setup()
  editor.command("hello", function()
    plugin_triggered = "yes"
  end)
  keymap.set("n", "ctrl+g", "hello")
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("Failed to write init.lua: %v", err)
	}

	if err := mgr.LoadAllPlugins(); err != nil {
		t.Fatalf("LoadAllPlugins() error = %v", err)
	}

	handled, pending, err := mgr.HandleKey("n", "ctrl+g")
	if err != nil {
		t.Fatalf("HandleKey() error = %v", err)
	}
	if !handled || pending {
		t.Fatalf("expected exact keybinding match, handled=%v pending=%v", handled, pending)
	}

	p, err := mgr.GetPlugin("test-plugin")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_triggered").String(); got != "yes" {
		t.Fatalf("plugin_triggered = %q, want %q", got, "yes")
	}
}

func TestPluginManagerLoadsPluginsWithAutocmdAPI(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"test-plugin\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write plugin.toml: %v", err)
	}
	initLua := `
	function setup()
	  autocmd.register("BufWrite", function() end)
	end
	`
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("Failed to write init.lua: %v", err)
	}

	if err := mgr.LoadPlugin(pluginDir); err != nil {
		t.Fatalf("expected autocmd setup to load successfully, got %v", err)
	}
	if len(mgr.ListPlugins()) != 1 {
		t.Fatalf("expected 1 loaded plugin, got %d", len(mgr.ListPlugins()))
	}
}

func TestPluginManagerRejectsUnsupportedUIAPI(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("Failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"test-plugin\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write plugin.toml: %v", err)
	}
	initLua := `
function setup()
  ui.notify("hello")
end
	`
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("Failed to write init.lua: %v", err)
	}

	err = mgr.LoadPlugin(pluginDir)
	if err == nil {
		t.Fatal("expected unsupported ui API setup to fail")
	}
	if !strings.Contains(err.Error(), "ui.notify is unavailable outside an active plugin dispatch context") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLuaStatePool(t *testing.T) {
	pool := newLuaStatePool()

	// Get a state
	L1 := pool.Get()
	if L1 == nil {
		t.Fatal("Get should return a Lua state")
	}

	// Put it back
	pool.Put(L1)

	// Get again - should reuse the state from pool
	L2 := pool.Get()
	if L2 == nil {
		t.Fatal("Get should return a Lua state")
	}

	// Clean up
	pool.Put(L2)
	pool.Close()
}

func TestLuaStatePoolMultipleStates(t *testing.T) {
	pool := newLuaStatePool()

	// Get multiple states
	states := make([]*lua.LState, 5)
	for i := 0; i < 5; i++ {
		states[i] = pool.Get()
		if states[i] == nil {
			t.Fatalf("Get %d should return a Lua state", i)
		}
	}

	// Put them all back
	for _, L := range states {
		pool.Put(L)
	}

	// Pool should have 5 states
	if len(pool.pool) != 5 {
		t.Errorf("Expected 5 states in pool, got %d", len(pool.pool))
	}

	pool.Close()
}

func TestAPIRegistry(t *testing.T) {
	registry := NewAPIRegistry()

	called := false
	testAPI := func(L *lua.LState) {
		called = true
		// Push a module onto the stack
		mod := L.NewTable()
		L.SetField(mod, "name", lua.LString("test"))
		L.Push(mod)
	}

	registry.Register("test", testAPI)
	registry.Register("test2", testAPI)

	L := lua.NewState()
	defer L.Close()

	registry.RegisterInState(L)

	if !called {
		t.Error("API functions should be called during registration")
	}

	// Check that modules are registered
	mod := L.GetGlobal("test")
	if mod == lua.LNil {
		t.Error("test module should be registered")
	}

	mod = L.GetGlobal("test2")
	if mod == lua.LNil {
		t.Error("test2 module should be registered")
	}
}

func TestLoadPluginConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "plugin.toml")

	configContent := `name = "test-plugin"
version = "1.0.0"
description = "Test plugin"
author = "Test Author"
main = "main.lua"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := loadPluginConfig(configPath)
	if err != nil {
		t.Fatalf("loadPluginConfig failed: %v", err)
	}

	if config.Name != "test-plugin" {
		t.Errorf("Expected name 'test-plugin', got '%s'", config.Name)
	}

	if config.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", config.Version)
	}

	if config.Description != "Test plugin" {
		t.Errorf("Expected description 'Test plugin', got '%s'", config.Description)
	}

	if config.Author != "Test Author" {
		t.Errorf("Expected author 'Test Author', got '%s'", config.Author)
	}

	if config.Main != "main.lua" {
		t.Errorf("Expected main 'main.lua', got '%s'", config.Main)
	}
}

func TestLoadPluginConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "plugin.toml")

	// Minimal config
	configContent := `name = "minimal"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := loadPluginConfig(configPath)
	if err != nil {
		t.Fatalf("loadPluginConfig failed: %v", err)
	}

	if config.Main != "init.lua" {
		t.Errorf("Expected default main 'init.lua', got '%s'", config.Main)
	}
}

func TestLoadPluginConfigSupportsInlineComments(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "plugin.toml")

	configContent := `name = "commented"
main = "plugin.lua" # inline comment that broke the old parser
description = "Works with TOML comments"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	config, err := loadPluginConfig(configPath)
	if err != nil {
		t.Fatalf("loadPluginConfig failed: %v", err)
	}
	if config.Main != "plugin.lua" {
		t.Fatalf("Expected main 'plugin.lua', got %q", config.Main)
	}
	if config.Description != "Works with TOML comments" {
		t.Fatalf("Expected description to decode correctly, got %q", config.Description)
	}
}
