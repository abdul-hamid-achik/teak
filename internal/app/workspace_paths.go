package app

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (m *Model) workspaceRelativePath(path string) (string, string, error) {
	if m.rootDir == "" {
		return "", "", fmt.Errorf("workspace root is unavailable")
	}
	if path == "" {
		return "", "", fmt.Errorf("workspace path is empty")
	}

	rootPath, err := filepath.Abs(m.rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace path: %w", err)
	}

	relativePath, err := filepath.Rel(rootPath, absolutePath)
	if err != nil {
		return "", "", fmt.Errorf("relativize workspace path: %w", err)
	}
	relativePath = filepath.Clean(relativePath)
	if relativePath == "." ||
		relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relativePath) {
		return "", "", fmt.Errorf("path %q is outside workspace %q", path, rootPath)
	}

	return relativePath, filepath.Join(rootPath, relativePath), nil
}
