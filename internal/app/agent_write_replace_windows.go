//go:build windows

package app

import "os"

// Windows' os.Root methods retain a handle to the workspace root and resolve
// both rename endpoints relative to that handle. Root.Rename uses a native
// replace-if-exists rename, so neither the temporary file nor the target can
// escape a pinned workspace through a path swap or reparse point.
//
// The operating system and filesystem still determine durability and the
// precise atomicity guarantee of the replacement. In particular, Go's
// os.Rename documentation does not make a cross-filesystem atomicity promise
// on non-Unix platforms. Both paths here are under one Root, so they are on
// the same filesystem namespace.
const agentWriteAtomicSupported = true

func replaceAgentWrite(root *os.Root, _ string, tempPath, targetPath string) error {
	return root.Rename(tempPath, targetPath)
}

func renameWorkspacePath(root *os.Root, oldPath, newPath string) error {
	return root.Rename(oldPath, newPath)
}
