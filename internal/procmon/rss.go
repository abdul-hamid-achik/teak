package procmon

import (
	"context"
	"errors"
)

// ErrRSSUnavailable means the current operating system does not expose a
// bounded, process-level RSS sampler to Teak. Callers enforcing a hard memory
// budget must fail closed when they receive this error.
var ErrRSSUnavailable = errors.New("process RSS sampling is unavailable")

// ProcessRSS returns the resident set size of pid in bytes. It is deliberately
// separate from the optional monitor executable: safety-critical callers must
// not depend on an extra developer tool being installed.
func ProcessRSS(ctx context.Context, pid int) (uint64, error) {
	return processRSS(ctx, pid)
}

// ProcessGroupRSS returns the sum of resident memory for the process group
// containing pid. Teak launches guarded external tools in their own process
// group, so this covers helper processes as well as the direct executable.
// It is deliberately separate from the optional monitor executable and fails
// closed on platforms that cannot inspect the group.
func ProcessGroupRSS(ctx context.Context, pid int) (uint64, error) {
	return processGroupRSS(ctx, pid)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
