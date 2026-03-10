//go:build windows

package app

func defaultMaxWatches() int {
	return defaultWatchLimit
}

func isWatchLimitError(error) bool {
	return false
}
