package procmon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"teak/internal/toolpath"
)

const (
	pollTimeout    = 3 * time.Second
	maxOutputBytes = 64 << 10
)

// ProcessInfo holds resource usage for a monitored process.
type ProcessInfo struct {
	PID        int
	Name       string
	CPUPercent float64
	RSSBytes   uint64
	Status     string
}

// RSSHuman returns a human-readable RSS string (e.g., "312M", "1.2G").
func (p ProcessInfo) RSSHuman() string {
	switch {
	case p.RSSBytes >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(p.RSSBytes)/(1<<30))
	case p.RSSBytes >= 1<<20:
		return fmt.Sprintf("%dM", p.RSSBytes/(1<<20))
	case p.RSSBytes >= 1<<10:
		return fmt.Sprintf("%dK", p.RSSBytes/(1<<10))
	default:
		return fmt.Sprintf("%dB", p.RSSBytes)
	}
}

// Monitor tracks resource usage of registered processes.
type Monitor struct {
	mu        sync.RWMutex
	processes map[int]string // pid → label (e.g., "gopls", "dlv")
	latest    map[int]ProcessInfo
}

// New creates a process monitor.
func New() *Monitor {
	return &Monitor{
		processes: make(map[int]string),
		latest:    make(map[int]ProcessInfo),
	}
}

// Available reports whether the monitor binary can be resolved. Availability is
// queried on each call rather than latched at construction so that installing
// the binary while Teak is running takes effect without a restart; toolpath
// caches the lookup, so calling this from a render path stays cheap.
func (m *Monitor) Available() bool {
	return toolpath.Available("monitor")
}

// Register adds a process to monitor.
func (m *Monitor) Register(pid int, label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processes[pid] = label
}

// Unregister removes a process from monitoring.
func (m *Monitor) Unregister(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processes, pid)
	delete(m.latest, pid)
}

// Poll refreshes resource info for all registered processes.
func (m *Monitor) Poll(ctx context.Context) {
	if !m.Available() {
		return
	}
	m.mu.RLock()
	pids := make([]int, 0, len(m.processes))
	for pid := range m.processes {
		pids = append(pids, pid)
	}
	m.mu.RUnlock()

	for _, pid := range pids {
		info, err := pollProcess(ctx, pid)
		if err != nil {
			m.mu.Lock()
			delete(m.latest, pid)
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		m.latest[pid] = info
		m.mu.Unlock()
	}
}

// Status returns the label and RSS of the most resource-heavy registered process.
func (m *Monitor) Status() (label string, rss string, warning bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var maxRSS uint64
	var maxPID int
	for pid, info := range m.latest {
		if info.RSSBytes > maxRSS {
			maxRSS = info.RSSBytes
			maxPID = pid
		}
	}
	if maxPID == 0 {
		return "", "", false
	}
	lbl := m.processes[maxPID]
	info := m.latest[maxPID]
	return lbl, info.RSSHuman(), info.RSSBytes > 1<<30 // warn above 1G
}

// Snapshot returns a copy of all current process info.
func (m *Monitor) Snapshot() map[int]ProcessInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[int]ProcessInfo, len(m.latest))
	for k, v := range m.latest {
		out[k] = v
	}
	return out
}

// Current returns resource usage for the running Teak process when the
// optional monitor executable is installed. It is intended for diagnostics
// and headless performance reports, not for the interactive render path.
func Current(ctx context.Context) (ProcessInfo, error) {
	return pollProcess(ctx, os.Getpid())
}

func pollProcess(ctx context.Context, pid int) (ProcessInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pid <= 0 {
		return ProcessInfo{}, fmt.Errorf("monitor process pid must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "monitor", "process", strconv.Itoa(pid), "--json")
	if err != nil {
		return ProcessInfo{}, err
	}
	out, stderr, err := toolpath.RunBounded(cmd, maxOutputBytes, maxOutputBytes)
	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			return ProcessInfo{}, fmt.Errorf("monitor process: %w: %s", err, detail)
		}
		return ProcessInfo{}, err
	}

	var raw struct {
		PID        int     `json:"pid"`
		Name       string  `json:"name"`
		CPUPercent float64 `json:"cpu_percent"`
		Memory     uint64  `json:"memory"`
		Status     string  `json:"status"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return ProcessInfo{}, err
	}
	return ProcessInfo{
		PID:        raw.PID,
		Name:       raw.Name,
		CPUPercent: raw.CPUPercent,
		RSSBytes:   raw.Memory,
		Status:     raw.Status,
	}, nil
}
