//go:build !aix && !darwin && !dragonfly && !freebsd && !hurd && !illumos && !linux && !netbsd && !openbsd && !solaris

package toolpath

import "os/exec"

// configureCommandProcess keeps the portable fallback bounded by
// CommandContext and Cmd.WaitDelay. Unix process-group isolation is provided
// by version_command_unix.go where the platform supports it.
func configureCommandProcess(cmd *exec.Cmd) {}
