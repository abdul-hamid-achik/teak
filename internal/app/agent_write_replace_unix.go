//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const agentWriteAtomicSupported = true

func replaceAgentWrite(root *os.Root, parent, tempPath, targetPath string) error {
	parentDir, err := root.Open(parent)
	if err != nil {
		return err
	}
	defer func() {
		_ = parentDir.Close()
	}()

	return unix.Renameat(
		int(parentDir.Fd()),
		filepath.Base(tempPath),
		int(parentDir.Fd()),
		filepath.Base(targetPath),
	)
}

func renameWorkspacePath(root *os.Root, oldPath, newPath string) error {
	oldParent, err := root.Open(filepath.Dir(oldPath))
	if err != nil {
		return err
	}
	defer func() {
		_ = oldParent.Close()
	}()

	newParent, err := root.Open(filepath.Dir(newPath))
	if err != nil {
		return err
	}
	defer func() {
		_ = newParent.Close()
	}()

	return unix.Renameat(
		int(oldParent.Fd()),
		filepath.Base(oldPath),
		int(newParent.Fd()),
		filepath.Base(newPath),
	)
}
