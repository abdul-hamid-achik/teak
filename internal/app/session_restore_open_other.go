//go:build !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package app

import "os"

// os.Root keeps path traversal and symbolic-link resolution confined to the
// root on supported non-Unix platforms. readOpenedEditorFile still rejects
// every non-regular descriptor before allocating or reading its contents.
func openSessionRestoreInput(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
