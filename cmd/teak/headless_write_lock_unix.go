//go:build aix || darwin || dragonfly || freebsd || hurd || illumos || linux || netbsd || openbsd || solaris

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type headlessFileLock struct {
	file *os.File
}

func acquireHeadlessWriteLock(root, path string) (headlessWriteLock, error) {
	key, err := headlessWriteLockKey(root, path)
	if err != nil {
		return nil, err
	}
	lockDir := filepath.Join(os.TempDir(), "teak-headless-locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("create headless write lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(lockDir, key+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open headless write lock: %w", err)
	}
	deadline := time.Now().Add(headlessWriteLockTimeout)
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return headlessFileLock{file: file}, nil
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			_ = file.Close()
			return nil, fmt.Errorf("acquire headless write lock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("headless write lock is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (lock headlessFileLock) Unlock() error {
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
