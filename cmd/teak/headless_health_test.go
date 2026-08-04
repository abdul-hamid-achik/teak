package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestRunHeadlessHealthReportsBoundedProjectSnapshot(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runHeadlessHealth([]string{"--json", "--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("runHeadlessHealth() code = %d, stderr = %s", code, stderr.String())
	}
	var response headlessHealthResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode health response: %v; output = %s", err, stdout.String())
	}
	if response.Workspace != root {
		t.Fatalf("workspace = %q, want %q", response.Workspace, root)
	}
	if response.State == "" || response.DurationMS < 0 || response.CollectedAt.IsZero() {
		t.Fatalf("health state/timing/collected_at = %q/%f/%v", response.State, response.DurationMS, response.CollectedAt)
	}
	if response.Summary.ToolsTotal != len(response.Tools) || response.Summary.LSPTotal != len(response.LanguageServers) {
		t.Fatalf("summary = %#v, tools = %d, lsp = %d", response.Summary, len(response.Tools), len(response.LanguageServers))
	}
	if response.Summary.LSPTotal != 1 || response.LanguageServers[0].LanguageID != "go" || response.LanguageServers[0].DetectedFiles != 1 {
		t.Fatalf("health should report only detected language servers: %#v", response.LanguageServers)
	}
	if response.Git.State == "" || response.Metrics.HeapAllocBytes == 0 {
		t.Fatalf("git/metrics = %#v/%#v", response.Git, response.Metrics)
	}
	for _, name := range []string{"tools_ms", "lsp_ms", "git_ms", "metrics_ms"} {
		if value, ok := response.TimingsMS[name]; !ok || value < 0 {
			t.Fatalf("timing %q = %v, want non-negative value", name, value)
		}
	}
	if len(response.Issues) > headlessMaxHealthIssues {
		t.Fatalf("issues = %d, want at most %d", len(response.Issues), headlessMaxHealthIssues)
	}
}

func TestHeadlessHealthSummaryStateIsDeterministic(t *testing.T) {
	if got := aggregateHeadlessHealthState([]headlessToolStatus{{State: "ready"}}, []headlessLSPEntry{{State: "available"}}, "ready"); got != "healthy" {
		t.Fatalf("healthy state = %q", got)
	}
	if got := aggregateHeadlessHealthState([]headlessToolStatus{{State: "stale"}}, []headlessLSPEntry{{State: "available"}}, "ready"); got != "degraded" {
		t.Fatalf("stale state = %q", got)
	}
	if got := aggregateHeadlessHealthState([]headlessToolStatus{{State: "failed"}}, []headlessLSPEntry{{State: "available"}}, "ready"); got != "failed" {
		t.Fatalf("failed state = %q", got)
	}
}

func TestHeadlessJSONAddsStableSchemaVersionWithoutOverwritingExplicitVersion(t *testing.T) {
	var output bytes.Buffer
	if code := writeHeadlessJSON(&output, map[string]any{"state": "ready"}); code != 0 {
		t.Fatalf("writeHeadlessJSON() code = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode versioned response: %v; output = %s", err, output.String())
	}
	if decoded["schema_version"] != float64(headlessSchemaVersion) {
		t.Fatalf("schema_version = %#v, want %d", decoded["schema_version"], headlessSchemaVersion)
	}

	output.Reset()
	if code := writeHeadlessJSON(&output, map[string]any{"schema_version": 7, "state": "future"}); code != 0 {
		t.Fatalf("writeHeadlessJSON() explicit version code = %d", code)
	}
	decoded = nil
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode explicit version response: %v; output = %s", err, output.String())
	}
	if decoded["schema_version"] != float64(7) {
		t.Fatalf("explicit schema_version = %#v, want 7", decoded["schema_version"])
	}

	output.Reset()
	if code := writeHeadlessError(headlessErrorWriter{Writer: &output, json: true}, errors.New("invalid request")); code != 2 {
		t.Fatalf("writeHeadlessError() code = %d, want 2", code)
	}
	decoded = nil
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode versioned error response: %v; output = %s", err, output.String())
	}
	if decoded["schema_version"] != float64(headlessSchemaVersion) || decoded["state"] != "error" {
		t.Fatalf("error response = %#v, want schema version and error state", decoded)
	}
}

func TestHeadlessHealthPropagatesSharedDeadlineToToolCollectors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX process-group behavior")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "codemap")
	fixtureSource := "#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'codemap fixture' ;;\nstructural-manifest) sleep 5 ;;\n*) exit 0 ;;\nesac\n"
	if err := os.WriteFile(fixture, []byte(fixtureSource), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	previousTimeout := headlessHealthTimeout
	headlessHealthTimeout = 100 * time.Millisecond
	t.Cleanup(func() { headlessHealthTimeout = previousTimeout })

	started := time.Now()
	response := collectHeadlessHealth(root)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("health collection took %s after shared deadline, want under 1s", elapsed)
	}
	if response.State != "timed_out" {
		t.Fatalf("health state = %q, want timed_out; response = %#v", response.State, response)
	}
	toolCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	toolResponse := collectHeadlessToolStatusContext(toolCtx, root)
	cancel()
	for _, tool := range toolResponse.Tools {
		if tool.Name == "codemap" {
			if tool.State != "timed_out" {
				t.Fatalf("codemap state = %q, want timed_out", tool.State)
			}
			return
		}
	}
	t.Fatalf("tool status response did not include codemap: %#v", toolResponse.Tools)
}

func TestBuildHeadlessHealthActionsIsActionableAndBounded(t *testing.T) {
	actions := buildHeadlessHealthActions(
		[]headlessToolStatus{
			{Name: "codemap", State: "stale", Detail: "changed=2"},
			{Name: "vecgrep", State: "unsupported", Hint: "upgrade vecgrep"},
			{Name: "hitspec", State: "available"},
		},
		[]headlessLSPEntry{
			{LanguageID: "python", Command: "pylsp", State: "missing", Hint: "python -m pip install python-lsp-server", DetectedFiles: 2},
		},
		headlessHealthGit{State: "unavailable", Detail: "not a repository"},
	)
	if len(actions) != 4 {
		t.Fatalf("actions = %#v, want codemap, vecgrep, python, and git", actions)
	}
	want := []struct{ component, name, action string }{
		{"tool", "codemap", "refresh"},
		{"tool", "vecgrep", "upgrade"},
		{"lsp", "python", "install"},
		{"git", "repository", "inspect"},
	}
	for index, expected := range want {
		got := actions[index]
		if got.Component != expected.component || got.Name != expected.name || got.Action != expected.action || got.Hint == "" {
			t.Fatalf("action[%d] = %#v, want actionable %#v", index, got, expected)
		}
	}
	if got := len(buildHeadlessHealthActions(
		make([]headlessToolStatus, headlessMaxHealthActions+4), nil, headlessHealthGit{State: "ready"})); got != headlessMaxHealthActions {
		t.Fatalf("action cap = %d, want %d", got, headlessMaxHealthActions)
	}
}

func TestHeadlessHealthRecordAndHistoryAreExplicitAndBounded(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	historyPath := headlessHealthHistoryPath(root)
	var stdout, stderr bytes.Buffer
	if code := runHeadlessHealth([]string{"--json", "--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("snapshot code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(historyPath); !os.IsNotExist(err) {
		t.Fatalf("read-only snapshot created history: stat error = %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessHealth([]string{"record", "--json", "--root", root}, &stdout, &stderr); code == 0 {
		t.Fatal("health record without --confirm succeeded")
	}
	if _, err := os.Stat(historyPath); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed record created history: stat error = %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessHealth([]string{"record", "--confirm", "--json", "--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("record code = %d, stderr = %s", code, stderr.String())
	}
	var record headlessHealthRecordResponse
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("decode record response: %v; output = %s", err, stdout.String())
	}
	if record.State != "recorded" || record.Entries != 1 || record.Snapshot.State == "" || record.Path != historyPath {
		t.Fatalf("record response = %#v", record)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessHealth([]string{"history", "--limit", "1", "--json", "--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("history code = %d, stderr = %s", code, stderr.String())
	}
	var history headlessHealthHistoryResponse
	if err := json.Unmarshal(stdout.Bytes(), &history); err != nil {
		t.Fatalf("decode history response: %v; output = %s", err, stdout.String())
	}
	if history.State != "ready" || history.Limit != 1 || len(history.Snapshots) != 1 || history.Path != historyPath {
		t.Fatalf("history response = %#v", history)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessHealth([]string{"dashboard", "--limit", "1", "--json", "--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("dashboard code = %d, stderr = %s", code, stderr.String())
	}
	var dashboard headlessHealthDashboardResponse
	if err := json.Unmarshal(stdout.Bytes(), &dashboard); err != nil {
		t.Fatalf("decode dashboard response: %v; output = %s", err, stdout.String())
	}
	if dashboard.Workspace != root || dashboard.State != dashboard.Current.State || dashboard.History.Limit != 1 || len(dashboard.History.Snapshots) != 1 {
		t.Fatalf("dashboard response = %#v", dashboard)
	}
	if dashboard.Trend.Entries != 1 || dashboard.Trend.LatestState != dashboard.History.Snapshots[0].State || dashboard.Trend.LatestAt == "" {
		t.Fatalf("dashboard trend = %#v", dashboard.Trend)
	}
}

func TestSummarizeHeadlessHealthTrendCountsStatesAndDeltas(t *testing.T) {
	current := headlessHealthResponse{
		State:      "healthy",
		DurationMS: 12.5,
		Metrics:    headlessRuntimeMetrics{HeapAllocBytes: 120},
	}
	snapshots := []headlessHealthHistorySnapshot{
		{RecordedAt: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), State: "degraded", DurationMS: 10, Metrics: headlessRuntimeMetrics{HeapAllocBytes: 100}},
		{RecordedAt: time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC), State: "failed"},
		{RecordedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC), State: "unknown"},
	}
	trend := summarizeHeadlessHealthTrend(current, snapshots)
	if trend.Entries != 3 || trend.Healthy != 0 || trend.Degraded != 1 || trend.Failed != 1 || trend.Other != 1 {
		t.Fatalf("trend counts = %#v", trend)
	}
	if trend.LatestState != "degraded" || trend.PreviousState != "failed" || trend.HeapDeltaBytes != 20 || trend.DurationDeltaMS != 2.5 {
		t.Fatalf("trend deltas = %#v", trend)
	}
}
