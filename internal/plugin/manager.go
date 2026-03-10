package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/BurntSushi/toml"
	lua "github.com/yuin/gopher-lua"
)

// Manager handles Lua plugin lifecycle and state management.
type Manager struct {
	mu          sync.RWMutex
	plugins     map[string]*Plugin
	pluginDir   string
	luaPool     *luaStatePool
	apiRegistry *APIRegistry
	loaded      bool
}

// Plugin represents a loaded plugin.
type Plugin struct {
	Name    string
	Path    string
	State   *lua.LState
	Config  PluginConfig
	Enabled bool
}

// PluginConfig holds plugin metadata.
type PluginConfig struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	Main        string `toml:"main"`
}

// NewManager creates a new plugin manager.
func NewManager(pluginDir string) (*Manager, error) {
	m := &Manager{
		plugins:     make(map[string]*Plugin),
		pluginDir:   pluginDir,
		luaPool:     newLuaStatePool(),
		apiRegistry: NewAPIRegistry(),
	}

	// Register built-in APIs
	m.registerAPIs()

	return m, nil
}

// DefaultDir returns the default plugin directory.
func DefaultDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "teak", "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "teak", "plugins")
	}
	return filepath.Join(home, ".config", "teak", "plugins")
}

// registerAPIs registers all built-in Lua APIs.
func (m *Manager) registerAPIs() {
	m.apiRegistry.Register("buffer", registerBufferAPI)
	m.apiRegistry.Register("editor", registerEditorAPI)
	m.apiRegistry.Register("keymap", registerKeymapAPI)
	m.apiRegistry.Register("autocmd", registerAutocmdAPI)
	m.apiRegistry.Register("ui", registerUIAPI)
}

// LoadPlugin loads a plugin from disk.
func (m *Manager) LoadPlugin(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Read plugin config
	configPath := filepath.Join(path, "plugin.toml")
	config, err := loadPluginConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load plugin config: %w", err)
	}

	// Check if already loaded
	if _, exists := m.plugins[config.Name]; exists {
		return fmt.Errorf("plugin %s already loaded", config.Name)
	}

	// Create new Lua state
	L := m.luaPool.Get()

	// Register APIs in this state
	m.apiRegistry.RegisterInState(L)

	// Set plugin context
	L.SetGlobal("plugin_name", lua.LString(config.Name))
	L.SetGlobal("plugin_version", lua.LString(config.Version))

	// Load main plugin file
	mainFile := filepath.Join(path, config.Main)
	if err := L.DoFile(mainFile); err != nil {
		m.luaPool.Put(L)
		return fmt.Errorf("failed to load plugin %s: %w", config.Name, err)
	}

	// Store plugin
	plugin := &Plugin{
		Name:    config.Name,
		Path:    path,
		State:   L,
		Config:  config,
		Enabled: true,
	}

	m.plugins[config.Name] = plugin

	// Call setup function if it exists
	if fn := L.GetGlobal("setup"); fn != lua.LNil {
		if setupFn, ok := fn.(*lua.LFunction); ok {
			if err := L.CallByParam(lua.P{
				Fn:      setupFn,
				NRet:    0,
				Protect: true,
			}); err != nil {
				delete(m.plugins, config.Name)
				m.luaPool.Put(L)
				return fmt.Errorf("plugin setup failed: %w", err)
			}
		}
	}

	return nil
}

// LoadAllPlugins loads all plugins from the plugin directory.
func (m *Manager) LoadAllPlugins() error {
	if m.loaded {
		return nil
	}

	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No plugin directory yet
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(m.pluginDir, entry.Name())
		if err := m.LoadPlugin(pluginPath); err != nil {
			// Log error but continue loading other plugins
			fmt.Fprintf(os.Stderr, "Failed to load plugin %s: %v\n", entry.Name(), err)
		}
	}

	m.loaded = true
	return nil
}

// UnloadPlugin unloads a plugin.
func (m *Manager) UnloadPlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Call teardown function if exists
	if fn := plugin.State.GetGlobal("teardown"); fn != lua.LNil {
		if err := plugin.State.CallByParam(lua.P{
			Fn:      fn.(*lua.LFunction),
			NRet:    0,
			Protect: true,
		}); err != nil {
			return fmt.Errorf("teardown plugin %s: %w", name, err)
		}
	}

	// Return Lua state to pool
	m.luaPool.Put(plugin.State)

	delete(m.plugins, name)

	return nil
}

// CallPlugin calls a function in a plugin.
func (m *Manager) CallPlugin(pluginName, funcName string, args ...lua.LValue) error {
	m.mu.RLock()
	plugin, ok := m.plugins[pluginName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	fn := plugin.State.GetGlobal(funcName)
	if fn == lua.LNil {
		return fmt.Errorf("function %s not found in plugin %s", funcName, pluginName)
	}

	if err := plugin.State.CallByParam(lua.P{
		Fn:      fn.(*lua.LFunction),
		NRet:    0,
		Protect: true,
	}, args...); err != nil {
		return err
	}

	return nil
}

// GetPlugin returns a plugin by name.
func (m *Manager) GetPlugin(name string) (*Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, ok := m.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin, nil
}

// ListPlugins returns all loaded plugins.
func (m *Manager) ListPlugins() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}

	return plugins
}

// Shutdown unloads all plugins.
func (m *Manager) Shutdown() {
	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		_ = m.UnloadPlugin(name)
	}
}

// HandleKey dispatches a key sequence to loaded plugins.
// It returns handled=true when the key was consumed, pending=true when the
// sequence matches a binding prefix and more input is required.
func (m *Manager) HandleKey(mode, keys string) (handled bool, pending bool, err error) {
	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	slices.Sort(names)
	plugins := make([]*Plugin, 0, len(names))
	for _, name := range names {
		plugins = append(plugins, m.plugins[name])
	}
	m.mu.RUnlock()

	pending = false
	for _, plugin := range plugins {
		binding, exact, prefix := matchKeybinding(plugin.State, mode, keys)
		if prefix {
			pending = true
		}
		if !exact {
			continue
		}
		if err := executePluginAction(plugin.State, binding.action); err != nil {
			return true, false, fmt.Errorf("plugin %s key %q: %w", plugin.Name, keys, err)
		}
		return true, false, nil
	}
	return pending, pending, nil
}

// TriggerEvent dispatches an autocmd event to all loaded plugins.
func (m *Manager) TriggerEvent(event string, ctx EventContext) error {
	if event == "" {
		return nil
	}

	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	slices.Sort(names)
	plugins := make([]*Plugin, 0, len(names))
	for _, name := range names {
		plugins = append(plugins, m.plugins[name])
	}
	m.mu.RUnlock()

	ctx.Event = event
	var firstErr error
	for _, plugin := range plugins {
		if err := triggerAutocommandsForState(plugin.State, ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("plugin %s event %s: %w", plugin.Name, event, err)
		}
	}
	return firstErr
}

func executePluginAction(L *lua.LState, action lua.LValue) error {
	switch value := action.(type) {
	case *lua.LFunction:
		return L.CallByParam(lua.P{
			Fn:      value,
			NRet:    0,
			Protect: true,
		})
	case lua.LString:
		return executeEditorCommand(L, string(value))
	default:
		return fmt.Errorf("unsupported action type %T", action)
	}
}

// loadPluginConfig reads plugin configuration from TOML file.
func loadPluginConfig(path string) (PluginConfig, error) {
	config := PluginConfig{
		Main: "init.lua", // Default entry point
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return config, err
	}
	if config.Main == "" {
		config.Main = "init.lua"
	}

	return config, nil
}
