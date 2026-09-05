package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/overlay"
	"teak/internal/toolpath"
	"teak/internal/ui"
)

const (
	healthDashboardHistoryLimit = 8
	healthDashboardOutputLimit  = 128 << 10
	healthDashboardErrorLimit   = 16 << 10
	healthDashboardMaxLines     = 32
	healthDashboardMaxLineBytes = 240
)

type healthDashboardRunner func(context.Context, string) (string, error)

type healthDashboardResultMsg struct {
	Generation uint64
	Content    string
	Err        error
}

type healthDashboardCloseMsg struct{}

// healthDashboardOverlay is intentionally read-only. It is separate from the
// plugin float so dismissing it cannot alter plugin-owned state or callbacks.
type healthDashboardOverlay struct {
	content   string
	width     int
	height    int
	dismissed bool
	theme     ui.Theme
}

func newHealthDashboardOverlay(width, height int) *healthDashboardOverlay {
	dashboard := &healthDashboardOverlay{theme: ui.DefaultTheme()}
	dashboard.SetSize(width, height)
	dashboard.SetContent("Loading workspace health…")
	return dashboard
}

func (o *healthDashboardOverlay) SetTheme(theme ui.Theme) { o.theme = theme }

func (o *healthDashboardOverlay) SetSize(width, height int) {
	o.width = max(1, width-4)
	o.height = max(1, height-4)
}

func (o *healthDashboardOverlay) SetContent(content string) {
	o.content = truncateHealthDashboardBytes(content, healthDashboardOutputLimit)
}

func (o *healthDashboardOverlay) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "esc", "escape", "enter", "q":
		o.dismissed = true
		return o, func() tea.Msg { return healthDashboardCloseMsg{} }
	default:
		return o, nil
	}
}

func (o *healthDashboardOverlay) View() string {
	title := o.theme.HelpTitle.Render("Workspace health")
	hint := o.theme.Gutter.Render("Enter, Esc, or q to close")
	content := o.content
	if content == "" {
		content = "(no health data)"
	}
	lines := strings.Split(content, "\n")
	maxContentLines := max(1, o.height-6)
	if len(lines) > maxContentLines {
		lines = append(lines[:maxContentLines-1], "… output truncated …")
	}
	lineLimit := max(16, o.width-8)
	for i, line := range lines {
		lines[i] = truncateHealthDashboardBytes(line, lineLimit)
	}
	body := o.theme.PromptMuted.Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(o.theme.TreeBorder.GetForeground()).
		Background(o.theme.StatusBar.GetBackground()).
		Padding(1, 2).
		Width(max(1, o.width)).
		Render(title + "\n\n" + body + "\n\n" + hint)
}

func (o *healthDashboardOverlay) IsDismissed() bool { return o.dismissed }

func (o *healthDashboardOverlay) CapturesInput() bool { return true }

type healthDashboardJSON struct {
	State   string `json:"state"`
	Current struct {
		State       string `json:"state"`
		CollectedAt string `json:"collected_at"`
		Summary     struct {
			ToolsTotal   int `json:"tools_total"`
			ToolsReady   int `json:"tools_ready"`
			LSPTotal     int `json:"lsp_total"`
			LSPReady     int `json:"lsp_ready"`
			ChangedFiles int `json:"changed_files"`
			Issues       int `json:"issues"`
			Actions      int `json:"actions"`
		} `json:"summary"`
		Issues  []string `json:"issues"`
		Actions []struct {
			Component string `json:"component"`
			Name      string `json:"name"`
			Action    string `json:"action"`
			Hint      string `json:"hint"`
		} `json:"actions"`
		DurationMS float64 `json:"duration_ms"`
	} `json:"current"`
	History struct {
		State     string `json:"state"`
		Snapshots []struct {
			State string `json:"state"`
		} `json:"snapshots"`
	} `json:"history"`
	Trend struct {
		Entries         int     `json:"entries"`
		Healthy         int     `json:"healthy"`
		Degraded        int     `json:"degraded"`
		Failed          int     `json:"failed"`
		HeapDeltaBytes  int64   `json:"heap_delta_bytes"`
		DurationDeltaMS float64 `json:"duration_delta_ms"`
	} `json:"trend"`
}

func formatHealthDashboardJSON(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("health dashboard returned an empty response")
	}
	if len(data) > healthDashboardOutputLimit {
		return "", fmt.Errorf("health dashboard response exceeds %d bytes", healthDashboardOutputLimit)
	}
	var response healthDashboardJSON
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(data), healthDashboardOutputLimit+1))
	if err := decoder.Decode(&response); err != nil {
		return "", fmt.Errorf("decode health dashboard: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", errors.New("decode health dashboard: trailing JSON value")
		}
		return "", fmt.Errorf("decode health dashboard: trailing data: %w", err)
	}

	state := response.State
	if state == "" {
		state = response.Current.State
	}
	lines := []string{
		fmt.Sprintf("State: %s", stateOrUnknown(state)),
		fmt.Sprintf("Tools: %d/%d ready   LSP: %d/%d ready",
			response.Current.Summary.ToolsReady, response.Current.Summary.ToolsTotal,
			response.Current.Summary.LSPReady, response.Current.Summary.LSPTotal),
		fmt.Sprintf("Git changes: %d   Issues: %d   Actions: %d",
			response.Current.Summary.ChangedFiles, response.Current.Summary.Issues, response.Current.Summary.Actions),
		fmt.Sprintf("Current check: %.2fms", response.Current.DurationMS),
		fmt.Sprintf("History: %s (%d entries)", response.History.State, len(response.History.Snapshots)),
		fmt.Sprintf("Trend: healthy=%d degraded=%d failed=%d",
			response.Trend.Healthy, response.Trend.Degraded, response.Trend.Failed),
		fmt.Sprintf("Runtime delta: heap=%dB duration=%.2fms",
			response.Trend.HeapDeltaBytes, response.Trend.DurationDeltaMS),
	}
	if strings.TrimSpace(response.Current.CollectedAt) != "" {
		lines = append(lines, "Checked: "+truncateHealthDashboardLine(response.Current.CollectedAt))
	}
	for _, issue := range response.Current.Issues {
		lines = append(lines, "issue: "+truncateHealthDashboardLine(issue))
		if len(lines) >= healthDashboardMaxLines {
			break
		}
	}
	for _, action := range response.Current.Actions {
		if len(lines) >= healthDashboardMaxLines {
			break
		}
		detail := action.Hint
		if detail == "" {
			detail = action.Action
		}
		lines = append(lines, fmt.Sprintf("action: %s/%s — %s", action.Component, action.Name, truncateHealthDashboardLine(detail)))
	}
	return strings.Join(lines, "\n"), nil
}

func stateOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func truncateHealthDashboardLine(value string) string {
	return truncateHealthDashboardBytes(strings.TrimSpace(value), healthDashboardMaxLineBytes)
}

func truncateHealthDashboardBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := max(0, limit-len("…"))
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…"
}

func runHealthDashboardCommand(ctx context.Context, root string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve Teak executable: %w", err)
	}
	cmd, err := toolpath.Command(ctx, executable, "headless", "health", "dashboard",
		"--limit", fmt.Sprint(healthDashboardHistoryLimit), "--json", "--root", root)
	if err != nil {
		return "", fmt.Errorf("resolve Teak health command: %w", err)
	}
	cmd.Dir = root
	stdout, stderr, runErr := toolpath.RunBounded(cmd, healthDashboardOutputLimit, healthDashboardErrorLimit)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if runErr != nil {
		detail := strings.TrimSpace(string(stderr))
		if detail != "" {
			return "", fmt.Errorf("health dashboard: %s", truncateHealthDashboardLine(detail))
		}
		return "", fmt.Errorf("health dashboard: %w", runErr)
	}
	return formatHealthDashboardJSON(stdout)
}

func (m Model) cancelHealthDashboard() {
	if m.healthDashboardCancel != nil {
		m.healthDashboardCancel()
		m.healthDashboardCancel = nil
	}
}

func (m Model) openHealthDashboard() (tea.Model, tea.Cmd) {
	if m.healthDashboard != nil {
		m.cancelHealthDashboard()
		m.overlayStack.Remove(m.healthDashboard)
		m.healthDashboard = nil
	}
	m.healthDashboardGeneration++
	generation := m.healthDashboardGeneration
	dashboard := newHealthDashboardOverlay(m.width, m.height)
	dashboard.SetTheme(m.theme)
	m.healthDashboard = dashboard
	m.overlayStack.Push(dashboard)
	m.status = "Loading workspace health…"
	ctx, cancel := context.WithCancel(context.Background())
	m.healthDashboardCancel = cancel
	runner := m.healthDashboardRunner
	if runner == nil {
		runner = runHealthDashboardCommand
	}
	root := m.rootDir
	return m, func() tea.Msg {
		content, err := runner(ctx, root)
		return healthDashboardResultMsg{Generation: generation, Content: content, Err: err}
	}
}

func (m Model) handleHealthDashboardResult(msg healthDashboardResultMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.healthDashboardGeneration || m.healthDashboard == nil {
		return m, nil
	}
	m.healthDashboardCancel = nil
	if msg.Err != nil {
		if !errors.Is(msg.Err, context.Canceled) {
			m.healthDashboard.SetContent("Unable to load workspace health:\n" + truncateHealthDashboardLine(msg.Err.Error()))
			m.status = "Workspace health unavailable"
		}
		return m, nil
	}
	m.healthDashboard.SetContent(msg.Content)
	m.status = "Workspace health ready"
	return m, nil
}

func (m Model) handleHealthDashboardClose() (tea.Model, tea.Cmd) {
	m.cancelHealthDashboard()
	m.healthDashboard = nil
	return m, nil
}
