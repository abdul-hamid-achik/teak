package plugin

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"teak/internal/config"

	"github.com/BurntSushi/toml"
	lua "github.com/yuin/gopher-lua"
)

// Manager handles Lua plugin lifecycle and state management.
type Manager struct {
	mu sync.RWMutex
	// luaMu serializes every LState access. Gopher-Lua states are explicitly
	// not safe for concurrent use, and a timed-out state must be closed before
	// another dispatch can observe it.
	luaMu       sync.Mutex
	loadMu      sync.Mutex
	plugins     map[string]*Plugin
	pluginDir   string
	luaStates   *luaStateFactory
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
	Name         string `toml:"name"`
	Version      string `toml:"version"`
	Description  string `toml:"description"`
	Author       string `toml:"author"`
	Main         string `toml:"main"`
	APIVersion   int    `toml:"api_version"`
	EventVersion int    `toml:"event_version"`
}

// NewManager creates a new plugin manager.
func NewManager(pluginDir string) (*Manager, error) {
	m := &Manager{
		plugins:     make(map[string]*Plugin),
		pluginDir:   pluginDir,
		luaStates:   newLuaStateFactory(),
		apiRegistry: NewAPIRegistry(),
	}

	// Register built-in APIs
	m.registerAPIs()

	return m, nil
}

// DefaultDir returns the default plugin directory.
func DefaultDir() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), "plugins")
}

// registerAPIs registers all built-in Lua APIs.
func (m *Manager) registerAPIs() {
	m.apiRegistry.Register("buffer", registerBufferAPI)
	m.apiRegistry.Register("editor", registerEditorAPI)
	m.apiRegistry.Register("keymap", registerKeymapAPI)
	m.apiRegistry.Register("autocmd", registerAutocmdAPI)
	m.apiRegistry.Register("ui", registerUIAPI)
	m.apiRegistry.Register("teak", registerTeakAPI(m.apiRegistry))
}

// LoadPlugin loads a plugin from disk.
func (m *Manager) LoadPlugin(path string) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	root, err := os.OpenRoot(path)
	if err != nil {
		return fmt.Errorf("open plugin root: %w", err)
	}
	defer func() { _ = root.Close() }()

	manifest, err := readPluginRootFile(root, "plugin.toml", maxPluginManifestBytes)
	if err != nil {
		return fmt.Errorf("failed to read plugin config: %w", err)
	}
	config, err := decodePluginConfig(manifest)
	if err != nil {
		return fmt.Errorf("failed to load plugin config: %w", err)
	}

	// Check if already loaded
	if _, exists := m.plugins[config.Name]; exists {
		return fmt.Errorf("plugin %s already loaded", config.Name)
	}
	if len(m.plugins) >= maxLoadedPlugins {
		return fmt.Errorf("%w: at most %d plugins may be loaded", ErrPluginResourceLimit, maxLoadedPlugins)
	}

	source, err := readPluginRootFile(root, config.Main, maxPluginSourceBytes)
	if err != nil {
		return fmt.Errorf("failed to read plugin %s main file: %w", config.Name, err)
	}

	// Create new Lua state
	L := m.luaStates.Get()

	// Register APIs in this state
	m.apiRegistry.RegisterInState(L)

	// Set plugin context
	L.SetGlobal("plugin_name", lua.LString(config.Name))
	L.SetGlobal("plugin_version", lua.LString(config.Version))

	// Load main plugin file
	mainFile := filepath.Join(path, config.Main)
	if err := runLuaWithPersistentStateBudget(L, pluginLoadBudget, "plugin load", func() error {
		fn, err := L.Load(bytes.NewReader(source), mainFile)
		if err != nil {
			return err
		}
		L.Push(fn)
		return L.PCall(0, lua.MultRet, nil)
	}); err != nil {
		m.luaStates.Put(L)
		return fmt.Errorf("failed to load plugin %s: %w", config.Name, err)
	}

	// Call setup function if it exists
	if fn := L.GetGlobal("setup"); fn != lua.LNil {
		setupFn, ok := fn.(*lua.LFunction)
		if !ok {
			m.luaStates.Put(L)
			return fmt.Errorf("plugin %s setup must be a function, got %s", config.Name, fn.Type())
		}
		if err := runLuaWithPersistentStateBudget(L, pluginLoadBudget, "plugin setup", func() error {
			return L.CallByParam(lua.P{
				Fn:      setupFn,
				NRet:    0,
				Protect: true,
			})
		}); err != nil {
			m.luaStates.Put(L)
			return fmt.Errorf("plugin setup failed: %w", err)
		}
	}

	m.plugins[config.Name] = &Plugin{
		Name:    config.Name,
		Path:    path,
		State:   L,
		Config:  config,
		Enabled: true,
	}

	return nil
}

// LoadAllPlugins loads all plugins from the plugin directory.
func (m *Manager) LoadAllPlugins() error {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	m.mu.RLock()
	loaded := m.loaded
	m.mu.RUnlock()
	if loaded {
		return nil
	}

	dir, err := os.Open(m.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Treat the empty directory as a completed, idempotent scan. Plugins
			// are loaded at startup; creating the directory later requires an
			// explicit reload rather than racing subsequent callers.
			m.mu.Lock()
			m.loaded = true
			m.mu.Unlock()
			return nil
		}
		return err
	}
	entries, err := dir.ReadDir(maxPluginDirectoryEntries + 1)
	_ = dir.Close()
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > maxPluginDirectoryEntries {
		return fmt.Errorf("%w: plugin directory contains more than %d entries", ErrPluginResourceLimit, maxPluginDirectoryEntries)
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

	m.mu.Lock()
	m.loaded = true
	m.mu.Unlock()
	return nil
}

// UnloadPlugin unloads a plugin.
func (m *Manager) UnloadPlugin(name string) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.mu.Lock()

	plugin, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", name)
	}

	// Teardown failures are reported but cannot prevent resource cleanup.
	var teardownErr error
	if fn := plugin.State.GetGlobal("teardown"); fn != lua.LNil {
		teardownFn, ok := fn.(*lua.LFunction)
		if !ok {
			teardownErr = fmt.Errorf("plugin %s teardown must be a function, got %s", name, fn.Type())
		} else if err := runLuaWithBudget(plugin.State, pluginTeardownBudget, "plugin teardown", func() error {
			return plugin.State.CallByParam(lua.P{
				Fn:      teardownFn,
				NRet:    0,
				Protect: true,
			})
		}); err != nil {
			teardownErr = fmt.Errorf("teardown plugin %s: %w", name, err)
		}
	}

	m.luaStates.Put(plugin.State)
	delete(m.plugins, name)
	m.mu.Unlock()
	discardUIConfirmCallbacks(plugin.State)
	return teardownErr
}

// CallPlugin calls a function in a plugin.
func (m *Manager) CallPlugin(pluginName, funcName string, args ...lua.LValue) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
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
	callFn, ok := fn.(*lua.LFunction)
	if !ok {
		return fmt.Errorf("function %s in plugin %s must be a function, got %s", funcName, pluginName, fn.Type())
	}

	if err := runLuaWithPersistentStateBudget(plugin.State, pluginActionBudget, "plugin function", func() error {
		return plugin.State.CallByParam(lua.P{
			Fn:      callFn,
			NRet:    0,
			Protect: true,
		}, args...)
	}); err != nil {
		if isPluginRuntimeBudgetExceeded(err) {
			m.quarantinePlugin(plugin.Name, plugin, err)
		}
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

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	slices.Sort(names)
	plugins := make([]*Plugin, 0, len(names))
	for _, name := range names {
		plugins = append(plugins, m.plugins[name])
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
	m.luaStates.Close()
}

// HandleKey dispatches a key sequence to loaded plugins.
// It returns handled=true when the key was consumed, pending=true when the
// sequence matches a binding prefix and more input is required.
func (m *Manager) HandleKey(mode, keys string) (handled bool, pending bool, err error) {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	return m.handleKeyLocked(mode, keys)
}

// MatchKey performs the non-executing half of key dispatch. It only consults
// Go-owned keybinding metadata, so callers can decide whether to consume a
// key without running Lua on the Bubble Tea update goroutine.
func (m *Manager) MatchKey(mode, keys string) (exact bool, prefix bool) {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, plugin := range m.plugins {
		if !plugin.Enabled {
			continue
		}
		_, candidateExact, candidatePrefix := matchKeybinding(plugin.State, mode, keys)
		if candidateExact {
			exact = true
		}
		if candidatePrefix {
			prefix = true
		}
	}
	return exact, prefix
}

func (m *Manager) handleKeyLocked(mode, keys string) (handled bool, pending bool, err error) {
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
		if !plugin.Enabled {
			continue
		}
		binding, exact, prefix := matchKeybinding(plugin.State, mode, keys)
		if prefix {
			pending = true
		}
		if !exact {
			continue
		}
		if err := executePluginAction(plugin.State, binding.action); err != nil {
			if isPluginRuntimeBudgetExceeded(err) {
				m.quarantinePlugin(plugin.Name, plugin, err)
			}
			return true, false, fmt.Errorf("plugin %s key %q: %w", plugin.Name, keys, err)
		}
		return true, false, nil
	}
	return pending, pending, nil
}

// TriggerEvent dispatches an autocmd event to all loaded plugins.
func (m *Manager) TriggerEvent(event string, ctx EventContext) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	return m.triggerEventLocked(event, ctx)
}

func (m *Manager) triggerEventLocked(event string, ctx EventContext) error {
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
		if !plugin.Enabled {
			continue
		}
		if err := triggerAutocommandsForState(plugin.State, ctx); err != nil {
			if isPluginRuntimeBudgetExceeded(err) {
				m.quarantinePlugin(plugin.Name, plugin, err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("plugin %s event %s: %w", plugin.Name, event, err)
			}
		}
	}
	return firstErr
}

func executePluginAction(L *lua.LState, action lua.LValue) error {
	switch value := action.(type) {
	case *lua.LFunction:
		return runLuaWithPersistentStateBudget(L, pluginActionBudget, "key action", func() error {
			return L.CallByParam(lua.P{
				Fn:      value,
				NRet:    0,
				Protect: true,
			})
		})
	case lua.LString:
		return runLuaWithPersistentStateBudget(L, pluginActionBudget, "key command", func() error {
			return executeEditorCommand(L, string(value))
		})
	default:
		return fmt.Errorf("unsupported action type %T", action)
	}
}

// quarantinePlugin removes and closes a state that exceeded a runtime budget.
// The caller must hold luaMu and must have returned from the Lua call first.
func (m *Manager) quarantinePlugin(name string, expected *Plugin, cause error) {
	m.mu.Lock()

	plugin, ok := m.plugins[name]
	if !ok || plugin != expected {
		m.mu.Unlock()
		return
	}
	plugin.Enabled = false
	delete(m.plugins, name)
	m.luaStates.Put(plugin.State)
	m.mu.Unlock()
	discardUIConfirmCallbacks(plugin.State)
}

// loadPluginConfig reads plugin configuration from TOML file.
func loadPluginConfig(path string) (PluginConfig, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return PluginConfig{Main: "init.lua"}, err
	}
	defer func() { _ = root.Close() }()
	data, err := readPluginRootFile(root, filepath.Base(path), maxPluginManifestBytes)
	if err != nil {
		return PluginConfig{Main: "init.lua"}, err
	}
	return decodePluginConfig(data)
}

func decodePluginConfig(data []byte) (PluginConfig, error) {
	config := PluginConfig{
		Main: "init.lua", // Default entry point
	}

	if err := toml.Unmarshal(data, &config); err != nil {
		return config, err
	}
	if config.Main == "" {
		config.Main = "init.lua"
	}
	if err := validatePluginConfig(config); err != nil {
		return config, err
	}

	return config, nil
}

func validatePluginConfig(config PluginConfig) error {
	if strings.TrimSpace(config.Name) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if config.APIVersion < 0 || config.APIVersion > PluginAPIVersion {
		return fmt.Errorf("plugin API version %d is unsupported (current %d)", config.APIVersion, PluginAPIVersion)
	}
	if config.EventVersion < 0 || config.EventVersion > PluginEventAPIVersion {
		return fmt.Errorf("plugin event version %d is unsupported (current %d)", config.EventVersion, PluginEventAPIVersion)
	}
	return nil
}
