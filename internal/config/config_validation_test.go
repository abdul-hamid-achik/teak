package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/ui"
)

// TestConfigValidation tests that config values are validated
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid default config",
			cfg:  DefaultConfig(),
		},
		{
			name: "tab size too small",
			cfg: Config{
				Editor: EditorConfig{TabSize: 0},
				UI:     UIConfig{Theme: "nord"},
			},
			wantErr: true,
		},
		{
			name: "tab size too large",
			cfg: Config{
				Editor: EditorConfig{TabSize: 9},
				UI:     UIConfig{Theme: "nord"},
			},
			wantErr: true,
		},
		{
			name: "tab size valid max",
			cfg: Config{
				Editor: EditorConfig{TabSize: 8},
				UI:     UIConfig{Theme: "nord"},
			},
		},
		{
			name: "tab size valid min",
			cfg: Config{
				Editor: EditorConfig{TabSize: 1},
				UI:     UIConfig{Theme: "nord"},
			},
		},
		{
			name: "unknown theme",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				UI:     UIConfig{Theme: "nonexistent-theme"},
			},
			wantErr: true,
		},
		{
			name: "valid nord theme",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				UI:     UIConfig{Theme: "nord"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestValidateTabSize tests tab size validation specifically
func TestValidateTabSize(t *testing.T) {
	tests := []struct {
		tabSize int
		wantErr bool
	}{
		{-1, true},
		{0, true},
		{1, false},
		{2, false},
		{4, false},
		{8, false},
		{9, true},
		{100, true},
	}

	for _, tt := range tests {
		cfg := Config{Editor: EditorConfig{TabSize: tt.tabSize}}
		err := cfg.Validate()
		hasErr := err != nil
		if hasErr != tt.wantErr {
			t.Errorf("TabSize=%d: error=%v, wantErr=%v", tt.tabSize, err, tt.wantErr)
		}
	}
}

// TestValidateTheme tests theme validation
func TestValidateTheme(t *testing.T) {
	// Get list of valid themes
	validThemes := ui.ThemeIDs()

	tests := []struct {
		theme   string
		wantErr bool
	}{
		{"nord", false},
		{"gruvbox-dark", false},
		{"monokai", false},
		{"night-owl", false},
		{"material-palenight", false},
		{"Nord", true}, // Case sensitive
		{"", false},    // Empty is OK (will use default)
		{"default", true},
		{"dark", true},
		{"light", true},
		{"nonexistent", true},
	}

	for _, tt := range tests {
		cfg := Config{
			Editor: EditorConfig{TabSize: 4},
			UI:     UIConfig{Theme: tt.theme},
		}
		err := cfg.Validate()
		hasErr := err != nil

		// Check if theme is in valid list
		isValid := false
		for _, t := range validThemes {
			if t == tt.theme {
				isValid = true
				break
			}
		}

		if hasErr && isValid {
			t.Errorf("Theme=%q: unexpected error: %v", tt.theme, err)
		}
		if !hasErr && !isValid && tt.theme != "" {
			t.Errorf("Theme=%q: expected error for unknown theme", tt.theme)
		}
	}
}

func TestKnownThemesUsesUIThemeCatalog(t *testing.T) {
	want := ui.ThemeIDs()
	got := KnownThemes()
	if len(got) != len(want) {
		t.Fatalf("KnownThemes() returned %d IDs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KnownThemes()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got[0] = "changed"
	if KnownThemes()[0] != want[0] {
		t.Error("KnownThemes leaked its returned slice")
	}
}

func TestValidateAgentSandbox(t *testing.T) {
	for _, mode := range []string{"", "off", "auto", "required"} {
		cfg := DefaultConfig()
		cfg.Agent.Sandbox = mode
		if err := cfg.Validate(); err != nil {
			t.Errorf("sandbox mode %q rejected: %v", mode, err)
		}
	}

	cfg := DefaultConfig()
	cfg.Agent.Sandbox = "unsafe"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.sandbox") {
		t.Fatalf("invalid sandbox mode error = %v, want agent.sandbox validation", err)
	}
}

// TestConfigMerge tests that user config merges correctly with defaults
func TestConfigMerge(t *testing.T) {
	defaults := DefaultConfig()
	user := userConfig{
		Editor: &userEditorConfig{
			TabSize: intPtr(2),
		},
	}

	merge(&defaults, &user)

	if defaults.Editor.TabSize != 2 {
		t.Errorf("TabSize = %d, want 2", defaults.Editor.TabSize)
	}

	// Other defaults should be preserved
	if defaults.Editor.AutoIndent != true {
		t.Error("AutoIndent was not preserved")
	}
}

// TestConfigMergePartial tests partial config merges
func TestConfigMergePartial(t *testing.T) {
	defaults := DefaultConfig()
	user := userConfig{
		UI: &userUIConfig{
			Theme: stringPtr("nord"),
		},
	}

	merge(&defaults, &user)

	if defaults.UI.Theme != "nord" {
		t.Errorf("Theme = %q, want \"nord\"", defaults.UI.Theme)
	}

	// Editor settings should be preserved
	if defaults.Editor.TabSize != 4 {
		t.Errorf("TabSize = %d, want 4", defaults.Editor.TabSize)
	}
}

// TestConfigMergeNilFields tests that nil fields don't overwrite defaults
func TestConfigMergeNilFields(t *testing.T) {
	defaults := DefaultConfig()
	user := userConfig{
		Editor: &userEditorConfig{
			// All fields nil
		},
	}

	merge(&defaults, &user)

	// All defaults should be preserved
	if defaults.Editor.TabSize != 4 {
		t.Errorf("TabSize changed to %d", defaults.Editor.TabSize)
	}
	if defaults.Editor.InsertTabs != false {
		t.Error("InsertTabs changed")
	}
	if defaults.Editor.AutoIndent != true {
		t.Error("AutoIndent changed")
	}
}

// TestConfigLoadNonExistent tests loading config when file doesn't exist
func TestConfigLoadNonExistent(t *testing.T) {
	// Load should return defaults without error
	cfg, err := Load()
	if err != nil {
		t.Errorf("Load() returned error: %v", err)
	}

	// Should have default values
	if cfg.Editor.TabSize != 4 {
		t.Errorf("TabSize = %d, want 4", cfg.Editor.TabSize)
	}
}

// TestConfigLoadInvalidTOML tests loading invalid TOML
func TestConfigLoadInvalidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	configFile := filepath.Join(configDir, "config.toml")

	// Write invalid TOML
	invalidTOML := `
	[editor
	tab_size = 4  # Missing closing bracket
	`
	if err := os.WriteFile(configFile, []byte(invalidTOML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// This would test Load() but it uses global configPath
	// For now, skip this test
	t.Skip("Load() uses global configPath, can't test with temp file")
}

// TestConfigAgentValidation tests agent config validation
func TestConfigAgentValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "agent enabled without command",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Agent: AgentConfig{
					Enabled: true,
					Command: "",
				},
			},
			wantErr: true,
		},
		{
			name: "agent disabled without command",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Agent: AgentConfig{
					Enabled: false,
					Command: "",
				},
			},
		},
		{
			name: "agent enabled with command",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Agent: AgentConfig{
					Enabled: true,
					Command: "opencode",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestConfigSessionValidation tests session config validation
func TestConfigSessionValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "session enabled with zero interval",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Session: SessionConfig{
					Enabled:          true,
					AutoSaveInterval: 0,
				},
			},
			wantErr: true,
		},
		{
			name: "session enabled with negative interval",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Session: SessionConfig{
					Enabled:          true,
					AutoSaveInterval: -10,
				},
			},
			wantErr: true,
		},
		{
			name: "session enabled with valid interval",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Session: SessionConfig{
					Enabled:          true,
					AutoSaveInterval: 30,
				},
			},
		},
		{
			name: "session disabled with zero interval",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				Session: SessionConfig{
					Enabled:          false,
					AutoSaveInterval: 0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigRejectsUnsafeTimerAndProcessConfiguration(t *testing.T) {
	validLSP := func(extension string) LSPConfig {
		return LSPConfig{
			Extensions: []string{extension},
			Command:    "test-lsp",
			LanguageID: "test",
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "auto save interval is bounded before duration conversion",
			mutate: func(cfg *Config) {
				cfg.Session.AutoSaveInterval = 24*60*60 + 1
			},
			wantErr: "session.auto_save_interval",
		},
		{
			name: "maximum auto save interval remains valid",
			mutate: func(cfg *Config) {
				cfg.Session.AutoSaveInterval = 24 * 60 * 60
			},
		},
		{
			name: "whitespace only agent command",
			mutate: func(cfg *Config) {
				cfg.Agent.Command = " \t "
			},
			wantErr: "agent.command",
		},
		{
			name: "too many extensions for one language server",
			mutate: func(cfg *Config) {
				extensions := make([]string, 65)
				for i := range extensions {
					extensions[i] = ".x" + string(rune('a'+i%26))
				}
				cfg.LSP = []LSPConfig{{Extensions: extensions, Command: "test-lsp", LanguageID: "test"}}
			},
			wantErr: "extensions exceeds",
		},
		{
			name: "duplicate extensions across language servers",
			mutate: func(cfg *Config) {
				cfg.LSP = []LSPConfig{validLSP(".go"), validLSP(".go")}
			},
			wantErr: "duplicate extension",
		},
		{
			name: "uppercase extension cannot silently fail matching",
			mutate: func(cfg *Config) {
				cfg.LSP = []LSPConfig{validLSP(".GO")}
			},
			wantErr: "must be lowercase",
		},
		{
			name: "total language server arguments are bounded",
			mutate: func(cfg *Config) {
				cfg.LSP = make([]LSPConfig, 9)
				for i := range cfg.LSP {
					args := make([]string, 128)
					for j := range args {
						args[j] = "--option"
					}
					cfg.LSP[i] = LSPConfig{
						Extensions: []string{".lang" + string(rune('a'+i))},
						Command:    "test-lsp",
						LanguageID: "test",
						Args:       args,
					}
				}
			},
			wantErr: "total lsp args",
		},
		{
			name: "language server environment names cannot contain separators",
			mutate: func(cfg *Config) {
				server := validLSP(".env")
				server.Env = map[string]string{"BAD=NAME": "1"}
				cfg.LSP = []LSPConfig{server}
			},
			wantErr: "lsp[0].env",
		},
		{
			name: "language server environment count is bounded",
			mutate: func(cfg *Config) {
				server := validLSP(".env")
				server.Env = make(map[string]string, maxLSPEnvEntries+1)
				for i := 0; i < maxLSPEnvEntries+1; i++ {
					server.Env[fmt.Sprintf("TEAK_ENV_%d", i)] = "1"
				}
				cfg.LSP = []LSPConfig{server}
			},
			wantErr: "env exceeds",
		},
		{
			name: "aggregate string data is bounded before config encoding",
			mutate: func(cfg *Config) {
				cfg.Agent.Args = make([]string, 128)
				for i := range cfg.Agent.Args {
					cfg.Agent.Args[i] = strings.Repeat("a", 4<<10)
				}
			},
			wantErr: "configuration string data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() succeeded, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestConfigLSPValidation tests LSP config validation
func TestConfigLSPValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "LSP with empty command",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				LSP: []LSPConfig{
					{
						Extensions: []string{".go"},
						Command:    "",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "LSP with valid command",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				LSP: []LSPConfig{
					{
						Extensions: []string{".go"},
						Command:    "gopls",
						LanguageID: "go",
					},
				},
			},
		},
		{
			name: "LSP with empty language id",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				LSP: []LSPConfig{
					{
						Extensions: []string{".go"},
						Command:    "gopls",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "LSP with no extensions",
			cfg: Config{
				Editor: EditorConfig{TabSize: 4},
				LSP: []LSPConfig{
					{
						Extensions: nil,
						Command:    "gopls",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestConfigValidateAllFields tests validation with all fields set
func TestConfigValidateAllFields(t *testing.T) {
	cfg := Config{
		Editor: EditorConfig{
			TabSize:      4,
			InsertTabs:   false,
			AutoIndent:   true,
			FormatOnSave: true,
			WordWrap:     false,
		},
		UI: UIConfig{
			Theme:    "nord",
			ShowTree: true,
		},
		LSP: []LSPConfig{
			{
				Extensions: []string{".go"},
				Command:    "gopls",
				LanguageID: "go",
			},
		},
		Agent: AgentConfig{
			Enabled: true,
			Command: "opencode",
			Args:    []string{"acp"},
		},
		Session: SessionConfig{
			Enabled:          true,
			AutoSaveInterval: 30,
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() returned error: %v", err)
	}
}

func TestToolOverrideValidation(t *testing.T) {
	valid := DefaultConfig()
	valid.Tools = map[string]string{
		"codemap": "/opt/teak/bin/codemap",
		"gopls":   "/opt/teak/bin/gopls",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid tool overrides rejected: %v", err)
	}

	for name, tools := range map[string]map[string]string{
		"empty tool name":     {"": "/opt/codemap"},
		"path-like tool name": {"../codemap": "/opt/codemap"},
		"empty path":          {"codemap": "   "},
		"nul path":            {"codemap": "/opt/codemap\x00fixture"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Tools = tools
			if err := cfg.Validate(); err == nil {
				t.Fatal("tool override validation succeeded, want error")
			}
		})
	}
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

func stringPtr(s string) *string {
	return &s
}
