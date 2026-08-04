//go:build darwin

package procmon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const maxRSSProbeOutput = 128
const maxRSSGroupProbeOutput = 64 << 10

func processRSS(ctx context.Context, pid int) (uint64, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("process RSS pid must be positive")
	}

	// macOS does not expose /proc. /bin/ps is a system utility, not a
	// user-configurable developer tool, and the fixed absolute path keeps this
	// safety probe independent of PATH or shell configuration.
	cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxRSSProbeOutput+1))
	waitErr := cmd.Wait()
	if readErr != nil {
		return 0, readErr
	}
	if len(output) > maxRSSProbeOutput {
		return 0, fmt.Errorf("process RSS probe output exceeds %d bytes", maxRSSProbeOutput)
	}
	if waitErr != nil {
		return 0, waitErr
	}
	fields := strings.Fields(string(output))
	if len(fields) != 1 {
		return 0, fmt.Errorf("process RSS is not reported for pid %d", pid)
	}
	kilobytes, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse process RSS: %w", err)
	}
	return kilobytes * 1024, nil
}

func processGroupRSS(ctx context.Context, pid int) (uint64, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("process group RSS pid must be positive")
	}
	groupOutput, err := runPS(ctx, maxRSSProbeOutput, "-o", "pgid=", "-p", strconv.Itoa(pid))
	if err != nil {
		return 0, err
	}
	groupFields := strings.Fields(string(groupOutput))
	if len(groupFields) != 1 {
		return 0, fmt.Errorf("process group is not reported for pid %d", pid)
	}
	group, err := strconv.Atoi(groupFields[0])
	if err != nil || group <= 0 {
		return 0, fmt.Errorf("parse process group: %q", groupFields[0])
	}

	rssOutput, err := runPS(ctx, maxRSSGroupProbeOutput, "-o", "rss=", "-g", strconv.Itoa(group))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(rssOutput))
	if len(fields) == 0 {
		return 0, fmt.Errorf("process group %d is not present: %w", group, os.ErrNotExist)
	}
	var total uint64
	for _, field := range fields {
		kilobytes, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse process group RSS: %q: %w", field, err)
		}
		total += kilobytes * 1024
	}
	return total, nil
}

func runPS(ctx context.Context, limit int64, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/bin/ps", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, readErr
	}
	if int64(len(output)) > limit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("process group RSS probe output exceeds %d bytes", limit)
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, waitErr
	}
	return output, nil
}
