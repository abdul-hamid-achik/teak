package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestHeadlessCommandPredicatesAndFormatting(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "doctor", args: []string{"doctor"}, want: true},
		{name: "doctor with option", args: []string{"doctor", "--json"}, want: true},
		{name: "other command", args: []string{"headless"}, want: false},
		{name: "empty", args: nil, want: false},
	} {
		t.Run("doctor/"+tt.name, func(t *testing.T) {
			if got := isDoctorCommand(tt.args); got != tt.want {
				t.Fatalf("isDoctorCommand(%v) = %t, want %t", tt.args, got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "headless", args: []string{"headless"}, want: true},
		{name: "headless with operation", args: []string{"headless", "context"}, want: true},
		{name: "doctor", args: []string{"doctor"}, want: false},
		{name: "empty", args: nil, want: false},
	} {
		t.Run("headless/"+tt.name, func(t *testing.T) {
			if got := isHeadlessCommand(tt.args); got != tt.want {
				t.Fatalf("isHeadlessCommand(%v) = %t, want %t", tt.args, got, tt.want)
			}
		})
	}

	if got := truncatedSuffix(false); got != "" {
		t.Fatalf("truncatedSuffix(false) = %q, want empty", got)
	}
	if got := truncatedSuffix(true); got != " (truncated)" {
		t.Fatalf("truncatedSuffix(true) = %q", got)
	}
}

func TestWriteDoctorReportSortsChecksAndRendersDetails(t *testing.T) {
	var output bytes.Buffer
	writeDoctorReport(&output, doctorReport{
		Version:    "test-version",
		GoVersion:  "go1.test",
		OS:         "test-os",
		Arch:       "test-arch",
		Workspace:  "/workspace",
		ConfigPath: "/config/teak.toml",
		Checks: []doctorCheck{
			{Name: "tool:z", Status: "warn", Detail: "optional", Hint: "install z"},
			{Name: "config", Status: "pass", Detail: "valid"},
			{Name: "tool:a", Status: "fail", Detail: "missing", Hint: "install a"},
		},
		Languages: []doctorLanguage{{LanguageID: "go", Files: 2, Server: "gopls", State: "available", VersionProbe: "ready"}},
	})

	text := output.String()
	for _, want := range []string{
		"teak doctor test-version",
		"Runtime: go1.test (test-os/test-arch)",
		"Workspace: /workspace",
		"PASS config             valid",
		"WARN tool:z             optional",
		"FAIL tool:a             missing",
		"hint: install z",
		"Summary: 1 passed, 1 warnings, 1 failures",
		"Detected languages:",
		"go files=2 server=gopls state=available version_probe=ready",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("doctor report missing %q in:\n%s", want, text)
		}
	}
	if strings.Index(text, "PASS config") > strings.Index(text, "FAIL tool:a") ||
		strings.Index(text, "FAIL tool:a") > strings.Index(text, "WARN tool:z") {
		t.Fatalf("doctor checks are not sorted by name:\n%s", text)
	}
}

func TestUnavailableToolStatusPreservesActionableHint(t *testing.T) {
	status := unavailableToolStatus(headlessToolStatus{Name: "codemap", State: "checking"}, &toolpath.MissingToolError{
		Tool: "codemap",
		Hint: "brew install codemap",
	})
	if status.State != "unavailable" || status.Detail == "" || status.Hint != "brew install codemap" {
		t.Fatalf("missing tool status = %#v", status)
	}

	status = unavailableToolStatus(headlessToolStatus{Name: "fixture", State: "checking"}, errors.New("probe failed"))
	if status.State != "unavailable" || status.Detail != "probe failed" || status.Hint != "" {
		t.Fatalf("generic tool status = %#v", status)
	}
}

func TestWriteHeadlessHealthDashboardRendersCurrentHistoryAndTrend(t *testing.T) {
	when := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	var output bytes.Buffer
	writeHeadlessHealthDashboard(&output, headlessHealthDashboardResponse{
		Workspace: "/workspace",
		State:     "degraded",
		Current: headlessHealthResponse{
			State:       "degraded",
			CollectedAt: when,
			Summary:     headlessHealthSummary{ToolsTotal: 3, ToolsReady: 2, LSPTotal: 1, LSPReady: 1, ChangedFiles: 4},
			Issues:      []string{"codemap unavailable"},
			Actions:     []headlessHealthAction{{Component: "tool", Name: "codemap", Action: "install", Hint: "brew install codemap"}},
			DurationMS:  12.5,
		},
		History: headlessHealthHistoryResponse{State: "ready", Snapshots: []headlessHealthHistorySnapshot{{State: "healthy"}, {State: "degraded"}}},
		Trend:   headlessHealthTrend{Healthy: 1, Degraded: 1, Failed: 0, Other: 0, LatestAt: when.Format(time.RFC3339Nano), LatestState: "healthy", HeapDeltaBytes: 12, DurationDeltaMS: 1.25},
	})

	text := output.String()
	for _, want := range []string{
		"Workspace: /workspace",
		"Current: degraded",
		"Tools: 2/3 ready",
		"LSP: 1/1 ready",
		"Checked: 2026-01-02T03:04:05Z",
		"History: ready (2 entries)",
		"Trend: healthy=1 degraded=1 failed=0 other=0",
		"Latest recorded: 2026-01-02T03:04:05Z state=healthy heap_delta=12B duration_delta=1.25ms",
		"issue: codemap unavailable",
		"action: tool/codemap [install] brew install codemap",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("health dashboard missing %q in:\n%s", want, text)
		}
	}
}
