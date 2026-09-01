package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Lock is an exclusive per-workspace lock so two Teak instances do not
// clobber the same session.json / recovery.json.
type Lock struct {
	file *os.File
	path string
}

const workspaceLockTimeout = 200 * time.Millisecond

var fallbackWorkspaceLock sync.Mutex

func lockPath(rootDir string) string {
	return filepath.Join(StateHome(), "sessions", rootKey(rootDir), "workspace.lock")
}

// TryLock acquires an exclusive workspace lock. Callers must Unlock.
func TryLock(rootDir string) (*Lock, error) {
	path := lockPath(rootDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create session lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session lock: %w", err)
	}
	if err := lockFileExclusive(file, workspaceLockTimeout); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("workspace already open: %w", err)
	}
	return &Lock{file: file, path: path}, nil
}

// Unlock releases the workspace lock.
func (l *Lock) Unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
