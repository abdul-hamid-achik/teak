package acp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMcpServersFromPathValidatesBoundsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	valid := `{"mcp":{"local":{"command":["npx","-y","server"]}}}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := loadMcpServersFromPath(path)
	if err != nil || len(servers) != 1 || servers[0].Stdio == nil || servers[0].Stdio.Command != "npx" {
		t.Fatalf("loadMcpServersFromPath() = %#v, %v", servers, err)
	}

	over := filepath.Join(dir, "large.json")
	if err := os.WriteFile(over, []byte(strings.Repeat("x", maxOpenCodeConfigBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMcpServersFromPath(over); err == nil {
		t.Fatal("loadMcpServersFromPath() accepted oversized config")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := loadMcpServersFromPath(link); err == nil {
		t.Fatal("loadMcpServersFromPath() accepted symlinked config")
	}
}

func TestLoadMcpServersFromPathRejectsInvalidCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	invalid := `{"mcp":{"bad":{"command":["", "arg"]}}}`
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMcpServersFromPath(path); err == nil {
		t.Fatal("loadMcpServersFromPath() accepted empty command")
	}
}
