//go:build !aix && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package runtime

import (
	"fmt"
	"sync"
	"time"
)

var fallbackStoreLock sync.Mutex

type fallbackStoreFileLock struct{}

func acquireStoreLock(path string) (storeLock, error) {
	file, err := openStoreLockFile(path)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	deadline := time.Now().Add(agentStoreLockTimeout)
	for {
		if fallbackStoreLock.TryLock() {
			return fallbackStoreFileLock{}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("agent runtime store lock is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (fallbackStoreFileLock) Unlock() error {
	fallbackStoreLock.Unlock()
	return nil
}
