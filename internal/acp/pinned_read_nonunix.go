//go:build !unix

package acp

import "os"

func openPinnedReadOnly(root *os.Root, relativePath string) (*os.File, error) {
	return root.Open(relativePath)
}
