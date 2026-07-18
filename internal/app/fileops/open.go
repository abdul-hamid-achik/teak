package fileops

import (
	"os"
	"path/filepath"
	"strings"
)

func IsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func IsReadable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	return file.Close() == nil
}

func ValidatePath(path string) error {
	if path == "" {
		return ErrEmptyPath
	}

	absPath, err := Abs(path)
	if err != nil {
		return ErrInvalidPath
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotExist
		}
		return err
	}

	if info.IsDir() {
		return ErrIsDirectory
	}

	return nil
}

func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func GetExt(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

func GetBaseName(path string) string {
	return filepath.Base(path)
}

func IsHiddenFile(path string) bool {
	base := filepath.Base(path)
	return len(base) > 1 && base[0] == '.'
}

func NormalizePath(path string) string {
	abs, err := Abs(path)
	if err != nil {
		return path
	}
	return Clean(abs)
}
