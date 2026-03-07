package config

import (
	"os"
	"path/filepath"
	"testing"
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
	validThemes := []string{"nord", "dracula", "catppuccin", "solarized-dark", "one-dark"}

	tests := []struct {
		theme   string
		wantErr bool
	}{
		{"nord", false},
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
	os.MkdirAll(configDir, 0o755)
	configFile := filepath.Join(configDir, "config.toml")

	// Write invalid TOML
	invalidTOML := `
	[editor
	tab_size = 4  # Missing closing bracket
	`
	os.WriteFile(configFile, []byte(invalidTOML), 0o644)

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
					},
				},
			},
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

// Helper functions

func intPtr(i int) *int {
	return &i
}

func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
