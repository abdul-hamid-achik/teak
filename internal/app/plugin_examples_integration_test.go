package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lua "github.com/yuin/gopher-lua"
	"teak/internal/config"
	"teak/internal/plugin"
)

func installExamplePlugin(t *testing.T, name string) {
	t.Helper()

	source := filepath.Join("..", "..", "examples", "plugins", name)
	destination := filepath.Join(plugin.DefaultDir(), name)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", destination, err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", source, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		from, err := os.Open(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatalf("Open(%q): %v", entry.Name(), err)
		}
		to, err := os.Create(filepath.Join(destination, entry.Name()))
		if err != nil {
			from.Close()
			t.Fatalf("Create(%q): %v", entry.Name(), err)
		}
		if _, err := io.Copy(to, from); err != nil {
			to.Close()
			from.Close()
			t.Fatalf("Copy(%q): %v", entry.Name(), err)
		}
		if err := to.Close(); err != nil {
			from.Close()
			t.Fatalf("Close(%q): %v", entry.Name(), err)
		}
		if err := from.Close(); err != nil {
			t.Fatalf("Close(%q): %v", entry.Name(), err)
		}
	}
}

func newModelWithExamplePlugins(t *testing.T, names ...string) Model {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	for _, name := range names {
		installExamplePlugin(t, name)
	}

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel(): %v", err)
	}
	t.Cleanup(model.cleanup)
	model.focus = FocusEditor
	return model
}

func invokeExampleCommand(t *testing.T, model *Model, pluginName, command string) {
	t.Helper()
	p, err := model.pluginMgr.GetPlugin(pluginName)
	if err != nil {
		t.Fatalf("GetPlugin(%q): %v", pluginName, err)
	}
	runtime := newPluginRuntime(model)
	model.pluginMgr.SetRuntime(runtime)
	err = p.State.CallByParam(lua.P{
		Fn:      p.State.GetField(p.State.GetGlobal("editor"), "command"),
		NRet:    0,
		Protect: true,
	}, lua.LString(command))
	model.pluginMgr.ClearRuntime()
	if err != nil {
		t.Fatalf("editor.command(%q): %v", command, err)
	}
	if cmd := runtime.command(); cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ := model.Update(msg)
			*model = updated.(Model)
		}
	}
}

func TestShippedPluginExamplesDriveLiveEditorBehavior(t *testing.T) {
	t.Run("autopairs mapping and command insert a pair and keep the cursor inside", func(t *testing.T) {
		model := newModelWithExamplePlugins(t, "autopairs")

		updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 'p', Text: "p"}))
		model = updated.(Model)
		if got := model.activeEditor().Buffer.Content(); got != "()" {
			t.Fatalf("content after <leader>ap = %q, want %q", got, "()")
		}
		if got := model.activeEditor().Buffer.Cursor.Col; got != 1 {
			t.Fatalf("cursor after <leader>ap = %d, want 1", got)
		}

		invokeExampleCommand(t, &model, "autopairs", "autopairs.insert_parens")
		if got := model.activeEditor().Buffer.Content(); got != "(())" {
			t.Fatalf("content after command = %q, want %q", got, "(())")
		}
		if got := model.activeEditor().Buffer.Cursor.Col; got != 2 {
			t.Fatalf("cursor after command = %d, want 2", got)
		}
	})

	t.Run("statusline mapping command and cursor autocmd update the live status", func(t *testing.T) {
		model := newModelWithExamplePlugins(t, "statusline")

		updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
		model = updated.(Model)
		if got := model.status; !strings.Contains(got, "[No Name] | 1:1") {
			t.Fatalf("status after <leader>ss = %q", got)
		}

		invokeExampleCommand(t, &model, "statusline", "statusline.refresh")
		if got := model.status; !strings.Contains(got, "[No Name] | 1:1") {
			t.Fatalf("status after command = %q", got)
		}

		updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
		model = updated.(Model)
		if got := model.status; !strings.Contains(got, "[No Name] | 1:2") {
			t.Fatalf("status after CursorMoved autocmd = %q", got)
		}
	})
}
