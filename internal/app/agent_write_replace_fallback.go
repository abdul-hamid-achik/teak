//go:build !aix && !android && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris && !windows

package app

import (
	"os"
)

const agentWriteAtomicSupported = false

func replaceAgentWrite(_ *os.Root, _ string, _ string, _ string) error {
	return errAgentWriteAtomicUnsupported
}

func renameWorkspacePath(_ *os.Root, _ string, _ string) error {
	return errAgentWriteAtomicUnsupported
}
