package fileops

import (
	"errors"
	"os"
	"path/filepath"
)

var (
	ErrEmptyPath      = errors.New("path is empty")
	ErrInvalidPath    = errors.New("invalid path")
	ErrFileNotExist   = errors.New("file does not exist")
	ErrIsDirectory    = errors.New("path is a directory")
	ErrNotRegularFile = errors.New("not a regular file")
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

// ListDir returns the entries in a directory.
func ListDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

// Walk walks the file tree rooted at root, calling walkFn for each file or directory.
func Walk(root string, walkFn func(path string, info os.DirEntry) error) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if err := walkFn(path, entry); err != nil {
			return err
		}
		if entry.IsDir() {
			if err := Walk(path, walkFn); err != nil {
				return err
			}
		}
	}
	return nil
}

// Glob returns the names of all files matching pattern.
func Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// Abs returns the absolute path of f.
func Abs(path string) (string, error) {
	return filepath.Abs(path)
}

// Rel returns a relative path from base to path.
func Rel(base, path string) (string, error) {
	return filepath.Rel(base, path)
}

// Clean returns the shortest path name equivalent to path.
func Clean(path string) string {
	return filepath.Clean(path)
}

// Ext returns the file name extension used by path.
func Ext(path string) string {
	return filepath.Ext(path)
}

// Base returns the last element of path.
func Base(path string) string {
	return filepath.Base(path)
}

// Join joins any number of path elements into a single path.
func Join(elem ...string) string {
	return filepath.Join(elem...)
}
