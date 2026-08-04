package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormatHealthDashboardJSONProducesBoundedSummary(t *testing.T) {
	data := []byte(`{
  "state": "degraded",
  "current": {
    "state": "degraded",
    "collected_at": "2026-08-03T02:15:00Z",
    "summary": {"tools_ready": 1, "tools_total": 3, "lsp_ready": 2, "lsp_total": 4, "changed_files": 7, "issues": 1, "actions": 1},
    "issues": ["tool vecgrep: unsupported"],
    "actions": [{"component":"tool","name":"vecgrep","action":"upgrade","hint":"upgrade vecgrep"}],
    "duration_ms": 12.5
  },
  "history": {"state":"ready", "snapshots":[{"state":"degraded"}]},
  "trend": {"entries":1,"healthy":0,"degraded":1,"failed":0,"heap_delta_bytes":128,"duration_delta_ms":2.5}
}`)

	got, err := formatHealthDashboardJSON(data)
	if err != nil {
		t.Fatalf("formatHealthDashboardJSON() error = %v", err)
	}
	for _, want := range []string{
		"State: degraded",
		"Checked: 2026-08-03T02:15:00Z",
		"Tools: 1/3 ready   LSP: 2/4 ready",
		"Git changes: 7   Issues: 1   Actions: 1",
		"History: ready (1 entries)",
		"Trend: healthy=0 degraded=1 failed=0",
		"Runtime delta: heap=128B duration=2.50ms",
		"issue: tool vecgrep: unsupported",
		"action: tool/vecgrep — upgrade vecgrep",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted dashboard = %q, missing %q", got, want)
		}
	}
	if lines := strings.Count(got, "\n") + 1; lines > healthDashboardMaxLines {
		t.Fatalf("formatted lines = %d, want at most %d", lines, healthDashboardMaxLines)
	}
}

func TestFormatHealthDashboardJSONRejectsMalformedOrTrailingData(t *testing.T) {
	for _, data := range []string{"", `{`, `{} {}`} {
		if _, err := formatHealthDashboardJSON([]byte(data)); err == nil {
			t.Fatalf("formatHealthDashboardJSON(%q) succeeded", data)
		}
	}
	if _, err := formatHealthDashboardJSON([]byte(strings.Repeat("x", healthDashboardOutputLimit+1))); err == nil {
		t.Fatal("oversized dashboard response succeeded")
	}
}

func TestHealthDashboardLoadsOutsideUpdateAndIgnoresStaleResults(t *testing.T) {
	root := t.TempDir()
	called := 0
	m := testModel(modelState{
		rootDir: root,
		width:   100,
		height:  30,
	})
	m.healthDashboardRunner = func(ctx context.Context, gotRoot string) (string, error) {
		called++
		if gotRoot != root {
			t.Fatalf("runner root = %q, want %q", gotRoot, root)
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "State: healthy", nil
	}

	firstAny, firstCmd := m.openHealthDashboard()
	first := firstAny.(Model)
	if firstCmd == nil || first.healthDashboard == nil || first.overlayStack.IsEmpty() {
		t.Fatal("opening dashboard did not install an asynchronous overlay")
	}
	firstGeneration := first.healthDashboardGeneration

	secondAny, secondCmd := first.openHealthDashboard()
	second := secondAny.(Model)
	if second.healthDashboardGeneration == firstGeneration || secondCmd == nil {
		t.Fatal("opening a second dashboard did not advance generation")
	}
	stale := firstCmd().(healthDashboardResultMsg)
	staleAppliedAny, _ := second.Update(stale)
	staleApplied := staleAppliedAny.(Model)
	if staleApplied.healthDashboard.content != "Loading workspace health…" {
		t.Fatalf("stale result changed dashboard content = %q", staleApplied.healthDashboard.content)
	}

	current := secondCmd().(healthDashboardResultMsg)
	readyAny, _ := staleApplied.Update(current)
	ready := readyAny.(Model)
	if called != 2 || ready.healthDashboard.content != "State: healthy" || ready.status != "Workspace health ready" {
		t.Fatalf("dashboard result state called=%d content=%q status=%q", called, ready.healthDashboard.content, ready.status)
	}

	keyAny, closeCmd, handled := ready.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled || closeCmd == nil {
		t.Fatal("dashboard escape did not dismiss the overlay")
	}
	closedAny, _ := keyAny.Update(closeCmd())
	closed := closedAny.(Model)
	if closed.healthDashboard != nil || !closed.overlayStack.IsEmpty() {
		t.Fatalf("dashboard close state = overlay=%v stack=%d", closed.healthDashboard != nil, closed.overlayStack.Len())
	}
}

func TestHealthDashboardOverlayClipsContentAndCancellation(t *testing.T) {
	overlay := newHealthDashboardOverlay(24, 10)
	overlay.SetContent(strings.Repeat("long line\n", healthDashboardMaxLines+10))
	view := ansi.Strip(overlay.View())
	if !strings.Contains(view, "output tr") {
		t.Fatalf("overlay view did not advertise truncation: %q", view)
	}
	if got := strings.Count(view, "\n") + 1; got > 16 {
		t.Fatalf("overlay rendered %d lines in a tiny viewport", got)
	}

	m := testModel(modelState{rootDir: t.TempDir(), width: 80, height: 20})
	cancelled := false
	m.healthDashboardCancel = func() { cancelled = true }
	m.healthDashboard = overlay
	updated, _ := m.handleHealthDashboardClose()
	if updated.(Model).healthDashboard != nil || !cancelled {
		t.Fatalf("dashboard close did not cancel command: %#v", updated)
	}
}
