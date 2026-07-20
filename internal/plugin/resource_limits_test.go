package plugin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestLoadPluginRejectsOversizedManifestBeforeDecode(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "large-manifest")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := append([]byte("name = \"large-manifest\"\n#"), bytes.Repeat([]byte("x"), int(maxPluginManifestBytes))...)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	err = mgr.LoadPlugin(pluginDir)
	if !errors.Is(err, ErrPluginResourceLimit) {
		t.Fatalf("LoadPlugin() error = %v, want ErrPluginResourceLimit", err)
	}
}

func TestLoadPluginRejectsOversizedLuaBeforeCompile(t *testing.T) {
	root := t.TempDir()
	pluginDir := writeBudgetPlugin(t, root, "large-source", string(bytes.Repeat([]byte("-"), int(maxPluginSourceBytes+1))))

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	err = mgr.LoadPlugin(pluginDir)
	if !errors.Is(err, ErrPluginResourceLimit) {
		t.Fatalf("LoadPlugin() error = %v, want ErrPluginResourceLimit", err)
	}
}

func TestLoadPluginRejectsMainSymlinkEscapingPluginRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.lua")
	if err := os.WriteFile(outside, []byte("escaped = true"), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "escape")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"escape\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pluginDir, "init.lua")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	if err := mgr.LoadPlugin(pluginDir); err == nil {
		t.Fatal("LoadPlugin() accepted a main file symlink escaping the plugin root")
	}
}

func TestLoadPluginCapsLoadedPluginCount(t *testing.T) {
	root := t.TempDir()
	pluginDir := writeBudgetPlugin(t, root, "overflow", "")
	mgr, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	for i := 0; i < maxLoadedPlugins; i++ {
		name := "existing-" + strconv.Itoa(i)
		mgr.plugins[name] = &Plugin{Name: name, Enabled: false}
	}

	err = mgr.LoadPlugin(pluginDir)
	if !errors.Is(err, ErrPluginResourceLimit) {
		t.Fatalf("LoadPlugin() error = %v, want ErrPluginResourceLimit", err)
	}
	// Test-owned placeholders have no Lua states; remove them before Shutdown.
	clear(mgr.plugins)
}

func TestKeymapRegistryCapsBindingsPerState(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerKeymapAPI(L)
	L.SetGlobal("keymap", L.Get(-1))
	L.Pop(1)
	defer clearKeybindingsForState(L)

	err := L.DoString(`
for i = 1, ` + strconv.Itoa(maxPluginKeymaps+1) + ` do
  keymap.set("n", "key" .. i, function() end)
end
`)
	if err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("DoString() error = %v, want keymap resource limit", err)
	}
}

func TestAutocmdRegistryCapsCallbacksPerState(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerAutocmdAPI(L)
	L.SetGlobal("autocmd", L.Get(-1))
	L.Pop(1)
	defer clearAutocommandsForState(L)

	err := L.DoString(`
for i = 1, ` + strconv.Itoa(maxPluginAutocmds+1) + ` do
  autocmd.register("BufWrite", function() end)
end
`)
	if err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("autocmdRegister() error = %v, want resource limit", err)
	}
}

func TestCommandRegistryCapsActionsPerState(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerEditorAPI(L)
	L.SetGlobal("editor", L.Get(-1))
	L.Pop(1)
	defer clearCommandsForState(L)

	err := L.DoString(`
for i = 1, ` + strconv.Itoa(maxPluginCommands+1) + ` do
  editor.command("command" .. i, function() end)
end
`)
	if err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("editorCommand() error = %v, want resource limit", err)
	}
}
