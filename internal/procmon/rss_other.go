//go:build !linux && !darwin

package procmon

import "context"

func processRSS(context.Context, int) (uint64, error) {
	return 0, ErrRSSUnavailable
}

func processGroupRSS(context.Context, int) (uint64, error) {
	return 0, ErrRSSUnavailable
}
