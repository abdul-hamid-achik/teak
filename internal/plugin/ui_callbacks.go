package plugin

import (
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/yuin/gopher-lua"
)

type uiCallbackRecord struct {
	state *lua.LState
	fn    *lua.LFunction
}

var uiCallbacks = struct {
	mu      sync.Mutex
	nextID  atomic.Uint64
	records map[uint64]uiCallbackRecord
}{
	records: make(map[uint64]uiCallbackRecord),
}

func registerUIConfirmCallback(state *lua.LState, fn *lua.LFunction) (uint64, error) {
	return registerUICallback(state, fn)
}

func registerUIInputCallback(state *lua.LState, fn *lua.LFunction) (uint64, error) {
	return registerUICallback(state, fn)
}

func registerUISelectCallback(state *lua.LState, fn *lua.LFunction) (uint64, error) {
	return registerUICallback(state, fn)
}

func registerUICallback(state *lua.LState, fn *lua.LFunction) (uint64, error) {
	uiCallbacks.mu.Lock()
	defer uiCallbacks.mu.Unlock()
	if len(uiCallbacks.records) >= maxPluginUIConfirmCallbacks {
		return 0, fmt.Errorf("%w: at most %d pending UI confirmations", ErrPluginResourceLimit, maxPluginUIConfirmCallbacks)
	}
	id := uiCallbacks.nextID.Add(1)
	if id == 0 {
		id = uiCallbacks.nextID.Add(1)
	}
	uiCallbacks.records[id] = uiCallbackRecord{state: state, fn: fn}
	return id, nil
}

func discardUIConfirmCallback(id uint64) {
	discardUICallback(id)
}

func discardUICallback(id uint64) {
	uiCallbacks.mu.Lock()
	delete(uiCallbacks.records, id)
	uiCallbacks.mu.Unlock()
}

// CancelUICallback releases a callback whose UI request was removed before a
// result could be delivered. It is safe to call after normal dispatch.
func CancelUICallback(id uint64) {
	discardUICallback(id)
}

func discardUIConfirmCallbacks(state *lua.LState) {
	discardUICallbacks(state)
}

func discardUICallbacks(state *lua.LState) {
	uiCallbacks.mu.Lock()
	defer uiCallbacks.mu.Unlock()
	for id, record := range uiCallbacks.records {
		if record.state == state {
			delete(uiCallbacks.records, id)
		}
	}
}

func (m *Manager) takeUICallback(callbackID uint64) (uiCallbackRecord, error) {
	uiCallbacks.mu.Lock()
	defer uiCallbacks.mu.Unlock()
	record, ok := uiCallbacks.records[callbackID]
	if !ok {
		return uiCallbackRecord{}, fmt.Errorf("UI callback %d is unavailable", callbackID)
	}
	m.mu.RLock()
	owned := false
	for _, candidate := range m.plugins {
		if candidate.State == record.state {
			owned = true
			break
		}
	}
	m.mu.RUnlock()
	if !owned {
		delete(uiCallbacks.records, callbackID)
		return uiCallbackRecord{}, fmt.Errorf("UI callback %d is unavailable", callbackID)
	}
	delete(uiCallbacks.records, callbackID)
	return record, nil
}

// dispatchUICallback resumes one callback on the manager's serialized Lua
// state. It is called from a tea.Cmd, never from Model.Update.
func (m *Manager) dispatchUICallback(runtime Runtime, callbackID uint64, args ...lua.LValue) error {
	m.luaMu.Lock()
	defer m.luaMu.Unlock()
	record, err := m.takeUICallback(callbackID)
	if err != nil {
		return err
	}

	m.setRuntimeLocked(runtime)
	defer m.clearRuntimeLocked()
	err = runLuaWithPersistentStateBudget(record.state, pluginActionBudget, "UI callback", func() error {
		return record.state.CallByParam(lua.P{
			Fn:      record.fn,
			NRet:    0,
			Protect: true,
		}, args...)
	})
	if err != nil && isPluginRuntimeBudgetExceeded(err) {
		m.quarantinePluginForState(record.state, err)
	}
	return err
}

// DispatchUIConfirm resumes one confirmation callback with its selected
// option. It is called from a tea.Cmd, never from Model.Update.
func (m *Manager) DispatchUIConfirm(runtime Runtime, callbackID uint64, result UIConfirmResult) error {
	return m.dispatchUICallback(runtime, callbackID,
		lua.LString(result.Option), lua.LNumber(result.Index), lua.LBool(result.Accepted))
}

// DispatchUIInput resumes one input callback with its entered value.
func (m *Manager) DispatchUIInput(runtime Runtime, callbackID uint64, result UIInputResult) error {
	return m.dispatchUICallback(runtime, callbackID, lua.LString(result.Value), lua.LBool(result.Accepted))
}

// DispatchUISelect resumes one selector callback with its selected option.
func (m *Manager) DispatchUISelect(runtime Runtime, callbackID uint64, result UISelectResult) error {
	return m.dispatchUICallback(runtime, callbackID,
		lua.LString(result.Option), lua.LNumber(result.Index), lua.LBool(result.Accepted))
}

func (m *Manager) quarantinePluginForState(state *lua.LState, cause error) {
	m.mu.Lock()
	var removed *Plugin
	for name, candidate := range m.plugins {
		if candidate.State != state {
			continue
		}
		candidate.Enabled = false
		delete(m.plugins, name)
		removed = candidate
		break
	}
	m.mu.Unlock()
	if removed != nil {
		discardUICallbacks(removed.State)
		m.luaStates.Put(removed.State)
	}
	_ = cause
}
