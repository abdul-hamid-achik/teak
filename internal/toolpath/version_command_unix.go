//go:build aix || darwin || dragonfly || freebsd || hurd || illumos || linux || netbsd || openbsd || solaris

package toolpath

import (
	"os/exec"
	"syscall"
)

// configureCommandProcess isolates a command in its own process group so a
// shim cannot leave descendants holding Teak's output pipes open after the
// direct process is cancelled.
func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
