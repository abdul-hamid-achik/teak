package fileops

import (
	"os"
	"path/filepath"
)

// Read reads a file and returns its contents
func Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Write writes data to a file
func Write(path string, data []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Delete deletes a file
func Delete(path string) error {
	return os.Remove(path)
}

// Mkdir creates a directory
func Mkdir(path string) error {
	return os.MkdirAll(path, 0755)
}

// Exists checks if a path exists
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir checks if a path is a directory
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Rename renames a file or directory
func Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// Copy copies a file
func Copy(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return Write(dst, data)
}

// Stat returns file info
func Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
