//go:build !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package app

import "os"

// Platforms without a portable non-blocking open still validate the opened
// descriptor before reading. os.Root keeps traversal confined; Unix builds
// additionally close the FIFO-open race above.
func openWorkspaceEditInput(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
