package plugin

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// luaStatePool manages a pool of Lua states for reuse.
type luaStatePool struct {
	mu    sync.Mutex
	pool  []*lua.LState
	count int
}

// newLuaStatePool creates a new Lua state pool.
func newLuaStatePool() *luaStatePool {
	return &luaStatePool{
		pool: make([]*lua.LState, 0, 10),
	}
}

// Get retrieves a Lua state from the pool or creates a new one.
func (p *luaStatePool) Get() *lua.LState {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pool) > 0 {
		L := p.pool[len(p.pool)-1]
		p.pool = p.pool[:len(p.pool)-1]
		return L
	}

	p.count++

	// Create new Lua state with optimized settings
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

// Put returns a Lua state to the pool.
func (p *luaStatePool) Put(L *lua.LState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if L == nil {
		return
	}

	// Clear globals to prevent cross-plugin contamination
	for _, name := range []string{
		"_G", "package", "buffer", "editor", "keymap", "autocmd", "ui",
		"plugin_name", "plugin_version",
	} {
		L.SetGlobal(name, lua.LNil)
	}

	p.pool = append(p.pool, L)
}

// Close closes all Lua states in the pool.
func (p *luaStatePool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, L := range p.pool {
		if L != nil {
			L.Close()
		}
	}
	p.pool = nil
}
