package app

import (
	"path/filepath"
	"strings"
	"testing"
)

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
