//go:build darwin || linux

package procmon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestProcessGroupRSSIncludesDescendant(t *testing.T) {
	if os.Getenv("TEAK_PROCMON_RSS_HELPER") == "1" {
		return
	}

	command := fmt.Sprintf("%q -test.run=TestProcessGroupRSSIncludesDescendantHelper", os.Args[0])
	cmd := exec.Command("/bin/sh", "-c", command+" & wait")
	cmd.Env = append(os.Environ(), "TEAK_PROCMON_RSS_HELPER=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process group fixture: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(time.Second)
	for {
		direct, err := ProcessRSS(t.Context(), cmd.Process.Pid)
		if errors.Is(err, ErrRSSUnavailable) {
			t.Skip("current platform has no process RSS sampler")
		}
		if err != nil {
			t.Fatalf("ProcessRSS() error = %v", err)
		}
		group, err := ProcessGroupRSS(t.Context(), cmd.Process.Pid)
		if err != nil {
			t.Fatalf("ProcessGroupRSS() error = %v", err)
		}
		if group >= direct+2<<20 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ProcessGroupRSS() = %d, direct RSS = %d; descendant memory was not included", group, direct)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProcessGroupRSSIncludesDescendantHelper(t *testing.T) {
	if os.Getenv("TEAK_PROCMON_RSS_HELPER") != "1" {
		return
	}

	// Keep a real resident allocation in the descendant so the group and direct
	// measurements are observably different. The parent test kills the group.
	const allocationSize = 16 << 20
	resident := make([]byte, allocationSize)
	for i := 0; i < len(resident); i += 4096 {
		resident[i] = byte(i)
	}
	time.Sleep(3 * time.Second)
	if len(resident) == 0 {
		t.Fatal("unreachable allocation")
	}
}
