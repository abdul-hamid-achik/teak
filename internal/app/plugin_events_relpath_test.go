package app

import (
	"path/filepath"
	"testing"

	"teak/internal/plugin"
)

func TestPluginEventRelativePathKeepsDotDotPrefixedNames(t *testing.T) {
	root := t.TempDir()
	m := Model{modelState: &modelState{rootDir: root}}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"ordinary file", filepath.Join(root, "main.go"), "main.go"},
		{"dot-dot prefixed directory", filepath.Join(root, "..notes", "a.go"), "..notes/a.go"},
		{"file outside root", filepath.Join(filepath.Dir(root), "outside.go"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.enrichPluginEventContext(plugin.EventContext{FilePath: tt.path})
			if got.RelativePath != tt.want {
				t.Fatalf("RelativePath = %q, want %q", got.RelativePath, tt.want)
			}
		})
	}
}
