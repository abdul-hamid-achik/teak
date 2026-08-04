//go:build !windows

package app

import (
	"errors"
	"syscall"
	"testing"
)

func TestIsWatchLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EMFILE", syscall.EMFILE, true},
		{"ENFILE", syscall.ENFILE, true},
		// Linux inotify reports an exhausted max_user_watches as ENOSPC;
		// without this classification the limit stays invisible there.
		{"ENOSPC", syscall.ENOSPC, true},
		{"wrapped ENOSPC", errors.New("add /repo: no space left on device"), false},
		{"permission", syscall.EACCES, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWatchLimitError(tt.err); got != tt.want {
				t.Errorf("isWatchLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
