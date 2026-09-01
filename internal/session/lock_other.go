//go:build !aix && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package session

import (
	"fmt"
	"os"
	"time"
)

func lockFileExclusive(_ *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if fallbackWorkspaceLock.TryLock() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("another Teak instance holds this workspace")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func unlockFile(_ *os.File) error {
	fallbackWorkspaceLock.Unlock()
	return nil
}
