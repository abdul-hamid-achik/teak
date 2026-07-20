package procmon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const pollTimeout = 3 * time.Second

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
	available bool
}

// New creates a process monitor. It checks if the monitor binary is available.
func New() *Monitor {
	_, err := exec.LookPath("monitor")
	return &Monitor{
		processes: make(map[int]string),
		latest:    make(map[int]ProcessInfo),
		available: err == nil,
	}
}

// Available returns whether the monitor binary is on PATH.
func (m *Monitor) Available() bool {
	return m.available
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
	if !m.available {
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

func pollProcess(ctx context.Context, pid int) (ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "monitor", "process", strconv.Itoa(pid), "--json").Output()
	if err != nil {
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
