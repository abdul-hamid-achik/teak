package procmon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"teak/internal/toolpath"
)

func writeMonitorFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureMonitor(t *testing.T, fixture string) {
	t.Helper()
	toolpath.Configure(map[string]string{"monitor": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

func TestCurrentParsesBoundedMonitorJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeMonitorFixture(t, `
printf '%s\n' '{"pid":123,"name":"teak","cpu_percent":2.5,"memory":2097152,"status":"running"}'
`)
	configureMonitor(t, fixture)

	info, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if info.PID != 123 || info.RSSBytes != 2097152 || info.Status != "running" {
		t.Fatalf("Current() = %#v, want fixture process info", info)
	}
}

func TestCurrentRejectsOversizedMonitorOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeMonitorFixture(t, `
head -c 100000 /dev/zero
`)
	configureMonitor(t, fixture)

	_, err := Current(context.Background())
	if !errors.Is(err, toolpath.ErrOutputLimit) {
		t.Fatalf("Current() error = %v, want toolpath.ErrOutputLimit", err)
	}
}

func TestRSSHumanFormatsResourceBoundaries(t *testing.T) {
	tests := []struct {
		name string
		rss  uint64
		want string
	}{
		{name: "bytes", rss: 999, want: "999B"},
		{name: "kilobytes", rss: 2 << 10, want: "2K"},
		{name: "megabytes", rss: 12 << 20, want: "12M"},
		{name: "gigabytes", rss: 1536 << 20, want: "1.5G"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (ProcessInfo{RSSBytes: tt.rss}).RSSHuman(); got != tt.want {
				t.Fatalf("RSSHuman() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMonitorStatusSnapshotAndUnregister(t *testing.T) {
	monitor := New()
	monitor.Register(11, "small")
	monitor.Register(22, "large")
	monitor.latest[11] = ProcessInfo{PID: 11, Name: "small", RSSBytes: 2 << 20}
	monitor.latest[22] = ProcessInfo{PID: 22, Name: "large", RSSBytes: 2 << 30}

	label, rss, warning := monitor.Status()
	if label != "large" || rss != "2.0G" || !warning {
		t.Fatalf("Status() = (%q, %q, %t), want largest process and warning", label, rss, warning)
	}

	snapshot := monitor.Snapshot()
	if len(snapshot) != 2 || snapshot[11].RSSBytes != 2<<20 {
		t.Fatalf("Snapshot() = %#v, want both process records", snapshot)
	}
	snapshot[11] = ProcessInfo{}
	if monitor.Snapshot()[11].RSSBytes != 2<<20 {
		t.Fatal("Snapshot() exposed the monitor's mutable map")
	}

	monitor.Unregister(22)
	if label, rss, warning := monitor.Status(); label != "small" || rss != "2M" || warning {
		t.Fatalf("Status() after unregister = (%q, %q, %t), want remaining process", label, rss, warning)
	}
	monitor.Unregister(11)
	if label, rss, warning := monitor.Status(); label != "" || rss != "" || warning {
		t.Fatalf("empty Status() = (%q, %q, %t), want zero values", label, rss, warning)
	}
}

func TestMonitorPollWithoutMonitorIsReadOnly(t *testing.T) {
	toolpath.Configure(map[string]string{"monitor": filepath.Join(t.TempDir(), "missing-monitor")})
	t.Cleanup(func() { toolpath.Configure(nil) })
	monitor := New()
	monitor.Register(os.Getpid(), "teak")
	monitor.Poll(context.Background())
	if snapshot := monitor.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("Snapshot() after unavailable Poll() = %#v, want empty", snapshot)
	}
}

func TestMonitorPollParsesRegisteredProcessesAndDropsFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeMonitorFixture(t, `
if [ "$1" = "process" ]; then
  printf '%s\n' '{"pid":123,"name":"fixture","cpu_percent":4.5,"memory":4096,"status":"running"}'
  exit 0
fi
exit 1
`)
	configureMonitor(t, fixture)
	monitor := New()
	monitor.Register(123, "fixture")
	monitor.Poll(context.Background())
	if got := monitor.Snapshot()[123]; got.RSSBytes != 4096 || got.Status != "running" {
		t.Fatalf("successful Poll() record = %#v, want parsed fixture", got)
	}

	failure := writeMonitorFixture(t, "exit 1\n")
	configureMonitor(t, failure)
	monitor.Poll(context.Background())
	if snapshot := monitor.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("failed Poll() snapshot = %#v, want stale record removed", snapshot)
	}
}
