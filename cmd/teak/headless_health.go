package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"teak/internal/atomicfile"
	"teak/internal/git"
	"teak/internal/session"
)

const (
	headlessMaxHealthIssues       = 32
	headlessMaxHealthActions      = 32
	headlessMaxHealthHistory      = 256
	headlessDefaultHistoryLimit   = 32
	headlessMaxHealthHistoryBytes = 1 << 20
)

var headlessHealthTimeout = 15 * time.Second

type headlessHealthSummary struct {
	ToolsTotal   int `json:"tools_total"`
	ToolsReady   int `json:"tools_ready"`
	LSPTotal     int `json:"lsp_total"`
	LSPReady     int `json:"lsp_ready"`
	ChangedFiles int `json:"changed_files"`
	Issues       int `json:"issues"`
	Actions      int `json:"actions"`
}

type headlessHealthAction struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Action    string `json:"action"`
	Hint      string `json:"hint,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type headlessHealthGit struct {
	State     string `json:"state"`
	Branch    string `json:"branch,omitempty"`
	Changed   int    `json:"changed"`
	Staged    int    `json:"staged"`
	Unstaged  int    `json:"unstaged"`
	Untracked int    `json:"untracked"`
	Detail    string `json:"detail,omitempty"`
}

type headlessHealthResponse struct {
	Workspace       string                 `json:"workspace"`
	State           string                 `json:"state"`
	CollectedAt     time.Time              `json:"collected_at"`
	Summary         headlessHealthSummary  `json:"summary"`
	Tools           []headlessToolStatus   `json:"tools"`
	LanguageServers []headlessLSPEntry     `json:"language_servers"`
	LanguageScan    *doctorLanguageScan    `json:"language_scan,omitempty"`
	Git             headlessHealthGit      `json:"git"`
	Metrics         headlessRuntimeMetrics `json:"metrics"`
	TimingsMS       map[string]float64     `json:"timings_ms"`
	Issues          []string               `json:"issues,omitempty"`
	Actions         []headlessHealthAction `json:"actions,omitempty"`
	DurationMS      float64                `json:"duration_ms"`
}

type headlessHealthHistorySnapshot struct {
	RecordedAt time.Time              `json:"recorded_at"`
	State      string                 `json:"state"`
	Summary    headlessHealthSummary  `json:"summary"`
	Metrics    headlessRuntimeMetrics `json:"metrics"`
	TimingsMS  map[string]float64     `json:"timings_ms"`
	DurationMS float64                `json:"duration_ms"`
}

type headlessHealthHistoryFile struct {
	Version   int                             `json:"version"`
	Workspace string                          `json:"workspace"`
	Snapshots []headlessHealthHistorySnapshot `json:"snapshots"`
}

type headlessHealthHistoryResponse struct {
	Workspace string                          `json:"workspace"`
	Path      string                          `json:"path"`
	State     string                          `json:"state"`
	Limit     int                             `json:"limit"`
	Snapshots []headlessHealthHistorySnapshot `json:"snapshots"`
	Detail    string                          `json:"detail,omitempty"`
}

type headlessHealthRecordResponse struct {
	Workspace  string                        `json:"workspace"`
	Path       string                        `json:"path"`
	State      string                        `json:"state"`
	RecordedAt time.Time                     `json:"recorded_at"`
	Entries    int                           `json:"entries"`
	Snapshot   headlessHealthHistorySnapshot `json:"snapshot"`
}

type headlessHealthTrend struct {
	Entries         int     `json:"entries"`
	Healthy         int     `json:"healthy"`
	Degraded        int     `json:"degraded"`
	Failed          int     `json:"failed"`
	Other           int     `json:"other"`
	LatestAt        string  `json:"latest_at,omitempty"`
	LatestState     string  `json:"latest_state,omitempty"`
	PreviousAt      string  `json:"previous_at,omitempty"`
	PreviousState   string  `json:"previous_state,omitempty"`
	HeapDeltaBytes  int64   `json:"heap_delta_bytes,omitempty"`
	DurationDeltaMS float64 `json:"duration_delta_ms,omitempty"`
}

type headlessHealthDashboardResponse struct {
	Workspace string                        `json:"workspace"`
	State     string                        `json:"state"`
	Current   headlessHealthResponse        `json:"current"`
	History   headlessHealthHistoryResponse `json:"history"`
	Trend     headlessHealthTrend           `json:"trend"`
}

type headlessHealthPart struct {
	name     string
	duration float64
	tools    headlessToolsResponse
	lsp      headlessLSPResponse
	lspScan  *doctorLanguageScan
	git      headlessHealthGit
	err      error
}

func runHeadlessHealth(args []string, stdout, stderr io.Writer) int {
	return runHeadlessHealthContext(context.Background(), args, stdout, stderr)
}

func runHeadlessHealthContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	operation, opts, limit, positional, help, err := parseHeadlessHealthArgs(args)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("health %s does not accept positional arguments", operation))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	switch operation {
	case "dashboard":
		if opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("health dashboard is read-only and does not accept --confirm"))
		}
		if err := ctx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		response, dashboardErr := collectHeadlessHealthDashboardContext(ctx, root, limit)
		if dashboardErr != nil {
			return writeHeadlessError(stderr, dashboardErr)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		writeHeadlessHealthDashboard(stdout, response)
		return 0
	case "history":
		if opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("health history is read-only and does not accept --confirm"))
		}
		if err := ctx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		response, historyErr := collectHeadlessHealthHistory(root, limit)
		if historyErr != nil {
			return writeHeadlessError(stderr, historyErr)
		}
		if err := ctx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		fmt.Fprintf(stdout, "Workspace: %s\nHistory: %s\nEntries: %d\n", response.Workspace, response.State, len(response.Snapshots))
		for _, snapshot := range response.Snapshots {
			fmt.Fprintf(stdout, "%s %-9s tools=%d/%d lsp=%d/%d changes=%d issues=%d actions=%d\n",
				snapshot.RecordedAt.Format(time.RFC3339), snapshot.State,
				snapshot.Summary.ToolsReady, snapshot.Summary.ToolsTotal,
				snapshot.Summary.LSPReady, snapshot.Summary.LSPTotal,
				snapshot.Summary.ChangedFiles, snapshot.Summary.Issues, snapshot.Summary.Actions)
		}
		return 0
	case "record":
		if !opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("health record requires --confirm"))
		}
		response, recordErr := recordHeadlessHealth(root)
		if recordErr != nil {
			return writeHeadlessError(stderr, recordErr)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		fmt.Fprintf(stdout, "Workspace: %s\nState: %s\nRecorded: %s\nEntries: %d\nPath: %s\n",
			response.Workspace, response.State, response.RecordedAt.Format(time.RFC3339), response.Entries, response.Path)
		return 0
	case "snapshot":
		if opts.confirm || limit != headlessDefaultHistoryLimit {
			return writeHeadlessError(stderr, fmt.Errorf("health snapshot does not accept --confirm or --limit"))
		}
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unknown health operation %q", operation))
	}
	response := collectHeadlessHealthContext(ctx, root)
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	fmt.Fprintf(stdout, "Workspace: %s\nState: %s\nTools: %d/%d ready\nLSP: %d/%d ready\nGit changes: %d\nIssues: %d\nActions: %d\nDuration: %.2fms\n",
		response.Workspace, response.State, response.Summary.ToolsReady, response.Summary.ToolsTotal,
		response.Summary.LSPReady, response.Summary.LSPTotal, response.Summary.ChangedFiles,
		len(response.Issues), len(response.Actions), response.DurationMS)
	for _, issue := range response.Issues {
		fmt.Fprintf(stdout, "issue: %s\n", issue)
	}
	for _, action := range response.Actions {
		fmt.Fprintf(stdout, "action: %s/%s [%s] %s\n", action.Component, action.Name, action.Action, action.Hint)
	}
	return 0
}

func collectHeadlessHealthDashboardContext(ctx context.Context, root string, limit int) (headlessHealthDashboardResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return headlessHealthDashboardResponse{}, err
	}
	current := collectHeadlessHealthContext(ctx, root)
	if err := ctx.Err(); err != nil {
		return headlessHealthDashboardResponse{}, err
	}
	history, err := collectHeadlessHealthHistory(root, limit)
	if err != nil {
		return headlessHealthDashboardResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return headlessHealthDashboardResponse{}, err
	}
	return headlessHealthDashboardResponse{
		Workspace: root,
		State:     current.State,
		Current:   current,
		History:   history,
		Trend:     summarizeHeadlessHealthTrend(current, history.Snapshots),
	}, nil
}

func summarizeHeadlessHealthTrend(current headlessHealthResponse, snapshots []headlessHealthHistorySnapshot) headlessHealthTrend {
	trend := headlessHealthTrend{Entries: len(snapshots)}
	for _, snapshot := range snapshots {
		switch snapshot.State {
		case "healthy":
			trend.Healthy++
		case "degraded":
			trend.Degraded++
		case "failed":
			trend.Failed++
		default:
			trend.Other++
		}
	}
	if len(snapshots) == 0 {
		return trend
	}
	latest := snapshots[0]
	trend.LatestAt = latest.RecordedAt.UTC().Format(time.RFC3339Nano)
	trend.LatestState = latest.State
	trend.HeapDeltaBytes = int64(current.Metrics.HeapAllocBytes) - int64(latest.Metrics.HeapAllocBytes)
	trend.DurationDeltaMS = current.DurationMS - latest.DurationMS
	if len(snapshots) > 1 {
		previous := snapshots[1]
		trend.PreviousAt = previous.RecordedAt.UTC().Format(time.RFC3339Nano)
		trend.PreviousState = previous.State
	}
	return trend
}

func writeHeadlessHealthDashboard(stdout io.Writer, response headlessHealthDashboardResponse) {
	fmt.Fprintf(stdout, "Workspace: %s\nCurrent: %s\nTools: %d/%d ready\nLSP: %d/%d ready\nGit changes: %d\nIssues: %d\nActions: %d\nDuration: %.2fms\n",
		response.Workspace, response.State, response.Current.Summary.ToolsReady, response.Current.Summary.ToolsTotal,
		response.Current.Summary.LSPReady, response.Current.Summary.LSPTotal, response.Current.Summary.ChangedFiles,
		len(response.Current.Issues), len(response.Current.Actions), response.Current.DurationMS)
	if !response.Current.CollectedAt.IsZero() {
		fmt.Fprintf(stdout, "Checked: %s\n", response.Current.CollectedAt.UTC().Format(time.RFC3339Nano))
	}
	fmt.Fprintf(stdout, "History: %s (%d entries)\nTrend: healthy=%d degraded=%d failed=%d other=%d\n",
		response.History.State, len(response.History.Snapshots), response.Trend.Healthy, response.Trend.Degraded,
		response.Trend.Failed, response.Trend.Other)
	if response.Trend.LatestAt != "" {
		fmt.Fprintf(stdout, "Latest recorded: %s state=%s heap_delta=%dB duration_delta=%.2fms\n",
			response.Trend.LatestAt, response.Trend.LatestState, response.Trend.HeapDeltaBytes, response.Trend.DurationDeltaMS)
	}
	for _, issue := range response.Current.Issues {
		fmt.Fprintf(stdout, "issue: %s\n", issue)
	}
	for _, action := range response.Current.Actions {
		fmt.Fprintf(stdout, "action: %s/%s [%s] %s\n", action.Component, action.Name, action.Action, action.Hint)
	}
}

func parseHeadlessHealthArgs(args []string) (string, headlessOptions, int, []string, bool, error) {
	operation := "snapshot"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		operation = args[0]
		args = args[1:]
	}
	limit := headlessDefaultHistoryLimit
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != "--limit" {
			filtered = append(filtered, args[i])
			continue
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			return "", headlessOptions{}, 0, nil, false, fmt.Errorf("--limit requires a value")
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
		if parseErr != nil || parsed < 1 || parsed > headlessMaxHealthHistory {
			return "", headlessOptions{}, 0, nil, false, fmt.Errorf("--limit must be an integer between 1 and %d", headlessMaxHealthHistory)
		}
		limit = parsed
		i++
	}
	opts, positional, help, err := parseHeadlessArgs(filtered)
	return operation, opts, limit, positional, help, err
}

func headlessHealthHistoryPath(root string) string {
	return filepath.Join(filepath.Dir(session.PathForRoot(root)), "health-history.json")
}

func snapshotHeadlessHealth(response headlessHealthResponse, recordedAt time.Time) headlessHealthHistorySnapshot {
	timings := make(map[string]float64, len(response.TimingsMS))
	for key, value := range response.TimingsMS {
		timings[key] = value
	}
	return headlessHealthHistorySnapshot{
		RecordedAt: recordedAt.UTC(),
		State:      response.State,
		Summary:    response.Summary,
		Metrics:    response.Metrics,
		TimingsMS:  timings,
		DurationMS: response.DurationMS,
	}
}

func collectHeadlessHealthHistory(root string, limit int) (headlessHealthHistoryResponse, error) {
	if limit < 1 || limit > headlessMaxHealthHistory {
		return headlessHealthHistoryResponse{}, fmt.Errorf("health history limit must be between 1 and %d", headlessMaxHealthHistory)
	}
	path := headlessHealthHistoryPath(root)
	history, err := loadHeadlessHealthHistory(path, root)
	if err != nil {
		return headlessHealthHistoryResponse{}, err
	}
	response := headlessHealthHistoryResponse{
		Workspace: root,
		Path:      path,
		State:     "ready",
		Limit:     limit,
		Snapshots: make([]headlessHealthHistorySnapshot, 0, minInt(limit, len(history.Snapshots))),
	}
	if len(history.Snapshots) == 0 {
		response.State = "empty"
		return response, nil
	}
	for index := len(history.Snapshots) - 1; index >= 0 && len(response.Snapshots) < limit; index-- {
		response.Snapshots = append(response.Snapshots, history.Snapshots[index])
	}
	return response, nil
}

func recordHeadlessHealth(root string) (headlessHealthRecordResponse, error) {
	response := collectHeadlessHealth(root)
	recordedAt := time.Now().UTC()
	snapshot := snapshotHeadlessHealth(response, recordedAt)
	path := headlessHealthHistoryPath(root)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return headlessHealthRecordResponse{}, fmt.Errorf("create health history directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return headlessHealthRecordResponse{}, fmt.Errorf("inspect health history directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return headlessHealthRecordResponse{}, fmt.Errorf("health history directory is not a real directory")
	}
	lock, err := acquireHeadlessWriteLock(root, directory)
	if err != nil {
		return headlessHealthRecordResponse{}, fmt.Errorf("lock health history: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	history, err := loadHeadlessHealthHistory(path, root)
	if err != nil {
		return headlessHealthRecordResponse{}, err
	}
	history.Version = 1
	history.Workspace = root
	history.Snapshots = append(history.Snapshots, snapshot)
	if len(history.Snapshots) > headlessMaxHealthHistory {
		history.Snapshots = append([]headlessHealthHistorySnapshot(nil), history.Snapshots[len(history.Snapshots)-headlessMaxHealthHistory:]...)
	}
	encoded, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return headlessHealthRecordResponse{}, fmt.Errorf("encode health history: %w", err)
	}
	if len(encoded) > headlessMaxHealthHistoryBytes {
		return headlessHealthRecordResponse{}, fmt.Errorf("health history exceeds %d bytes", headlessMaxHealthHistoryBytes)
	}
	if err := atomicfile.Write(path, func(file *os.File) error {
		_, writeErr := file.Write(encoded)
		return writeErr
	}); err != nil {
		return headlessHealthRecordResponse{}, fmt.Errorf("write health history: %w", err)
	}
	return headlessHealthRecordResponse{
		Workspace:  root,
		Path:       path,
		State:      "recorded",
		RecordedAt: recordedAt,
		Entries:    len(history.Snapshots),
		Snapshot:   snapshot,
	}, nil
}

func loadHeadlessHealthHistory(path, root string) (headlessHealthHistoryFile, error) {
	history := headlessHealthHistoryFile{Version: 1, Workspace: root, Snapshots: []headlessHealthHistorySnapshot{}}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return history, nil
	}
	if err != nil {
		return headlessHealthHistoryFile{}, fmt.Errorf("inspect health history: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return headlessHealthHistoryFile{}, fmt.Errorf("health history is not a regular file")
	}
	if info.Size() > headlessMaxHealthHistoryBytes {
		return headlessHealthHistoryFile{}, fmt.Errorf("health history exceeds %d bytes", headlessMaxHealthHistoryBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return headlessHealthHistoryFile{}, fmt.Errorf("open health history: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, headlessMaxHealthHistoryBytes+1))
	if err := decoder.Decode(&history); err != nil {
		return headlessHealthHistoryFile{}, fmt.Errorf("decode health history: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return headlessHealthHistoryFile{}, fmt.Errorf("decode health history: trailing JSON value")
		}
		return headlessHealthHistoryFile{}, fmt.Errorf("decode health history: trailing data: %w", err)
	}
	if history.Version != 1 {
		return headlessHealthHistoryFile{}, fmt.Errorf("unsupported health history version %d", history.Version)
	}
	if history.Workspace != "" && filepath.Clean(history.Workspace) != filepath.Clean(root) {
		return headlessHealthHistoryFile{}, fmt.Errorf("health history belongs to a different workspace")
	}
	if len(history.Snapshots) > headlessMaxHealthHistory {
		return headlessHealthHistoryFile{}, fmt.Errorf("health history contains more than %d entries", headlessMaxHealthHistory)
	}
	history.Workspace = root
	if history.Snapshots == nil {
		history.Snapshots = []headlessHealthHistorySnapshot{}
	}
	return history, nil
}

func collectHeadlessHealth(root string) headlessHealthResponse {
	return collectHeadlessHealthContext(context.Background(), root)
}

func collectHeadlessHealthContext(parentCtx context.Context, root string) headlessHealthResponse {
	started := time.Now()
	response := headlessHealthResponse{
		Workspace:       root,
		State:           "failed",
		Tools:           make([]headlessToolStatus, 0),
		LanguageServers: make([]headlessLSPEntry, 0),
		TimingsMS:       make(map[string]float64, 4),
		Issues:          make([]string, 0),
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, headlessHealthTimeout)
	defer cancel()

	parts := make(chan headlessHealthPart, 3)
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		partStarted := time.Now()
		toolsResponse := collectHeadlessToolStatusContext(ctx, root)
		parts <- headlessHealthPart{
			name:     "tools_ms",
			duration: elapsedHeadlessMS(partStarted),
			tools:    toolsResponse,
		}
	}()
	go func() {
		defer wait.Done()
		partStarted := time.Now()
		lspResponse, scan, err := collectHeadlessHealthLSPStatusContext(ctx, root)
		parts <- headlessHealthPart{
			name:     "lsp_ms",
			duration: elapsedHeadlessMS(partStarted),
			lsp:      lspResponse,
			lspScan:  scan,
			err:      err,
		}
	}()
	go func() {
		defer wait.Done()
		partStarted := time.Now()
		gitResponse := collectHeadlessHealthGit(ctx, root)
		parts <- headlessHealthPart{
			name:     "git_ms",
			duration: elapsedHeadlessMS(partStarted),
			git:      gitResponse,
		}
	}()
	go func() {
		wait.Wait()
		close(parts)
	}()

	received := 0
	for received < 3 {
		select {
		case part, ok := <-parts:
			if !ok {
				received = 3
				continue
			}
			received++
			response.TimingsMS[part.name] = part.duration
			switch part.name {
			case "tools_ms":
				response.Tools = part.tools.Tools
			case "lsp_ms":
				response.LanguageServers = part.lsp.Servers
				response.LanguageScan = part.lspScan
				if part.err != nil {
					response.Issues = appendHealthIssue(response.Issues, "lsp: "+part.err.Error())
				}
			case "git_ms":
				response.Git = part.git
			}
		case <-ctx.Done():
			response.Issues = appendHealthIssue(response.Issues, headlessHealthContextDetail(ctx.Err()))
			received = 3
		}
	}
	metricsStarted := time.Now()
	response.Metrics = collectHeadlessRuntimeMetrics(ctx)
	response.TimingsMS["metrics_ms"] = elapsedHeadlessMS(metricsStarted)
	response.Summary = summarizeHeadlessHealth(response)
	if err := ctx.Err(); err != nil {
		response.State = headlessHealthContextState(err)
	} else {
		response.Issues = appendHeadlessHealthComponentIssues(response.Issues, response.Tools, response.LanguageServers, response.Git)
		response.Actions = buildHeadlessHealthActions(response.Tools, response.LanguageServers, response.Git)
		response.Summary = summarizeHeadlessHealth(response)
		response.State = aggregateHeadlessHealthState(response.Tools, response.LanguageServers, response.Git.State)
	}
	if response.LanguageScan != nil && response.LanguageScan.Truncated && response.State == "healthy" {
		response.State = "degraded"
		response.Issues = appendHealthIssue(response.Issues, fmt.Sprintf("lsp: language scan truncated after %d files (%dms)", response.LanguageScan.ScannedFiles, response.LanguageScan.DurationMS))
	}
	if len(response.Issues) > 0 && response.State == "healthy" {
		response.State = "failed"
	}
	response.Summary.Issues = len(response.Issues)
	response.DurationMS = elapsedHeadlessMS(started)
	response.CollectedAt = time.Now().UTC()
	return response
}

func headlessHealthContextState(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "timed_out"
}

func headlessHealthContextDetail(err error) string {
	if errors.Is(err, context.Canceled) {
		return "health: collection was cancelled"
	}
	return "health: collection deadline exceeded"
}

func collectHeadlessHealthGit(ctx context.Context, root string) headlessHealthGit {
	response := headlessHealthGit{State: "failed"}
	snapshot, err := git.StatusContext(ctx, root)
	if err != nil {
		response.State = "unavailable"
		response.Detail = err.Error()
		return response
	}
	response.State = "ready"
	response.Branch = snapshot.Branch
	for _, entry := range snapshot.Entries {
		response.Changed++
		if entry.IsStagedChange() {
			response.Staged++
		}
		if entry.IsUnstagedChange() {
			response.Unstaged++
		}
		if entry.IsUntracked() {
			response.Untracked++
		}
	}
	return response
}

func summarizeHeadlessHealth(response headlessHealthResponse) headlessHealthSummary {
	summary := headlessHealthSummary{
		ToolsTotal:   len(response.Tools),
		LSPTotal:     len(response.LanguageServers),
		ChangedFiles: response.Git.Changed,
		Actions:      len(response.Actions),
	}
	for _, tool := range response.Tools {
		if tool.Ready || tool.State == "available" {
			summary.ToolsReady++
		}
	}
	for _, server := range response.LanguageServers {
		if server.Ready || server.State == "available" {
			summary.LSPReady++
		}
	}
	return summary
}

func buildHeadlessHealthActions(tools []headlessToolStatus, servers []headlessLSPEntry, gitStatus headlessHealthGit) []headlessHealthAction {
	actions := make([]headlessHealthAction, 0)
	for _, tool := range tools {
		if headlessHealthComponentReady(tool.State) {
			continue
		}
		action, hint := classifyHeadlessHealthAction(tool.Name, tool.State, tool.Hint)
		actions = appendHealthAction(actions, headlessHealthAction{
			Component: "tool",
			Name:      tool.Name,
			State:     tool.State,
			Action:    action,
			Hint:      hint,
			Detail:    tool.Detail,
		})
	}
	for _, server := range servers {
		if headlessHealthComponentReady(server.State) {
			continue
		}
		action, hint := classifyHeadlessHealthAction(server.Command, server.State, server.Hint)
		actions = appendHealthAction(actions, headlessHealthAction{
			Component: "lsp",
			Name:      server.LanguageID,
			State:     server.State,
			Action:    action,
			Hint:      hint,
			Detail:    fmt.Sprintf("%s (%d detected files)", server.Command, server.DetectedFiles),
		})
	}
	if gitStatus.State != "ready" && gitStatus.State != "healthy" {
		actions = appendHealthAction(actions, headlessHealthAction{
			Component: "git",
			Name:      "repository",
			State:     gitStatus.State,
			Action:    "inspect",
			Hint:      "verify repository access and run git status",
			Detail:    gitStatus.Detail,
		})
	}
	return actions
}

func headlessHealthComponentReady(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ready", "available", "healthy":
		return true
	default:
		return false
	}
}

func classifyHeadlessHealthAction(name, state, hint string) (string, string) {
	state = strings.ToLower(strings.TrimSpace(state))
	if strings.TrimSpace(hint) != "" {
		switch state {
		case "unavailable", "missing":
			return "install", hint
		case "unsupported":
			return "upgrade", hint
		case "stale", "unknown", "uninitialized":
			return "refresh", hint
		default:
			return "repair", hint
		}
	}
	switch state {
	case "stale", "unknown", "uninitialized":
		switch name {
		case "codemap":
			return "refresh", "run codemap index explicitly for this workspace"
		case "vecgrep":
			return "refresh", "run vecgrep init/index explicitly for this workspace"
		default:
			return "refresh", "refresh the project index explicitly; health checks remain read-only"
		}
	case "unavailable", "missing":
		return "install", "install the capability and make its executable available on PATH"
	case "unsupported":
		return "upgrade", "upgrade the executable to a release with the required bounded health contract"
	default:
		return "repair", "run teak doctor --json and repair the reported capability before retrying"
	}
}

func appendHealthAction(actions []headlessHealthAction, action headlessHealthAction) []headlessHealthAction {
	if len(actions) >= headlessMaxHealthActions {
		return actions
	}
	action.Hint = truncateHeadlessHealthText(action.Hint)
	action.Detail = truncateHeadlessHealthText(action.Detail)
	return append(actions, action)
}

func aggregateHeadlessHealthState(tools []headlessToolStatus, servers []headlessLSPEntry, gitState string) string {
	degraded := false
	for _, tool := range tools {
		switch strings.ToLower(tool.State) {
		case "failed", "error":
			return "failed"
		case "ready", "available", "healthy":
		default:
			degraded = true
		}
	}
	for _, server := range servers {
		switch strings.ToLower(server.State) {
		case "failed", "error":
			return "failed"
		case "ready", "available", "healthy":
		default:
			degraded = true
		}
	}
	if gitState != "ready" && gitState != "healthy" {
		degraded = true
	}
	if degraded {
		return "degraded"
	}
	return "healthy"
}

func appendHealthIssue(issues []string, issue string) []string {
	if len(issues) >= headlessMaxHealthIssues {
		return issues
	}
	return append(issues, truncateHeadlessHealthText(issue))
}

func truncateHeadlessHealthText(value string) string {
	if len(value) <= 512 {
		return value
	}
	limit := 512 - len("…")
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "…"
}

func appendHeadlessHealthComponentIssues(issues []string, tools []headlessToolStatus, servers []headlessLSPEntry, gitStatus headlessHealthGit) []string {
	for _, tool := range tools {
		switch strings.ToLower(tool.State) {
		case "ready", "available", "healthy":
		default:
			detail := tool.Detail
			if detail == "" {
				detail = tool.Hint
			}
			issues = appendHealthIssue(issues, fmt.Sprintf("tool %s: %s (%s)", tool.Name, tool.State, detail))
		}
	}
	for _, server := range servers {
		switch strings.ToLower(server.State) {
		case "ready", "available", "healthy":
		default:
			detail := server.Hint
			issues = appendHealthIssue(issues, fmt.Sprintf("lsp %s: %s (%s)", server.Command, server.State, detail))
		}
	}
	if gitStatus.State != "ready" && gitStatus.State != "healthy" {
		issues = appendHealthIssue(issues, fmt.Sprintf("git: %s (%s)", gitStatus.State, gitStatus.Detail))
	}
	return issues
}
