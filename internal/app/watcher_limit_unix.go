//go:build !windows

package app

import (
	"errors"
	"syscall"
)

func defaultMaxWatches() int {
	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		return defaultWatchLimit
	}

	cur := int(rlimit.Cur)
	if cur <= 0 {
		return defaultWatchLimit
	}
	maxWatches := cur - watchFDReserve
	if maxWatches < minWatchLimit {
		return minWatchLimit
	}
	return maxWatches
}

func isWatchLimitError(err error) bool {
	// Linux inotify reports an exhausted fs.inotify.max_user_watches as
	// ENOSPC; without it the limit stays invisible on the platform where it
	// is hit most often.
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) || errors.Is(err, syscall.ENOSPC)
}
