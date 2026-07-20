//go:build !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package app

import "os"

// openEditorInput is the portable fallback. It follows symlinks, matching the
// platform's normal editor semantics, then the caller rechecks the opened
// descriptor before reading. Unix builds additionally use O_NONBLOCK to close
// the FIFO-open race that this fallback cannot express portably.
func openEditorInput(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errEditorFileNotRegular
	}
	return os.Open(path)
}
