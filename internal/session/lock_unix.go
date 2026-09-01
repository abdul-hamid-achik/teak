//go:build aix || darwin || dragonfly || freebsd || hurd || illumos || linux || netbsd || openbsd || solaris

package session

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func lockFileExclusive(file *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return fmt.Errorf("flock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("another Teak instance holds this workspace")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
