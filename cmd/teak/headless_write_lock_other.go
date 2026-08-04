//go:build !aix && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !linux && !netbsd && !openbsd && !solaris

package main

import (
	"fmt"
	"sync"
	"time"
)

var headlessProcessWriteLock sync.Mutex

type headlessMutexLock struct{}

func acquireHeadlessWriteLock(root, path string) (headlessWriteLock, error) {
	if _, err := headlessWriteLockKey(root, path); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(headlessWriteLockTimeout)
	for {
		if headlessProcessWriteLock.TryLock() {
			return headlessMutexLock{}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("headless write lock is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (headlessMutexLock) Unlock() error {
	headlessProcessWriteLock.Unlock()
	return nil
}
