//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package text

import "os"

// Platforms without a portable O_NONBLOCK open use the pre-stat and descriptor
// recheck in openRegularBufferFile. That preserves regular-file and identity
// validation even though the platform cannot make a hostile FIFO swap itself
// non-blocking.
func openBufferFileReadOnly(path string) (*os.File, error) {
	return os.Open(path)
}
