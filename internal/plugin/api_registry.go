package plugin

import (
	lua "github.com/yuin/gopher-lua"
)

// APIFunc is a function that registers API functions in a Lua state.
type APIFunc func(*lua.LState)

// APIRegistry manages API registration.
type APIRegistry struct {
	apis map[string]APIFunc
}

// NewAPIRegistry creates a new API registry.
func NewAPIRegistry() *APIRegistry {
	return &APIRegistry{
		apis: make(map[string]APIFunc),
	}
}

// Register registers an API function.
func (r *APIRegistry) Register(name string, fn APIFunc) {
	r.apis[name] = fn
}

// RegisterInState registers all APIs in a Lua state.
func (r *APIRegistry) RegisterInState(L *lua.LState) {
	for name, fn := range r.apis {
		fn(L)
		// The module should be at the top of the stack after fn(L)
		if L.GetTop() > 0 {
			mod := L.Get(-1)
			L.Pop(1)
			L.SetGlobal(name, mod)
		}
	}
}
