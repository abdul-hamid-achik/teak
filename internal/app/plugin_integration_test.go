package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/overlay"
	"teak/internal/plugin"
	"teak/internal/text"
)

func loadPluginsForTest(t *testing.T, model *Model) {
	t.Helper()
	msg := pluginLoadCmd(plugin.DefaultDir(), model.pluginLoadGeneration)()
	updated, _ := model.Update(msg)
	*model = updated.(Model)
	if model.pluginLoading {
		t.Fatal("plugin load did not complete")
	}
}

// updatePluginTest drains the command chain produced by an input/event. Lua
// now runs in tea.Cmd and returns an effect message to Update, so plugin
// integration tests must model Bubble Tea's normal command dispatch rather
// than assuming a callback runs inside the original keypress Update.
func updatePluginTest(t *testing.T, model Model, msg tea.Msg) Model {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = drainPluginCmd(t, model, child)
		}
		return model
	}
	updated, cmd := model.Update(msg)
	return drainPluginCmd(t, updated.(Model), cmd)
}

func drainPluginCmd(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = drainPluginCmd(t, model, child)
		}
		return model
	}
	updated, next := model.Update(msg)
	return drainPluginCmd(t, updated.(Model), next)
}

func TestNewModelDefersPluginLoadingUntilAsyncResult(t *testing.T) {
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
	if model.pluginMgr != nil || !model.pluginLoading {
		t.Fatal("NewModel must not load user Lua before the first frame")
	}
	loadPluginsForTest(t, &model)
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_triggered").String(); got != "direct" {
		t.Fatalf("plugin_triggered after direct key = %q, want %q", got, "direct")
	}
	if updated.goToLineMode {
		t.Fatal("plugin ctrl+g leaked into the global go-to-line shortcut")
	}
	if got := updated.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("plugin ctrl+g leaked into editor content: %q", got)
	}

	updated.pluginKeySequence = ""
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
	if updated.pluginKeySequence != "<leader>" {
		t.Fatalf("pluginKeySequence after leader = %q", updated.pluginKeySequence)
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	if updated.pluginKeySequence != "<leader>s" {
		t.Fatalf("pluginKeySequence after leader+s = %q", updated.pluginKeySequence)
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'c', Text: "c"}))
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

func TestPluginLeaderAbandonedSequenceReinsertsKeys(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	pluginDir := filepath.Join(plugin.DefaultDir(), "sample")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initLua := `
function setup()
  keymap.set("n", "<leader>ap", function()
    plugin_triggered = "autopairs"
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	// Space starts the <leader> prefix and is consumed while the sequence
	// is pending.
	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
	if updated.pluginKeySequence != "<leader>" {
		t.Fatalf("pluginKeySequence after space = %q, want %q", updated.pluginKeySequence, "<leader>")
	}

	// 'x' continues no binding: the buffered space must be reinserted into
	// the editor together with the 'x', not silently dropped.
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	if updated.pluginKeySequence != "" {
		t.Fatalf("pluginKeySequence after abandoned sequence = %q, want cleared", updated.pluginKeySequence)
	}
	if got := updated.activeEditor().Buffer.Content(); got != " x" {
		t.Fatalf("editor content after abandoned leader sequence = %q, want %q", got, " x")
	}

	// A completed sequence after a previous abandonment must still dispatch.
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'p', Text: "p"}))
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_triggered").String(); got != "autopairs" {
		t.Fatalf("plugin_triggered after completed sequence = %q, want %q", got, "autopairs")
	}
	if got := updated.activeEditor().Buffer.Content(); got != " x" {
		t.Fatalf("editor content after completed sequence = %q, want unchanged %q", got, " x")
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'e', Mod: tea.ModCtrl}))
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

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'o', Mod: tea.ModCtrl}))
	if got := updated.activeEditor().Buffer.FilePath; got != openedPath {
		t.Fatalf("opened file path = %q, want %q", got, openedPath)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "opened from plugin\n" {
		t.Fatalf("opened file content = %q", got)
	}
	if len(updated.editors) != 2 {
		t.Fatalf("editor count after plugin open = %d, want 2", len(updated.editors))
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl}))
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

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'w', Mod: tea.ModCtrl}))
	if len(updated.editors) != 1 {
		t.Fatalf("editor count after plugin close = %d, want 1", len(updated.editors))
	}
	if got := updated.activeEditor().Buffer.Content(); got != "hello" {
		t.Fatalf("active buffer after plugin close = %q, want %q", got, "hello")
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
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
  keymap.set("a", "ctrl+u", function()
    plugin_bufnr = ui.new_buffer()
    buffer.set_text("PLUGIN_BUFFER_CONTENT")
    editor.set_status("PLUGIN_BUFFER_" .. tostring(plugin_bufnr))
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModCtrl}))
	if got := updated.status; got != "Warning: plugin hello" {
		t.Fatalf("status after ui.notify = %q, want %q", got, "Warning: plugin hello")
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'b', Mod: tea.ModCtrl}))
	if !updated.showTree || updated.sidebarTab != SidebarFiles || updated.focus != FocusTree {
		t.Fatalf("show_panel(tree) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	if !updated.showTree || updated.sidebarTab != SidebarProblems || updated.focus != FocusProblems {
		t.Fatalf("show_panel(problems) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
	if !updated.showTree || updated.sidebarTab != SidebarDebugger || updated.focus != FocusDebugger {
		t.Fatalf("show_panel(debugger) state = showTree:%v sidebar:%v focus:%v", updated.showTree, updated.sidebarTab, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if !updated.showAgent || updated.focus != FocusAgent {
		t.Fatalf("toggle_panel(agent) on state = showAgent:%v focus:%v", updated.showAgent, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if updated.showAgent || updated.focus != FocusEditor {
		t.Fatalf("toggle_panel(agent) off state = showAgent:%v focus:%v", updated.showAgent, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'x', Mod: tea.ModCtrl}))
	if updated.showTree || updated.focus != FocusEditor {
		t.Fatalf("hide_panel(tree) state = showTree:%v focus:%v", updated.showTree, updated.focus)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl}))
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after ui.new_buffer: %v", err)
	}
	if got := p.State.GetGlobal("plugin_bufnr").String(); got != "2" {
		t.Fatalf("ui.new_buffer() returned %q, want 2", got)
	}
	if len(updated.editors) != 2 || updated.activeTab != 1 {
		t.Fatalf("ui.new_buffer() state = editors:%d active:%d, want 2/1", len(updated.editors), updated.activeTab)
	}
	if updated.status != "PLUGIN_BUFFER_2" {
		t.Fatalf("status after ui.new_buffer() = %q, want PLUGIN_BUFFER_2", updated.status)
	}
	if got := updated.tabBar.Tabs[1].Label; got != "Untitled-1" {
		t.Fatalf("ui.new_buffer() label = %q, want Untitled-1", got)
	}
	if got := updated.editors[0].Buffer.Content(); got != "" {
		t.Fatalf("original buffer content = %q, want unchanged empty buffer", got)
	}
	if got := updated.editors[1].Buffer.Content(); got != "PLUGIN_BUFFER_CONTENT" {
		t.Fatalf("new buffer content = %q, want plugin content", got)
	}
}

func TestPluginUIConfirmResumesCallbackWithoutBlockingUpdate(t *testing.T) {
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
  keymap.set("a", "ctrl+c", function()
    ui.confirm("Deploy changes?", {"Deploy", "Cancel"}, function(option, index, accepted)
      confirm_result = option .. ":" .. tostring(index) .. ":" .. tostring(accepted)
      buffer.set_text(confirm_result)
    end)
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	confirm, ok := updated.overlayStack.Top().(*overlay.Confirm)
	if !ok || confirm == nil {
		t.Fatalf("confirm overlay = %T, want *overlay.Confirm", updated.overlayStack.Top())
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEnter})
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after confirm = %v", err)
	}
	if got := p.State.GetGlobal("confirm_result").String(); got != "Deploy:1:true" {
		t.Fatalf("confirm_result after accept = %q, want Deploy:1:true", got)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "Deploy:1:true" {
		t.Fatalf("buffer after confirm callback = %q, want callback result", got)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if updated.overlayStack.IsEmpty() {
		t.Fatal("second confirm did not open")
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEscape})
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after dismiss = %v", err)
	}
	if got := p.State.GetGlobal("confirm_result").String(); got != ":0:false" {
		t.Fatalf("confirm_result after dismiss = %q, want :0:false", got)
	}
}

func TestPluginUIInputResumesCallbackWithoutBlockingUpdate(t *testing.T) {
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
  keymap.set("a", "ctrl+i", function()
    ui.input("Branch name", "feature/", function(value, accepted)
      input_result = value .. ":" .. tostring(accepted)
      buffer.set_text(input_result)
    end)
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'i', Mod: tea.ModCtrl}))
	if _, ok := updated.overlayStack.Top().(*overlay.Input); !ok {
		t.Fatalf("input overlay = %T, want *overlay.Input", updated.overlayStack.Top())
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEnter})
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after input = %v", err)
	}
	if got := p.State.GetGlobal("input_result").String(); got != "feature/:true" {
		t.Fatalf("input_result after accept = %q, want feature/:true", got)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "feature/:true" {
		t.Fatalf("buffer after input callback = %q, want callback result", got)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'i', Mod: tea.ModCtrl}))
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEscape})
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after input dismiss = %v", err)
	}
	if got := p.State.GetGlobal("input_result").String(); got != ":false" {
		t.Fatalf("input_result after dismiss = %q, want :false", got)
	}
}

func TestPluginUISelectResumesCallbackWithoutBlockingUpdate(t *testing.T) {
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
  keymap.set("a", "ctrl+l", function()
    ui.select("Choose target", {"one", "two"}, function(option, index, accepted)
      select_result = option .. ":" .. tostring(index) .. ":" .. tostring(accepted)
      buffer.set_text(select_result)
    end)
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	if _, ok := updated.overlayStack.Top().(*overlay.Picker); !ok {
		t.Fatalf("selector overlay = %T, want *overlay.Picker", updated.overlayStack.Top())
	}
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyDown})
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEnter})
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after select = %v", err)
	}
	if got := p.State.GetGlobal("select_result").String(); got != "two:2:true" {
		t.Fatalf("select_result after selection = %q, want two:2:true", got)
	}
	if got := updated.activeEditor().Buffer.Content(); got != "two:2:true" {
		t.Fatalf("buffer after select callback = %q, want callback result", got)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEscape})
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() after select dismiss = %v", err)
	}
	if got := p.State.GetGlobal("select_result").String(); got != ":0:false" {
		t.Fatalf("select_result after dismiss = %q, want :0:false", got)
	}
}

func TestPluginUIFloatCreatesBoundedOverlayAndCloses(t *testing.T) {
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
  keymap.set("a", "ctrl+f", function()
    float_id = ui.new_float({title = "Preview", content = "generated output", width = 40, height = 4})
    editor.set_status("FLOAT_OPEN")
  end)
  autocmd.register("CursorMoved", function()
    ui.close_float(float_id)
    editor.set_status("FLOAT_CLOSED")
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	float, ok := updated.overlayStack.Top().(*overlay.Float)
	if !ok || float == nil {
		t.Fatalf("float overlay = %T, want *overlay.Float", updated.overlayStack.Top())
	}
	if float.Title != "Preview" || float.Content != "generated output" {
		t.Fatalf("float = %#v, want bounded preview content", float)
	}
	if updated.status != "FLOAT_OPEN" {
		t.Fatalf("status after float open = %q, want FLOAT_OPEN", updated.status)
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !updated.overlayStack.IsEmpty() {
		t.Fatal("Escape did not close plugin float")
	}

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	updated = updatePluginTest(t, updated, pluginEventMsg{Events: []plugin.EventContext{{Event: plugin.EventCursorMoved}}})
	if !updated.overlayStack.IsEmpty() {
		t.Fatalf("ui.close_float did not remove plugin float: status=%q top=%T floats=%v", updated.status, updated.overlayStack.Top(), updated.pluginFloats)
	}
	if updated.status != "FLOAT_CLOSED" {
		t.Fatalf("status after float close = %q, want FLOAT_CLOSED", updated.status)
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()

	model.focus = FocusTree
	updated := updatePluginTest(t, model, tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl}))
	p, err := updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_scope").String(); got != "tree" {
		t.Fatalf("plugin_scope after tree key = %q, want %q", got, "tree")
	}

	updated.focus = FocusGitPanel
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	p, err = updated.pluginMgr.GetPlugin("sample")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	if got := p.State.GetGlobal("plugin_scope").String(); got != "git" {
		t.Fatalf("plugin_scope after git key = %q, want %q", got, "git")
	}

	updated.focus = FocusTree
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
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
	loadPluginsForTest(t, &model)
	defer model.cleanup()

	updated := updatePluginTest(t, model, pluginEventMsg{Events: []plugin.EventContext{{Event: plugin.EventVimEnter}}})
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
	updated = updatePluginTest(t, openedModel.(Model), fileMsg)
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
	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
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

	updated = updatePluginTest(t, updated, tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
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
	updated = updatePluginTest(t, updated, fileMsg)
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

	updatedModel, closeCmd := updated.closeTab(updated.activeTab)
	updated = drainPluginCmd(t, updatedModel.(Model), closeCmd)
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
