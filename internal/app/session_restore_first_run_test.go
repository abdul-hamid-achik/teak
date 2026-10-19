package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"teak/internal/config"
	"teak/internal/session"
)

// A workspace opened for the first time has no session file. That is the
// normal case, not a failure: restoration must stay silent (no status text, no
// WARN), while a session file that exists but cannot be trusted or parsed must
// still be reported. The real session loader is used so the legacy-migration
// fallback and its error chain are exercised end to end.
func TestSessionRestoreMissingCorruptAndValidSessionFiles(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, root, sessionPath, kept string)
		wantStatus string // substring; empty means the status must stay empty
		wantWarn   bool
		wantTabs   int
	}{
		{
			name:  "missing session file is a silent first run",
			setup: func(*testing.T, string, string, string) {},
		},
		{
			name: "corrupt session JSON surfaces",
			setup: func(t *testing.T, _, sessionPath, _ string) {
				writeSessionFixture(t, sessionPath, []byte("{not json"))
			},
			wantStatus: "Session restore failed:",
			wantWarn:   true,
		},
		{
			name: "symlinked session file is still rejected",
			setup: func(t *testing.T, root, sessionPath, kept string) {
				target := filepath.Join(t.TempDir(), "elsewhere.json")
				data, err := json.Marshal(session.State{Version: 1, RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: kept}}})
				if err != nil {
					t.Fatal(err)
				}
				writeSessionFixture(t, target, data)
				if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, sessionPath); err != nil {
					t.Skipf("symlink unsupported: %v", err)
				}
			},
			wantStatus: "Session restore failed:",
			wantWarn:   true,
		},
		{
			name: "valid session restores its tab",
			setup: func(t *testing.T, root, _, kept string) {
				if err := session.Save(session.State{Version: 1, RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: kept}}}); err != nil {
					t.Fatalf("session.Save: %v", err)
				}
			},
			wantTabs: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := t.TempDir()
			kept := filepath.Join(root, "kept.go")
			if err := os.WriteFile(kept, []byte("package p\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// The per-workspace session directory is deliberately not created:
			// a first launch has neither the directory nor the file.
			sessionPath := session.PathForRoot(root)
			tt.setup(t, root, sessionPath, kept)

			cfg := config.DefaultConfig()
			cfg.Agent.Enabled = false
			m, err := NewModel("", root, cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.cleanup)
			if !m.sessionRestoreEligible {
				t.Fatal("model is not eligible for session restore")
			}
			// NewModel installs its own file logger; capture after it.
			var logs bytes.Buffer
			previous := log.Default()
			logger := log.New(&logs)
			logger.SetLevel(log.DebugLevel)
			log.SetDefault(logger)
			t.Cleanup(func() { log.SetDefault(previous) })

			result := sessionRestoreCmd(m.sessionRestoreCtx, m.sessionRestoreGeneration, root)()
			updated, command := m.Update(result)
			m = updated.(Model)

			if tt.wantStatus == "" && m.status != "" {
				t.Fatalf("status = %q, want no status message", m.status)
			}
			if tt.wantStatus != "" && !strings.Contains(m.status, tt.wantStatus) {
				t.Fatalf("status = %q, want it to contain %q", m.status, tt.wantStatus)
			}
			warned := strings.Contains(logs.String(), "WARN") && strings.Contains(logs.String(), "session restore failed")
			if warned != tt.wantWarn {
				t.Fatalf("WARN session restore failed logged = %v, want %v; log:\n%s", warned, tt.wantWarn, logs.String())
			}
			if tt.wantTabs == 0 {
				return
			}
			if command == nil {
				t.Fatal("valid session did not schedule its file loads")
			}
			if len(m.editors) != tt.wantTabs || m.activeEditor().Buffer.FilePath != kept {
				t.Fatalf("restored tabs = %+v, want %d tab(s) with %s active", m.tabBar.Tabs, tt.wantTabs, kept)
			}
		})
	}
}

func writeSessionFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
