package fileops

import (
	"os"
	"path/filepath"
)

// DeleteAll removes a file or directory (including contents).
func DeleteAll(path string) error {
	return os.RemoveAll(path)
}

// CreateFile creates an empty file at the given path,
// creating parent directories as needed.
func CreateFile(path string) error {
	dir := filepath.Dir(path)
	if err := Mkdir(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// IsEmptyDir checks if a directory has no files or subdirectories.
func IsEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

// FileCount returns the number of files in a directory (non-recursive).
func FileCount(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	return len(entries)
}
