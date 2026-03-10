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
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}
