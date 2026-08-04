//go:build aix || darwin || dragonfly || freebsd || hurd || illumos || linux || netbsd || openbsd || solaris

package toolpath

import (
	"os/exec"
	"syscall"
	"time"
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
		pgid := cmd.Process.Pid
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			return err
		}
		// A descendant forked between signal delivery and the direct process's
		// death joins the group after the kill was dispatched and escapes it;
		// sweep the group once more after a brief grace period. Reproduced on
		// Linux where the shim's first fork landed in exactly that window.
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}()
		return nil
	}
}
