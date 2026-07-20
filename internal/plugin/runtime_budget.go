package plugin

import (
	"context"
	"errors"
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// ErrExecutionBudgetExceeded marks Lua work that exceeded the editor's
// responsiveness budget. The manager quarantines the owning plugin after this
// error: resuming a timed-out LState would make subsequent UI work unreliable.
var ErrExecutionBudgetExceeded = errors.New("plugin execution budget exceeded")

const (
	// Loading happens off the Bubble Tea update loop, but it still needs a hard
	// limit so one malformed plugin cannot monopolize a CPU core indefinitely.
	pluginLoadBudget = time.Second

	// Interactive callbacks run while a key or editor event is being handled.
	// Keep these deliberately short; non-interactive work belongs in a tea.Cmd.
	pluginActionBudget   = 35 * time.Millisecond
	pluginAutocmdBudget  = 35 * time.Millisecond
	pluginTeardownBudget = 100 * time.Millisecond
)

// runLuaWithBudget uses Gopher-Lua's supported LState context mechanism. Its
// VM checks the context on every Lua instruction, so pure-Lua loops are
// interrupted without a goroutine that could keep running after a timeout.
// Callers must serialize access to L; LState is not goroutine-safe.
func runLuaWithBudget(L *lua.LState, budget time.Duration, operation string, fn func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	previous := L.Context()
	L.SetContext(ctx)
	defer func() {
		L.RemoveContext()
		if previous != nil {
			L.SetContext(previous)
		}
	}()

	err := fn()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if err != nil {
			return fmt.Errorf("%w during %s: %v", ErrExecutionBudgetExceeded, operation, err)
		}
		return fmt.Errorf("%w during %s", ErrExecutionBudgetExceeded, operation)
	}
	return err
}

func isExecutionBudgetExceeded(err error) bool {
	return errors.Is(err, ErrExecutionBudgetExceeded)
}

// runLuaWithPersistentStateBudget combines the instruction-time CPU budget
// with the bounded persistent-state accounting pass. The latter intentionally
// runs after every completed plugin entry point: arbitrary Lua callbacks can
// retain data through globals, closures, or Go-owned callback registries.
//
// A timed-out VM is quarantined by the caller already, so scanning it would
// only add latency without improving containment. Other Lua errors are still
// scanned because a plugin can allocate retained state and then raise an
// ordinary runtime error.
func runLuaWithPersistentStateBudget(L *lua.LState, budget time.Duration, operation string, fn func() error) error {
	runErr := runLuaWithBudget(L, budget, operation, fn)
	if isExecutionBudgetExceeded(runErr) {
		return runErr
	}
	_, stateErr := checkPluginPersistentStateBudget(L)
	if stateErr == nil {
		return runErr
	}
	if runErr == nil {
		return stateErr
	}
	return errors.Join(runErr, stateErr)
}

func isPluginRuntimeBudgetExceeded(err error) bool {
	return errors.Is(err, ErrExecutionBudgetExceeded) || errors.Is(err, ErrPluginPersistentStateBudgetExceeded)
}
