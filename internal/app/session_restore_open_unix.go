//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// openSessionRestoreInput relies on os.Root for race-free confinement and
// additionally asks Unix to open non-blocking. A malicious replacement with a
// FIFO therefore cannot stall startup before the descriptor is inspected.
func openSessionRestoreInput(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
