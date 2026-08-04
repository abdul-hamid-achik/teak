package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const agentStoreLockTimeout = 2 * time.Second

type storeLock interface {
	Unlock() error
}

func openStoreLockFile(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create agent runtime store directory: %w", err)
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect agent runtime store directory: %w", err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return nil, fmt.Errorf("agent runtime store directory is not a real directory")
	}

	lockPath := path + ".lock"
	lockInfo, err := os.Lstat(lockPath)
	if err == nil {
		if lockInfo.Mode()&os.ModeSymlink != 0 || !lockInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("agent runtime store lock is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect agent runtime store lock: %w", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open agent runtime store lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure agent runtime store lock: %w", err)
	}
	return file, nil
}
