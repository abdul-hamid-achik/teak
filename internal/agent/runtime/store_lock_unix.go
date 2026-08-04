//go:build aix || darwin || dragonfly || freebsd || hurd || illumos || linux || netbsd || openbsd || solaris

package runtime

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type unixStoreLock struct {
	file *os.File
}

func acquireStoreLock(path string) (storeLock, error) {
	file, err := openStoreLockFile(path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(agentStoreLockTimeout)
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return unixStoreLock{file: file}, nil
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
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

func (lock unixStoreLock) Unlock() error {
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
