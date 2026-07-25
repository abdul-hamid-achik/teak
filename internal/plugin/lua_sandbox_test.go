package plugin

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// evalLua runs source in a plugin state and returns the error, if any.
func evalLua(t *testing.T, source string) error {
	t.Helper()
	factory := newLuaStateFactory()
	L := factory.Get()
	defer factory.Put(L)
	return L.DoString(source)
}

// Plugins ran with no standard library at all, so ordinary code failed: the
// quickstart in README.md calls string.format and errored on its first run.
func TestPluginsCanUseTheDocumentedStandardLibrary(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"string.format", `assert(string.format("%s|%d", "a", 1) == "a|1")`},
		{"string.rep and sub", `assert(string.sub(string.rep("ab", 2), 1, 3) == "aba")`},
		{"tostring", `assert(tostring(12) == "12")`},
		{"tonumber", `assert(tonumber("12") == 12)`},
		{"type", `assert(type({}) == "table")`},
		{"pairs", `local n = 0 for _ in pairs({a=1,b=2}) do n = n + 1 end assert(n == 2)`},
		{"ipairs", `local n = 0 for _ in ipairs({1,2,3}) do n = n + 1 end assert(n == 3)`},
		{"table.insert and concat", `local t = {} table.insert(t, "x") assert(table.concat(t) == "x")`},
		{"table.sort", `local t = {3,1,2} table.sort(t) assert(t[1] == 1)`},
		{"math.floor", `assert(math.floor(1.7) == 1)`},
		{"math.max", `assert(math.max(1, 5) == 5)`},
		{"pcall catches an error", `local ok = pcall(function() error("boom") end) assert(ok == false)`},
		{"setmetatable", `local t = setmetatable({}, {__index = function() return 7 end}) assert(t.anything == 7)`},
		{"select", `assert(select("#", 1, 2, 3) == 3)`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := evalLua(t, tc.source); err != nil {
				t.Errorf("%s is unavailable to plugins: %v", tc.name, err)
			}
		})
	}
}

// The README quickstart verbatim. If this fails, the documented getting-started
// path is broken.
func TestReadmeQuickstartSnippetRuns(t *testing.T) {
	source := `
local path = "main.go"
local line, col = 1, 2
local status = string.format("%s | %d:%d", path, line, col)
assert(status == "main.go | 1:2")
`
	if err := evalLua(t, source); err != nil {
		t.Errorf("the documented plugin example fails: %v", err)
	}
}

// Opening the computation libraries must not reopen filesystem, process or
// runtime access. Each of these would let a plugin escape the sandbox that the
// resource budgets and workspace confinement depend on.
func TestPluginSandboxWithholdsUnsafeCapabilities(t *testing.T) {
	tests := []struct {
		name   string
		global string
	}{
		{"filesystem library", "io"},
		{"process library", "os"},
		{"introspection library", "debug"},
		{"module loader", "package"},
		{"coroutines", "coroutine"},
		{"run a file", "dofile"},
		{"load a file", "loadfile"},
		{"compile a string", "load"},
		{"compile a string (5.1 name)", "loadstring"},
		{"require a module", "require"},
		{"declare a module", "module"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := `assert(` + tc.global + ` == nil, "` + tc.global + ` is reachable")`
			if err := evalLua(t, source); err != nil {
				t.Errorf("%s (%s) is reachable from a plugin: %v", tc.name, tc.global, err)
			}
		})
	}
}

// A plugin must not be able to read or write the filesystem by any route the
// standard library provides.
func TestPluginCannotReachTheFilesystem(t *testing.T) {
	err := evalLua(t, `local f = io.open("/etc/passwd", "r")`)
	if err == nil {
		t.Fatal("a plugin opened a file through io.open")
	}
	if !strings.Contains(err.Error(), "non-table object") && !strings.Contains(err.Error(), "nil") {
		t.Logf("io.open failed as expected: %v", err)
	}
}

func TestSandboxedLibsAndUnsafeGlobalsAreConsistent(t *testing.T) {
	// Guard against someone adding a library here without considering what its
	// globals reintroduce.
	allowed := map[string]bool{
		lua.BaseLibName:   true,
		lua.StringLibName: true,
		lua.TabLibName:    true,
		lua.MathLibName:   true,
	}
	for _, lib := range sandboxedLibs {
		if !allowed[lib.name] {
			t.Errorf("library %q was added to the sandbox; confirm it grants no filesystem, process or runtime access and update this test", lib.name)
		}
	}
	if len(unsafeBaseGlobals) == 0 {
		t.Error("unsafeBaseGlobals is empty; OpenBase installs code-loading globals that must be removed")
	}
}
