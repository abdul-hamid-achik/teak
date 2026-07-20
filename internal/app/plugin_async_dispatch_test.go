package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/plugin"
)

func TestPluginKeyCallbackRunsOutsideUpdateWithBudget(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", configHome)

	dir := filepath.Join(plugin.DefaultDir(), "loop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte("name = \"loop\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(`function setup() keymap.set("n", "ctrl+g", function() while true do end end) end`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	loadPluginsForTest(t, &model)
	defer model.cleanup()
	model.focus = FocusEditor

	start := time.Now()
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("Update ran plugin Lua for %s", elapsed)
	}
	if cmd == nil {
		t.Fatal("plugin key dispatch command is nil")
	}

	resultMsg := cmd()
	updatedModel, _ := updated.(Model).Update(resultMsg)
	result := updatedModel.(Model)
	if !strings.Contains(result.status, "execution budget exceeded") {
		t.Fatalf("status = %q, want budget error", result.status)
	}
	if got := len(result.pluginMgr.ListPlugins()); got != 0 {
		t.Fatalf("plugins after timeout = %d, want 0", got)
	}
}

func TestPluginAutocommandRunsOutsideUpdateWithBudget(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", configHome)

	dir := filepath.Join(plugin.DefaultDir(), "loop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte("name = \"loop\"\nmain = \"init.lua\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(`function setup() autocmd.register("BufWrite", function() while true do end end) end`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	loadPluginsForTest(t, &model)
	defer model.cleanup()

	start := time.Now()
	updated, cmd := model.Update(pluginEventMsg{Events: []plugin.EventContext{{Event: plugin.EventBufWrite}}})
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("Update ran plugin Lua for %s", elapsed)
	}
	if cmd == nil {
		t.Fatal("plugin event dispatch command is nil")
	}
	resultMsg := cmd()
	updatedModel, _ := updated.(Model).Update(resultMsg)
	result := updatedModel.(Model)
	if !strings.Contains(result.status, "execution budget exceeded") {
		t.Fatalf("status = %q, want budget error", result.status)
	}
}
