//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// openWorkspaceEditInput keeps validation and open on the same root-relative
// descriptor path and uses O_NONBLOCK so a FIFO substituted after validation
// cannot freeze the background worker.
func openWorkspaceEditInput(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
