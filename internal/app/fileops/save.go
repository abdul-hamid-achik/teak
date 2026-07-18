package fileops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func Save(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := Mkdir(dir); err != nil {
		return err
	}
	return Write(path, data)
}

func SaveAtomic(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := Write(tmpPath, data); err != nil {
		return err
	}

	existingInfo, err := os.Stat(path)
	existingPerm := os.FileMode(0644)
	if err == nil {
		existingPerm = existingInfo.Mode().Perm()
	}

	if err := os.Rename(tmpPath, path); err != nil {
		renameErr := fmt.Errorf("rename temporary file: %w", err)
		if cleanupErr := os.Remove(tmpPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			return errors.Join(renameErr, fmt.Errorf("remove temporary file: %w", cleanupErr))
		}
		return renameErr
	}

	if err := os.Chmod(path, existingPerm); err != nil {
		return fmt.Errorf("restore file permissions: %w", err)
	}
	return nil
}

func SaveIfDifferent(path string, data []byte) (bool, error) {
	existing, err := Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, Save(path, data)
		}
		return false, err
	}

	if string(existing) == string(data) {
		return false, nil
	}

	return true, Save(path, data)
}

func HasPermission(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		return false
	}

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			return false
		}
		return false
	}
	return file.Close() == nil
}
