//go:build linux

package procmon

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processRSS(ctx context.Context, pid int) (uint64, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("process RSS pid must be positive")
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "VmRSS:" || fields[2] != "kB" {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse process RSS: %w", err)
		}
		return kilobytes * 1024, nil
	}
	return 0, fmt.Errorf("process RSS is not reported for pid %d", pid)
}

// processGroupRSS sums VmRSS for every process in the command's process
// group. toolpath.ConfigureCommand puts guarded commands in a private group,
// making this a bounded view of the direct process and any helpers it starts.
func processGroupRSS(ctx context.Context, pid int) (uint64, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("process group RSS pid must be positive")
	}
	group, err := linuxProcessGroupID(pid)
	if err != nil {
		return 0, err
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	var total uint64
	found := false
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return 0, err
		}
		candidate, err := strconv.Atoi(entry.Name())
		if err != nil || candidate <= 0 {
			continue
		}
		stat, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		candidateGroup, err := linuxProcessGroupIDFromStat(stat)
		if err != nil || candidateGroup != group {
			continue
		}
		status, err := os.ReadFile("/proc/" + entry.Name() + "/status")
		if err != nil {
			continue
		}
		rss, err := linuxRSSFromStatus(status)
		if err != nil {
			continue
		}
		total += rss
		found = true
	}
	if !found {
		return 0, fmt.Errorf("process group %d is not present: %w", group, os.ErrNotExist)
	}
	return total, nil
}

func linuxProcessGroupID(pid int) (int, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	return linuxProcessGroupIDFromStat(data)
}

func linuxProcessGroupIDFromStat(data []byte) (int, error) {
	line := string(data)
	closeName := strings.LastIndexByte(line, ')')
	if closeName < 0 || closeName+1 >= len(line) {
		return 0, fmt.Errorf("parse process stat: missing command name")
	}
	fields := strings.Fields(line[closeName+1:])
	// The suffix starts at stat field 3 (state); process group is field 5,
	// therefore it is the third token after the closing command name.
	if len(fields) < 3 {
		return 0, fmt.Errorf("parse process stat: missing process group")
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil || group <= 0 {
		return 0, fmt.Errorf("parse process group: %q", fields[2])
	}
	return group, nil
}

func linuxRSSFromStatus(data []byte) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "VmRSS:" || fields[2] != "kB" {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse process RSS: %w", err)
		}
		return kilobytes * 1024, nil
	}
	return 0, fmt.Errorf("process RSS is not reported")
}
