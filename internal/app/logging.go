package app

import (
	"fmt"
	"os"
	"path/filepath"
)

func openPrivateLogFile(home string) (*os.File, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	logDir := filepath.Join(home, ".local", "state", "teak")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	info, err := os.Lstat(logDir)
	if err != nil {
		return nil, fmt.Errorf("inspect log directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("log directory is not a regular directory")
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}

	root, err := os.OpenRoot(logDir)
	if err != nil {
		return nil, fmt.Errorf("open log directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	if info, err := root.Lstat("teak.log"); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("log destination is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect log destination: %w", err)
	}

	logFile, err := root.OpenFile("teak.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("secure log file: %w", err)
	}
	return logFile, nil
}
