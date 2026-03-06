package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/overlay"
)

// TestAgentPanelWidth tests the responsive agent panel width calculations.
func TestAgentPanelWidth(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		showAgent bool
		showTree  bool
		want      int
	}{
		{"hidden panel returns 0", 200, false, false, 0},
		{"wide terminal no tree", 200, true, false, 60}, // 199 avail, 35% = 69, capped to 60
		{"wide terminal with tree", 200, true, true, 60},
		{"medium terminal no tree", 140, true, false, 48}, // 139 avail, 35% = 48
		{"narrow terminal auto-hides", 60, true, false, 0}, // too narrow for editor
		{"very narrow auto-hides", 50, true, true, 0},
		{"just enough for both", 100, true, false, 34}, // 99 avail, 35%=34, editor=65
		{"minimum clamp 25", 80, true, false, 27},       // 79 avail, 35%=27, editor=52
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				width:     tt.width,
				showAgent: tt.showAgent,
				showTree:  tt.showTree,
			}
			got := m.agentPanelWidth()
			if got != tt.want {
				t.Errorf("agentPanelWidth() = %d, want %d (width=%d, showAgent=%v, showTree=%v)",
					got, tt.want, tt.width, tt.showAgent, tt.showTree)
			}
		})
	}
}

// TestFilesToAgentPickerItems tests converting file paths to picker items.
func TestFilesToAgentPickerItems(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  int
	}{
		{"empty list", nil, 0},
		{"single file", []string{"src/main.go"}, 1},
		{"multiple files", []string{"a.go", "b/c.go", "d/e/f.go"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := filesToAgentPickerItems(tt.files)
			if len(items) != tt.want {
				t.Errorf("len(items) = %d, want %d", len(items), tt.want)
			}
		})
	}

	// Verify label/description for a specific file
	items := filesToAgentPickerItems([]string{"src/internal/main.go"})
	if len(items) != 1 {
		t.Fatal("expected 1 item")
	}
	if items[0].Label != "main.go" {
		t.Errorf("Label = %q, want %q", items[0].Label, "main.go")
	}
	if items[0].Description != "src/internal" {
		t.Errorf("Description = %q, want %q", items[0].Description, "src/internal")
	}
	if msg, ok := items[0].Value.(agentFilePickerSelectMsg); !ok {
		t.Errorf("Value type = %T, want agentFilePickerSelectMsg", items[0].Value)
	} else if msg.Path != "src/internal/main.go" {
		t.Errorf("Value.Path = %q, want %q", msg.Path, "src/internal/main.go")
	}
}

// TestFilesToAgentPickerItems_RootFile tests a file with no directory component.
func TestFilesToAgentPickerItems_RootFile(t *testing.T) {
	items := filesToAgentPickerItems([]string{"README.md"})
	if len(items) != 1 {
		t.Fatal("expected 1 item")
	}
	if items[0].Label != "README.md" {
		t.Errorf("Label = %q, want %q", items[0].Label, "README.md")
	}
	if items[0].Description != "." {
		t.Errorf("Description = %q, want %q", items[0].Description, ".")
	}
}

// TestFilesToAgentPickerItems_PickerItemInterface ensures items satisfy overlay.PickerItem.
func TestFilesToAgentPickerItems_PickerItemInterface(t *testing.T) {
	items := filesToAgentPickerItems([]string{"test.go"})
	// overlay.PickerItem is a struct, just verify the fields are set correctly
	var _ overlay.PickerItem = items[0]
}

// TestValidatePathStrict tests validatePathStrict using a real temp directory.
func TestValidatePathStrict(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file inside
	testFile := filepath.Join(tmpDir, "hello.go")
	os.WriteFile(testFile, []byte("package main"), 0o644)
	// Create a subdir
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0o755)
	subFile := filepath.Join(subDir, "world.go")
	os.WriteFile(subFile, []byte("package sub"), 0o644)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"file in root", "hello.go", false},
		{"file in subdir", "sub/world.go", false},
		{"traversal blocked", "../../../etc/passwd", true},
		{"absolute path outside", "/etc/passwd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validatePathStrict(tmpDir, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for path %q, got result %q", tt.path, result)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for path %q: %v", tt.path, err)
				}
				if result == "" {
					t.Errorf("expected non-empty result for path %q", tt.path)
				}
			}
		})
	}
}

// TestValidatePathValidation tests path validation logic WITHOUT writing any files
func TestValidatePathValidation(t *testing.T) {
	tests := []struct {
		name      string
		rootDir   string
		path      string
		wantValid bool
	}{
		{"simple file", "/project", "file.go", true},
		{"nested file", "/project", "src/main.go", true},
		{"with dot", "/project", "./file.go", true},
		{"parent traversal blocked", "/project", "../test.go", false},
		{"deep traversal blocked", "/project", "../../../etc/passwd", false},
		{"mixed traversal blocked", "/project", "src/../../../etc/passwd", false},
		{"absolute outside blocked", "/project", "/etc/passwd", false},
		{"empty path blocked", "/project", "", false},
		{"windows traversal blocked", "/project", "..\\..\\Windows\\System32", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := simpleValidatePath(tt.rootDir, tt.path)
			if isValid != tt.wantValid {
				t.Errorf("simpleValidatePath(%q, %q) = %v, want %v", tt.rootDir, tt.path, isValid, tt.wantValid)
			}
		})
	}
}

// simpleValidatePath is a simple validation for testing (doesn't resolve symlinks)
func simpleValidatePath(rootDir, path string) bool {
	// Empty path is invalid
	if path == "" {
		return false
	}

	cleanPath := filepath.Clean(path)

	// "." or empty after clean is invalid
	if cleanPath == "." || cleanPath == "" {
		return false
	}

	if filepath.IsAbs(cleanPath) {
		return false
	}

	if strings.HasPrefix(cleanPath, "..") {
		return false
	}

	cleanPath = strings.ReplaceAll(cleanPath, "\\", "/")
	if strings.Contains(cleanPath, "..") {
		return false
	}

	return true
}
