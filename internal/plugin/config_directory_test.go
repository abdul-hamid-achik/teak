package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDirHonorsAbsoluteXDGConfigHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	want := filepath.Join(root, "teak", "plugins")
	if got := DefaultDir(); got != want {
		t.Fatalf("DefaultDir() = %q, want %q beside config.toml", got, want)
	}
	dir := filepath.Join(want, "fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"plugin.toml": "name = \"fixture\"\nmain = \"init.lua\"\n",
		"init.lua":    "function setup() end\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := NewManager(DefaultDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()
	if err := mgr.LoadAllPlugins(); err != nil {
		t.Fatal(err)
	}
	if len(mgr.ListPlugins()) != 1 {
		t.Fatal("plugin next to the selected config directory was not loaded")
	}
}
