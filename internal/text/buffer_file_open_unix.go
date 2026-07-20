//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package text

import (
	"os"

	"golang.org/x/sys/unix"
)

// openBufferFileReadOnly is non-blocking even if a path is swapped for a FIFO
// between the path stat and open. The descriptor is revalidated by the shared
// opener before any bytes are read.
func openBufferFileReadOnly(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
