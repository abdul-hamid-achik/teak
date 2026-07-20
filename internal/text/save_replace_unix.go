//go:build !windows

package text

import "os"

// replaceFileAtomically replaces destination with source when both paths are
// in the same directory. POSIX rename has the required atomic replacement
// semantics.
func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
