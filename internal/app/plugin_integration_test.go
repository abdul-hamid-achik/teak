package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/plugin"
	"teak/internal/text"
)

func TestNewModelLoadsPluginsFromDefaultDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte("function setup() end\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	if model.pluginMgr == nil {
		t.Fatal("expected plugin manager to be initialized")
	}
	if len(model.pluginMgr.ListPlugins()) != 1 {
		t.Fatalf("expected 1 loaded plugin, got %d", len(model.pluginMgr.ListPlugins()))
	}
}

func TestPluginKeybindingsExecuteThroughAppUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  keymap.set("n", "ctrl+g", function()
    plugin_triggered = "direct"
  end)
  keymap.set("n", "<leader>sc", function()
    plugin_triggered = "leader"
  end)
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()
	model.focus = FocusEditor

	updatedModel, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	updated := updatedModel.(Model)
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_triggered").String(); got != "direct" {
		t.Fatalf("plugin_triggered after direct key = %q, want %q", got, "direct")
	}

	updated.pluginKeySequence = ""
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
	updated = updatedModel.(Model)
	if updated.pluginKeySequence != "<leader>" {
		t.Fatalf("pluginKeySequence after leader = %q", updated.pluginKeySequence)
	}
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	updated = updatedModel.(Model)
	if updated.pluginKeySequence != "<leader>s" {
		t.Fatalf("pluginKeySequence after leader+s = %q", updated.pluginKeySequence)
	}
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Text: "c"}))
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_triggered").String(); got != "leader" {
		t.Fatalf("plugin_triggered after leader sequence = %q, want %q", got, "leader")
	}
	if updated.pluginKeySequence != "" {
		t.Fatalf("pluginKeySequence should reset after execution, got %q", updated.pluginKeySequence)
	}
}

func TestPluginEditorAndBufferAPIsDriveLiveModel(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	openedPath := filepath.Join(rootDir, "opened.txt")
	if err := os.WriteFile(openedPath, []byte("opened from plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(opened.txt) error = %v", err)
	}

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  keymap.set("n", "ctrl+e", function()
    editor.set_status("plugin status")
    buffer.set_text("hello")
    buffer.set_cursor(1, 6)
    plugin_mode = editor.get_mode()
    plugin_status = editor.get_status()
    plugin_dirty = buffer.is_dirty()
    plugin_cursor_line, plugin_cursor_col = buffer.get_cursor()
  end)
  keymap.set("n", "ctrl+o", function()
    local ok, err = editor.open_file("opened.txt")
    assert(ok, err)
  end)
  keymap.set("n", "ctrl+t", function()
    plugin_tab_count = editor.get_tab_count()
    plugin_active_tab = editor.get_active_tab()
  end)
  keymap.set("n", "ctrl+w", function()
    editor.close_tab()
  end)
  keymap.set("n", "j", function()
    plugin_feed_recursed = "yes"
  end)
  keymap.set("n", "ctrl+f", function()
    editor.feed_keys("j")
  end)
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()
	model.focus = FocusEditor

	updatedModel, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: 'e', Mod: tea.ModCtrl}))
	updated := updatedModel.(Model)
	if got := updated.activeEditor().Buffer.Content(); got != "hello" {
		t.Fatalf("buffer content after ctrl+e = %q, want %q", got, "hello")
	}
	if got := updated.status; got != "plugin status" {
		t.Fatalf("status after ctrl+e = %q, want %q", got, "plugin status")
	}
	if got := updated.activeEditor().Buffer.Cursor; got != (text.Position{Line: 0, Col: 5}) {
		t.Fatalf("cursor after ctrl+e = %#v", got)
	}
	if !updated.tabBar.Tabs[updated.activeTab].Dirty {
		t.Fatal("expected active tab to be dirty after plugin buffer edit")
	}

	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_mode").String(); got != "normal" {
		t.Fatalf("plugin_mode = %q, want %q", got, "normal")
	}
	if got := p.State.GetGlobal("plugin_status").String(); got != "plugin status" {
		t.Fatalf("plugin_status = %q, want %q", got, "plugin status")
	}
	if got := p.State.GetGlobal("plugin_dirty").String(); got != "true" {
		t.Fatalf("plugin_dirty = %q, want %q", got, "true")
	}
	if got := p.State.GetGlobal("plugin_cursor_line").String(); got != "1" {
		t.Fatalf("plugin_cursor_line = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("plugin_cursor_col").String(); got != "6" {
		t.Fatalf("plugin_cursor_col = %q, want %q", got, "6")
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'o', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if got := updated.activeEditor().Buffer.FilePath; got != openedPath {
		t.Fatalf("opened file path = %q, want %q", got, openedPath)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "opened from plugin\n" {
		t.Fatalf("opened file content = %q", got)
	}
	if len(updated.editors) != 2 {
		t.Fatalf("editor count after plugin open = %d, want 2", len(updated.editors))
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_tab_count").String(); got != "2" {
		t.Fatalf("plugin_tab_count = %q, want %q", got, "2")
	}
	if got := p.State.GetGlobal("plugin_active_tab").String(); got != "2" {
		t.Fatalf("plugin_active_tab = %q, want %q", got, "2")
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'w', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if len(updated.editors) != 1 {
		t.Fatalf("editor count after plugin close = %d, want 1", len(updated.editors))
	}
	if got := updated.activeEditor().Buffer.Content(); got != "hello" {
		t.Fatalf("active buffer after plugin close = %q, want %q", got, "hello")
	}
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if got := updated.activeEditor().Buffer.Content(); got != "helloj" {
		t.Fatalf("buffer content after plugin feed_keys = %q, want %q", got, "helloj")
	}
	if got := updated.activeEditor().Buffer.Cursor; got != (text.Position{Line: 0, Col: 6}) {
		t.Fatalf("cursor after plugin feed_keys = %#v", got)
	}
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_feed_recursed").String(); got != "nil" {
		t.Fatalf("plugin_feed_recursed = %q, want %q", got, "nil")
	}
}

func TestPluginUIAPIsDriveLiveModel(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  keymap.set("a", "ctrl+n", function()
    ui.notify("plugin hello", "warn")
  end)
  keymap.set("a", "ctrl+b", function()
    ui.show_panel("tree")
  end)
  keymap.set("a", "ctrl+p", function()
    ui.show_panel("problems")
  end)
  keymap.set("a", "ctrl+d", function()
    ui.show_panel("debugger")
  end)
  keymap.set("a", "ctrl+a", function()
    ui.toggle_panel("agent")
  end)
  keymap.set("a", "ctrl+x", function()
    ui.hide_panel("tree")
  end)
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	cfg.UI.ShowTree = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()
	model.focus = FocusEditor

	updatedModel, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModCtrl}))
	updated := updatedModel.(Model)
	if got := updated.status; got != "Warning: plugin hello" {
		t.Fatalf("status after ui.notify = %q, want %q", got, "Warning: plugin hello")
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'b', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if !updated.showTree || updated.sidebarTab != SidebarFiles || updated.focus != FocusTree {
		t.Fatalf("show_panel(tree) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if !updated.showTree || updated.sidebarTab != SidebarProblems || updated.focus != FocusProblems {
		t.Fatalf("show_panel(problems) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if !updated.showTree || updated.sidebarTab != SidebarDebugger || updated.focus != FocusDebugger {
		t.Fatalf("show_panel(debugger) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if !updated.showAgent || updated.focus != FocusAgent {
		t.Fatalf("toggle_panel(agent) on state = showAgent:%v focus:%v", updated.showAgent, updated.focus)
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if updated.showAgent || updated.focus != FocusEditor {
		t.Fatalf("toggle_panel(agent) off state = showAgent:%v focus:%v", updated.showAgent, updated.focus)
	}

	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if updated.showTree || updated.focus != FocusEditor {
		t.Fatalf("hide_panel(tree) state = showTree:%v focus:%v", updated.showTree, updated.focus)
	}
}

func TestPluginKeybindingsDispatchByFocusMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  keymap.set("a", "ctrl+p", function()
    plugin_scope = "global"
  end)
  keymap.set("tree", "ctrl+t", function()
    plugin_scope = "tree"
  end)
  keymap.set("git", "ctrl+g", function()
    plugin_scope = "git"
  end)
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	model.focus = FocusTree
	updatedModel, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl}))
	updated := updatedModel.(Model)
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_scope").String(); got != "tree" {
		t.Fatalf("plugin_scope after tree key = %q, want %q", got, "tree")
	}

	updated.focus = FocusGitPanel
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_scope").String(); got != "git" {
		t.Fatalf("plugin_scope after git key = %q, want %q", got, "git")
	}

	updated.focus = FocusTree
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_scope").String(); got != "global" {
		t.Fatalf("plugin_scope after global key = %q, want %q", got, "global")
	}
}

func TestPluginAutocmdsFireFromAppWorkflows(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	firstPath := filepath.Join(rootDir, "first.go")
	secondPath := filepath.Join(rootDir, "second.go")
	if err := os.WriteFile(firstPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(first.go) error = %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(second.go) error = %v", err)
	}

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  autocmd.register("VimEnter", function(ev)
    vim_enter_count = (vim_enter_count or 0) + 1
  end)
  autocmd.register("BufRead", function(ev)
    read_count = (read_count or 0) + 1
    last_read = ev.relative_path
  end, { pattern = "*.go" })
  autocmd.register("BufEnter", function(ev)
    enter_count = (enter_count or 0) + 1
  end)
  autocmd.register("BufLeave", function(ev)
    leave_count = (leave_count or 0) + 1
  end)
  autocmd.register("TextChanged", function(ev)
    changed_count = (changed_count or 0) + 1
  end)
  autocmd.register("CursorMoved", function(ev)
    cursor_count = (cursor_count or 0) + 1
  end)
  autocmd.register("BufWrite", function(ev)
    write_count = (write_count or 0) + 1
    last_write = ev.relative_path
  end)
  autocmd.register("BufDelete", function(ev)
    delete_count = (delete_count or 0) + 1
    last_delete = ev.relative_path
  end)
end
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte("name = \"sample\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.toml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatalf("WriteFile(init.lua) error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false

	model, err := NewModel("", rootDir, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	updatedModel, _ := model.Update(pluginEventMsg{Events: []plugin.EventContext{{Event: plugin.EventVimEnter}}})
	updated := updatedModel.(Model)
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("vim_enter_count").String(); got != "1" {
		t.Fatalf("vim_enter_count = %q, want %q", got, "1")
	}

	openedModel, loadCmd := updated.openFilePinned(firstPath)
	if loadCmd == nil {
		t.Fatal("expected load command when opening first file")
	}
	fileMsg := loadCmd()
	updatedModel, _ = openedModel.(Model).Update(fileMsg)
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("read_count").String(); got != "1" {
		t.Fatalf("read_count after first open = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("enter_count").String(); got != "1" {
		t.Fatalf("enter_count after first open = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("last_read").String(); got != "first.go" {
		t.Fatalf("last_read = %q, want %q", got, "first.go")
	}

	updated.focus = FocusEditor
	updatedModel, _ = updated.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("changed_count").String(); got != "1" {
		t.Fatalf("changed_count after edit = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("cursor_count").String(); got != "1" {
		t.Fatalf("cursor_count after edit = %q, want %q", got, "1")
	}

	updatedModel, saveCmd := updated.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	updated = updatedModel.(Model)
	if saveCmd == nil {
		t.Fatal("expected save command")
	}
	updatedModel, _ = updated.Update(saveCmd())
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("write_count").String(); got != "1" {
		t.Fatalf("write_count after save = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("last_write").String(); got != "first.go" {
		t.Fatalf("last_write = %q, want %q", got, "first.go")
	}

	openedModel, loadCmd = updated.openFilePinned(secondPath)
	if loadCmd == nil {
		t.Fatal("expected load command when opening second file")
	}
	updated = openedModel.(Model)
	fileMsg = loadCmd()
	updatedModel, _ = updated.Update(fileMsg)
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("read_count").String(); got != "2" {
		t.Fatalf("read_count after second open = %q, want %q", got, "2")
	}
	if got := p.State.GetGlobal("enter_count").String(); got != "2" {
		t.Fatalf("enter_count after second open = %q, want %q", got, "2")
	}
	if got := p.State.GetGlobal("leave_count").String(); got != "1" {
		t.Fatalf("leave_count after switching files = %q, want %q", got, "1")
	}

	updatedModel, _ = updated.closeTab(updated.activeTab)
	updated = updatedModel.(Model)
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("delete_count").String(); got != "1" {
		t.Fatalf("delete_count after close = %q, want %q", got, "1")
	}
	if got := p.State.GetGlobal("last_delete").String(); got != "second.go" {
		t.Fatalf("last_delete = %q, want %q", got, "second.go")
	}
}
