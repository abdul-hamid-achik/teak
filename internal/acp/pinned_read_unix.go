//go:build unix

package acp

import (
	"os"
	"syscall"
)

// O_NONBLOCK prevents a malicious FIFO replacement from stalling the ACP
// request between the pre-open validation and the post-open regular-file check.
func openPinnedReadOnly(root *os.Root, relativePath string) (*os.File, error) {
	return root.OpenFile(relativePath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
