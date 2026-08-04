//go:build windows

package runtime

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

type windowsStoreLock struct {
	file       *os.File
	overlapped *windows.Overlapped
}

func acquireStoreLock(path string) (storeLock, error) {
	file, err := openStoreLockFile(path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(agentStoreLockTimeout)
	for {
		overlapped := new(windows.Overlapped)
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			return windowsStoreLock{file: file, overlapped: overlapped}, nil
		}
		if err != windows.ERROR_LOCK_VIOLATION && err != windows.ERROR_IO_PENDING {
			_ = file.Close()
			return nil, fmt.Errorf("acquire agent runtime store lock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("agent runtime store lock is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (lock windowsStoreLock) Unlock() error {
	unlockErr := windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, lock.overlapped)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
