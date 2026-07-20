package plugin

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// ErrPluginPersistentStateBudgetExceeded identifies a plugin whose
// persistent, reachable Lua state exceeded one of the bounded accounting
// limits. It is intentionally distinct from ErrPluginResourceLimit: source
// and registration caps stop individual inputs, while this guard measures the
// graph kept alive by a loaded plugin across callbacks.
var ErrPluginPersistentStateBudgetExceeded = errors.New("plugin persistent state budget exceeded")

const (
	// These limits cover Lua values reachable from globals, the Lua registry,
	// and Teak's Go-owned callback registries. They are accounting limits, not
	// a process heap ceiling; a hard ceiling for untrusted Lua requires process
	// isolation because Gopher-Lua's SetMx terminates the host process.
	maxPluginPersistentStateBytes   int64 = 4 << 20
	maxPluginPersistentStateNodes         = 16 * 1024
	maxPluginPersistentStateEntries       = 16 * 1024

	// LTable does not expose its backing-array sizes. The inspector reads only
	// their lengths/capacities through reflection (never unsafe) before it
	// iterates. This makes the otherwise public table traversal API bounded
	// even for a sparse table with a very large backing array.
	maxPluginPersistentTableSlots = 32 * 1024
	maxPluginPersistentWorkItems  = 64 * 1024
)

// PersistentStateStats is the deterministic, conservative accounting result
// for one complete reachability scan. Bytes is an estimate of persistent
// value payload and metadata, rather than the Go runtime heap allocation.
type PersistentStateStats struct {
	Nodes      int
	Entries    int
	TableSlots int
	Bytes      int64
}

// PersistentStateBudgetError describes the first reached accounting limit.
// Errors.Is matches ErrPluginPersistentStateBudgetExceeded and callers can
// use errors.As to inspect the failed dimension.
type PersistentStateBudgetError struct {
	Limit string
	Max   int64
	Got   int64
	Stats PersistentStateStats
}

func (e *PersistentStateBudgetError) Error() string {
	return fmt.Sprintf("%v: %s is %d (max %d)", ErrPluginPersistentStateBudgetExceeded, e.Limit, e.Got, e.Max)
}

func (e *PersistentStateBudgetError) Unwrap() error {
	return ErrPluginPersistentStateBudgetExceeded
}

type persistentStateInspector struct {
	stats PersistentStateStats
	err   error

	seenTables    map[*lua.LTable]struct{}
	seenFunctions map[*lua.LFunction]struct{}
	seenUserData  map[*lua.LUserData]struct{}
	seenThreads   map[*lua.LState]struct{}
	seenProtos    map[*lua.FunctionProto]struct{}
	work          []lua.LValue
	protoWork     []*lua.FunctionProto
}

var persistentStateInspectorPool = sync.Pool{
	New: func() any {
		return &persistentStateInspector{
			seenTables:    make(map[*lua.LTable]struct{}),
			seenFunctions: make(map[*lua.LFunction]struct{}),
			seenUserData:  make(map[*lua.LUserData]struct{}),
			seenThreads:   make(map[*lua.LState]struct{}),
			seenProtos:    make(map[*lua.FunctionProto]struct{}),
			work:          make([]lua.LValue, 0, 128),
			protoWork:     make([]*lua.FunctionProto, 0, 32),
		}
	},
}

// checkPluginPersistentStateBudget walks all values that a plugin can retain
// between invocations. The walker is iterative, cycle-safe, and bounded by
// independent node, entry, table-slot, byte, and work-item limits.
//
// Gopher-Lua v1.1.1 deliberately has no safe in-process heap cap: SetMx starts
// a goroutine which calls os.Exit(3) when the whole host heap crosses its
// threshold. This reachability budget therefore limits persistent growth; it
// does not claim to sandbox transient allocations or arbitrary Go userdata.
func checkPluginPersistentStateBudget(L *lua.LState) (PersistentStateStats, error) {
	if L == nil || L.G == nil {
		return PersistentStateStats{}, fmt.Errorf("cannot inspect nil Lua state")
	}

	inspector := persistentStateInspectorPool.Get().(*persistentStateInspector)
	inspector.reset()
	defer func() {
		// Do not let sync.Pool retain a closed plugin's Lua graph after it has
		// been quarantined or unloaded.
		inspector.releaseReferences()
		persistentStateInspectorPool.Put(inspector)
	}()

	inspector.enqueuePluginPersistentStateRoots(L)
	for inspector.err == nil && (len(inspector.work) > 0 || len(inspector.protoWork) > 0) {
		if len(inspector.work) > 0 {
			last := len(inspector.work) - 1
			value := inspector.work[last]
			inspector.work = inspector.work[:last]
			inspector.inspectValue(value)
			continue
		}
		last := len(inspector.protoWork) - 1
		proto := inspector.protoWork[last]
		inspector.protoWork = inspector.protoWork[:last]
		inspector.inspectProto(proto)
	}
	if inspector.err != nil {
		return inspector.stats, inspector.err
	}
	return inspector.stats, nil
}

func (i *persistentStateInspector) reset() {
	i.stats = PersistentStateStats{}
	i.err = nil
	i.releaseReferences()
}

func (i *persistentStateInspector) releaseReferences() {
	clear(i.seenTables)
	clear(i.seenFunctions)
	clear(i.seenUserData)
	clear(i.seenThreads)
	clear(i.seenProtos)
	clear(i.work)
	i.work = i.work[:0]
	clear(i.protoWork)
	i.protoWork = i.protoWork[:0]
}

func (i *persistentStateInspector) inspectValue(value lua.LValue) {
	if i.err != nil || value == nil || value == lua.LNil {
		return
	}
	switch value := value.(type) {
	case lua.LString:
		i.addBytes(int64(len(value)) + 16)
	case lua.LNumber:
		i.addBytes(8)
	case lua.LBool:
		i.addBytes(1)
	case *lua.LTable:
		if _, seen := i.seenTables[value]; seen {
			return
		}
		i.seenTables[value] = struct{}{}
		i.addNode(96)
		if i.err != nil {
			return
		}
		slots, err := luaTableSlots(value)
		if err != nil {
			i.fail("table layout", 0, 0)
			return
		}
		i.addTableSlots(slots)
		if i.err != nil {
			return
		}
		i.enqueueValue(value.Metatable)
		key := lua.LValue(lua.LNil)
		for i.err == nil {
			var item lua.LValue
			key, item = value.Next(key)
			if key == lua.LNil {
				break
			}
			i.addEntry()
			i.enqueueValue(key)
			i.enqueueValue(item)
		}
	case *lua.LFunction:
		if _, seen := i.seenFunctions[value]; seen {
			return
		}
		i.seenFunctions[value] = struct{}{}
		i.addNode(80 + int64(len(value.Upvalues))*16)
		if i.err != nil {
			return
		}
		i.enqueueValue(value.Env)
		for _, upvalue := range value.Upvalues {
			if upvalue != nil {
				i.enqueueValue(upvalue.Value())
			}
		}
		i.enqueueProto(value.Proto)
	case *lua.LUserData:
		if _, seen := i.seenUserData[value]; seen {
			return
		}
		i.seenUserData[value] = struct{}{}
		i.addNode(64)
		if i.err != nil {
			return
		}
		i.enqueueValue(value.Env)
		i.enqueueValue(value.Metatable)
		switch payload := value.Value.(type) {
		case string:
			i.addBytes(int64(len(payload)))
		case []byte:
			i.addBytes(int64(len(payload)))
		}
	case *lua.LState:
		if _, seen := i.seenThreads[value]; seen {
			return
		}
		i.seenThreads[value] = struct{}{}
		i.addNode(64)
	default:
		// LChannel and future LValue implementations do not expose a safe,
		// side-effect-free graph traversal. Count a conservative header and
		// never attempt reflection or channel reads.
		i.addBytes(32)
	}
}

func (i *persistentStateInspector) inspectProto(proto *lua.FunctionProto) {
	if i.err != nil || proto == nil {
		return
	}
	if _, seen := i.seenProtos[proto]; seen {
		return
	}
	i.seenProtos[proto] = struct{}{}
	i.addNode(128 + int64(len(proto.Code))*4 + int64(len(proto.DbgSourcePositions))*8)
	if i.err != nil {
		return
	}
	i.addBytes(int64(len(proto.SourceName)))
	for _, local := range proto.DbgLocals {
		if local != nil {
			i.addBytes(int64(len(local.Name)) + 24)
		}
	}
	for _, upvalue := range proto.DbgUpvalues {
		i.addBytes(int64(len(upvalue)) + 16)
	}
	for _, constant := range proto.Constants {
		i.enqueueValue(constant)
	}
	for _, child := range proto.FunctionPrototypes {
		i.enqueueProto(child)
	}
}

func (i *persistentStateInspector) enqueueValue(value lua.LValue) {
	if i.err != nil || value == nil || value == lua.LNil {
		return
	}
	if len(i.work)+len(i.protoWork) >= maxPluginPersistentWorkItems {
		i.fail("work items", maxPluginPersistentWorkItems, len(i.work)+len(i.protoWork)+1)
		return
	}
	i.work = append(i.work, value)
}

func (i *persistentStateInspector) enqueueProto(proto *lua.FunctionProto) {
	if i.err != nil || proto == nil {
		return
	}
	if len(i.work)+len(i.protoWork) >= maxPluginPersistentWorkItems {
		i.fail("work items", maxPluginPersistentWorkItems, len(i.work)+len(i.protoWork)+1)
		return
	}
	i.protoWork = append(i.protoWork, proto)
}

func (i *persistentStateInspector) addNode(bytes int64) {
	if i.err != nil {
		return
	}
	i.stats.Nodes++
	if i.stats.Nodes > maxPluginPersistentStateNodes {
		i.fail("nodes", maxPluginPersistentStateNodes, i.stats.Nodes)
		return
	}
	i.addBytes(bytes)
}

func (i *persistentStateInspector) addEntry() {
	if i.err != nil {
		return
	}
	i.stats.Entries++
	if i.stats.Entries > maxPluginPersistentStateEntries {
		i.fail("entries", maxPluginPersistentStateEntries, i.stats.Entries)
	}
}

func (i *persistentStateInspector) addTableSlots(slots int) {
	if i.err != nil || slots <= 0 {
		return
	}
	if slots > maxPluginPersistentTableSlots-i.stats.TableSlots {
		i.fail("table slots", maxPluginPersistentTableSlots, i.stats.TableSlots+slots)
		return
	}
	i.stats.TableSlots += slots
	// One interface slot plus conservative allocator metadata for each table
	// backing slot. Exact Go heap accounting is intentionally out of scope.
	i.addBytes(int64(slots) * 24)
}

func (i *persistentStateInspector) addBytes(bytes int64) {
	if i.err != nil || bytes <= 0 {
		return
	}
	if bytes > maxPluginPersistentStateBytes-i.stats.Bytes {
		i.fail64("bytes", maxPluginPersistentStateBytes, i.stats.Bytes+bytes)
		return
	}
	i.stats.Bytes += bytes
}

func (i *persistentStateInspector) fail(limit string, max, got int) {
	i.fail64(limit, int64(max), int64(got))
}

func (i *persistentStateInspector) fail64(limit string, max, got int64) {
	if i.err != nil {
		return
	}
	i.err = &PersistentStateBudgetError{Limit: limit, Max: max, Got: got, Stats: i.stats}
}

func (i *persistentStateInspector) addHostString(value string) {
	i.addBytes(int64(len(value)) + 16)
}

// enqueuePluginPersistentStateRoots includes the two Lua-owned roots plus
// callbacks retained only in Go maps. Registries are snapshotted under their
// own locks, which keeps the walk race-free without holding a registry lock
// while Lua tables are traversed.
func (i *persistentStateInspector) enqueuePluginPersistentStateRoots(L *lua.LState) {
	i.enqueueValue(L.G.Global)
	i.enqueueValue(L.G.Registry)
	i.enqueueValue(L.Env)

	pluginKeymaps.mu.RLock()
	if modes := pluginKeymaps.states[L]; modes != nil {
		modeNames := sortedMapKeys(modes)
		for _, mode := range modeNames {
			i.addHostString(mode)
			bindings := modes[mode]
			keys := sortedMapKeys(bindings)
			for _, key := range keys {
				binding := bindings[key]
				i.addHostString(key)
				i.addHostString(binding.description)
				i.enqueueValue(binding.action)
			}
		}
	}
	pluginKeymaps.mu.RUnlock()

	pluginCommands.mu.RLock()
	if commands := pluginCommands.states[L]; commands != nil {
		for _, name := range sortedMapKeys(commands) {
			i.addHostString(name)
			i.enqueueValue(commands[name])
		}
	}
	pluginCommands.mu.RUnlock()

	pluginAutocommands.mu.RLock()
	if events := pluginAutocommands.states[L]; events != nil {
		for _, event := range sortedMapKeys(events) {
			for _, command := range events[event] {
				i.addHostString(command.Event)
				i.addHostString(command.Pattern)
				i.addHostString(command.Group)
				i.enqueueValue(command.Callback)
			}
		}
	}
	pluginAutocommands.mu.RUnlock()
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// luaTableSlots reads only the stable structural collection sizes from
// Gopher-Lua v1.1.1. We deliberately do not use unsafe to obtain backing
// values; if that layout ever changes, fail closed rather than risk an
// unbounded public table traversal.
func luaTableSlots(table *lua.LTable) (int, error) {
	if table == nil {
		return 0, nil
	}
	value := reflect.ValueOf(table)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return 0, fmt.Errorf("invalid LTable")
	}
	value = value.Elem()
	if value.Kind() != reflect.Struct {
		return 0, fmt.Errorf("invalid LTable representation")
	}

	array, err := reflectedTableSize(value.FieldByName("array"), true)
	if err != nil {
		return 0, err
	}
	dict, err := reflectedTableSize(value.FieldByName("dict"), false)
	if err != nil {
		return 0, err
	}
	strdict, err := reflectedTableSize(value.FieldByName("strdict"), false)
	if err != nil {
		return 0, err
	}
	keys, err := reflectedTableSize(value.FieldByName("keys"), true)
	if err != nil {
		return 0, err
	}

	total := array + dict + strdict + keys
	if total < 0 { // Defensive overflow check on unusual architectures.
		return 0, fmt.Errorf("LTable size overflow")
	}
	return total, nil
}

func reflectedTableSize(value reflect.Value, capacity bool) (int, error) {
	if !value.IsValid() {
		return 0, fmt.Errorf("missing LTable field")
	}
	if capacity {
		if value.Kind() != reflect.Slice {
			return 0, fmt.Errorf("unexpected LTable slice field")
		}
		return value.Cap(), nil
	}
	if value.Kind() != reflect.Map {
		return 0, fmt.Errorf("unexpected LTable map field")
	}
	return value.Len(), nil
}
