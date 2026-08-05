package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

func branchPickerForMouse(t *testing.T, width, height int, branches []string, current string) BranchPickerModel {
	t.Helper()
	m := NewBranchPicker(ui.DefaultTheme())
	m.SetSize(width, height)
	m.SetBranches(branches, current)
	return m
}

func branchPickerRowClick(m BranchPickerModel, row int) tea.MouseClickMsg {
	geometry := m.geometry()
	return tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      geometry.listX,
		Y:      geometry.listY + row,
	})
}

func TestBranchPickerMouseClickSelectsAndSwitchesVisibleBranch(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature/caf\u00e9", "release"}, "main")

	updated, cmd := m.Update(branchPickerRowClick(m, 1))
	if updated.cursor != 1 {
		t.Fatalf("cursor = %d, want clicked filtered row 1", updated.cursor)
	}
	if cmd == nil {
		t.Fatal("clicking a non-current visible branch returned no command")
	}
	msg := cmd()
	switchMsg, ok := msg.(SwitchBranchMsg)
	if !ok {
		t.Fatalf("click command = %T, want SwitchBranchMsg", msg)
	}
	if switchMsg.Branch != "feature/caf\u00e9" {
		t.Errorf("branch = %q, want Unicode branch name", switchMsg.Branch)
	}
}

func TestBranchPickerMouseCurrentBranchAndOutsideDoNotSwitch(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature"}, "main")

	updated, cmd := m.Update(branchPickerRowClick(m, 0))
	if updated.cursor != 0 {
		t.Errorf("current-branch click moved cursor to %d, want 0", updated.cursor)
	}
	if cmd != nil {
		t.Fatal("clicking the current branch must not start a redundant switch")
	}

	geometry := m.geometry()
	for _, mouse := range []tea.Mouse{
		{Button: tea.MouseLeft, X: geometry.x - 1, Y: geometry.listY},
		{Button: tea.MouseLeft, X: geometry.listX, Y: geometry.listY - 1},
		{Button: tea.MouseLeft, X: geometry.x + geometry.width, Y: geometry.listY},
		{Button: tea.MouseRight, X: geometry.listX, Y: geometry.listY + 1},
	} {
		updated, cmd = m.Update(tea.MouseClickMsg(mouse))
		if cmd != nil {
			t.Fatalf("outside/non-left click (%d,%d) returned a branch command", mouse.X, mouse.Y)
		}
		if updated.cursor != 0 {
			t.Fatalf("outside/non-left click moved cursor to %d", updated.cursor)
		}
	}
}

func TestBranchPickerEnterOnCurrentBranchDoesNotSwitch(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature"}, "main")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter on the current branch must not start a redundant switch")
	}
}

func TestBranchPickerMouseWheelMovesSelectionWithinVisibleListAndClamps(t *testing.T) {
	branches := make([]string, 0, 24)
	for i := range cap(branches) {
		branches = append(branches, fmt.Sprintf("branch-%02d-%s", i, strings.Repeat("\u754c", 4)))
	}
	m := branchPickerForMouse(t, 100, 22, branches, "branch-00-")
	geometry := m.geometry()
	if geometry.visible < 5 {
		t.Fatalf("visible rows = %d, want a scrollable picker", geometry.visible)
	}

	for range len(branches) + 4 {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, X: geometry.listX, Y: geometry.listY}))
		if cmd != nil {
			t.Fatal("wheel navigation must not switch branches")
		}
	}
	if m.cursor != len(branches)-1 {
		t.Fatalf("cursor after scrolling down = %d, want %d", m.cursor, len(branches)-1)
	}
	if m.scrollY != len(branches)-m.maxVisible() {
		t.Fatalf("scrollY after scrolling down = %d, want %d", m.scrollY, len(branches)-m.maxVisible())
	}

	for range len(branches) + 4 {
		m, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp, X: geometry.listX, Y: geometry.listY}))
	}
	if m.cursor != 0 || m.scrollY != 0 {
		t.Fatalf("upward wheel clamp = cursor %d, scrollY %d; want 0, 0", m.cursor, m.scrollY)
	}
}

func TestBranchPickerMouseWheelOutsideListDoesNotChangeSelection(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature", "release"}, "main")
	geometry := m.geometry()

	for _, mouse := range []tea.Mouse{
		{Button: tea.MouseWheelDown, X: geometry.x - 1, Y: geometry.listY},
		{Button: tea.MouseWheelDown, X: geometry.listX, Y: geometry.listY - 1},
		{Button: tea.MouseWheelDown, X: geometry.x + geometry.width, Y: geometry.listY},
		{Button: tea.MouseWheelLeft, X: geometry.listX, Y: geometry.listY},
	} {
		updated, cmd := m.Update(tea.MouseWheelMsg(mouse))
		if cmd != nil {
			t.Fatal("wheel never activates a branch")
		}
		if updated.cursor != 0 || updated.scrollY != 0 {
			t.Fatalf("wheel outside list changed state: cursor %d scrollY %d", updated.cursor, updated.scrollY)
		}
	}
}

func TestBranchPickerMouseRespectsFilterAndClippedTerminalCells(t *testing.T) {
	m := branchPickerForMouse(t, 12, 12, []string{"main", "feature/\u754c\u754c\u754c"}, "main")
	focus := m.Focus()
	m, _ = m.Update(focus())
	filtered, filterCmd := m.Update(tea.KeyPressMsg{Text: "feature"})
	filtered, _ = filtered.Update(branchPickerFilterMessage(t, filterCmd))
	if len(filtered.filtered) != 1 || filtered.filtered[0] != "feature/\u754c\u754c\u754c" {
		t.Fatalf("text filter = %#v, want one Unicode branch", filtered.filtered)
	}

	geometry := filtered.geometry()
	if geometry.listWidth != 10 {
		t.Fatalf("clipped list width = %d, want terminal-visible 10 cells", geometry.listWidth)
	}
	// Coordinates beyond the 12-cell terminal are never valid rows even though
	// the logical modal has its normal 30-cell minimum width.
	updated, cmd := filtered.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      12,
		Y:      geometry.listY,
	}))
	if cmd != nil || updated.cursor != 0 {
		t.Fatal("click beyond clipped terminal cells activated the hidden row")
	}

	updated, cmd = filtered.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      geometry.listX,
		Y:      geometry.listY,
	}))
	if cmd == nil {
		t.Fatal("click on the visible clipped row did not switch the filtered branch")
	}
	if got := cmd().(SwitchBranchMsg).Branch; got != "feature/\u754c\u754c\u754c" {
		t.Fatalf("switch branch = %q, want filtered Unicode branch", got)
	}
}

func TestBranchPickerFiltersInputAsynchronously(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature/café", "feature/other", "release"}, "main")
	_ = m.Focus()

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "café"})
	if !updated.filterPending {
		t.Fatal("input did not mark the branch filter pending")
	}
	if updated.filtered != nil {
		t.Fatalf("input synchronously projected branches: %#v", updated.filtered)
	}
	ready := branchPickerFilterMessage(t, cmd)
	updated, followup := updated.Update(ready)
	if followup != nil {
		t.Fatal("ordinary filter completion emitted an unexpected command")
	}
	if updated.filterPending {
		t.Fatal("current filter result remained pending")
	}
	if len(updated.filtered) != 1 || updated.filtered[0] != "feature/café" {
		t.Fatalf("filtered branches = %#v, want Unicode match", updated.filtered)
	}
}

func TestBranchPickerLatestFilterWins(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature/one", "feature/two", "release"}, "main")
	_ = m.Focus()

	first, firstCmd := m.Update(tea.KeyPressMsg{Text: "fea"})
	latest, latestCmd := first.Update(tea.KeyPressMsg{Text: "ture/two"})
	stale := branchPickerFilterMessage(t, firstCmd)
	current := branchPickerFilterMessage(t, latestCmd)

	latest, followup := latest.Update(stale)
	if followup != nil || !latest.filterPending || latest.filtered != nil {
		t.Fatal("stale filter result changed the pending latest projection")
	}
	latest, _ = latest.Update(current)
	if latest.filterPending || len(latest.filtered) != 1 || latest.filtered[0] != "feature/two" {
		t.Fatalf("latest filter result = pending %v, branches %#v", latest.filterPending, latest.filtered)
	}
}

func TestBranchPickerEnterDuringPendingFilterSwitchesWhenReady(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature/one", "feature/two"}, "main")
	_ = m.Focus()
	m, cmd := m.Update(tea.KeyPressMsg{Text: "feature/two"})

	var followup tea.Cmd
	m, followup = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if followup != nil {
		t.Fatal("Enter switched a stale branch while filtering was pending")
	}
	m, followup = m.Update(branchPickerFilterMessage(t, cmd))
	if followup == nil {
		t.Fatal("pending Enter did not switch after the current result arrived")
	}
	msg, ok := followup().(SwitchBranchMsg)
	if !ok || msg.Branch != "feature/two" {
		t.Fatalf("pending switch = %#v, want feature/two", msg)
	}
}

func TestBranchPickerEscapeCancelsPendingFilter(t *testing.T) {
	m := branchPickerForMouse(t, 100, 30, []string{"main", "feature/one"}, "main")
	_ = m.Focus()
	m, cmd := m.Update(tea.KeyPressMsg{Text: "feature"})
	ready := branchPickerFilterMessage(t, cmd)

	m, closeCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if closeCmd == nil {
		t.Fatal("Escape did not close the branch picker")
	}
	if m.filterPending {
		t.Fatal("Escape left the branch filter pending")
	}
	updated, followup := m.Update(ready)
	if followup != nil || updated.filterPending || updated.filtered != nil {
		t.Fatal("canceled filter result was applied after Escape")
	}
}

func TestFilterBranchesContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	branches := make([]string, 1_000)
	if matches, err := filterBranchesContext(ctx, branches, "feature"); !errors.Is(err, context.Canceled) || matches != nil {
		t.Fatalf("canceled filter = (%#v, %v), want nil context.Canceled", matches, err)
	}
}

func branchPickerFilterMessage(t *testing.T, cmd tea.Cmd) BranchFilterReadyMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil branch picker command")
	}
	switch msg := cmd().(type) {
	case BranchFilterReadyMsg:
		return msg
	case tea.BatchMsg:
		for _, child := range msg {
			if child == nil {
				continue
			}
			if ready, ok := child().(BranchFilterReadyMsg); ok {
				return ready
			}
		}
	}
	t.Fatal("branch picker command did not produce BranchFilterReadyMsg")
	return BranchFilterReadyMsg{}
}

func BenchmarkBranchPickerFilterDispatchThousands(b *testing.B) {
	branches := make([]string, 50_000)
	for i := range branches {
		branches[i] = fmt.Sprintf("feature/branch-%05d", i)
	}
	branches[len(branches)/2] = "feature/needle"

	model := NewBranchPicker(ui.DefaultTheme())
	model.SetBranches(branches, "main")
	_ = model.Focus()
	input := tea.KeyPressMsg{Text: "needle"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		updated, cmd := model.Update(input)
		if !updated.filterPending || cmd == nil || updated.filtered != nil {
			b.Fatal("branch filter was not dispatched asynchronously")
		}
		updated.cancelFilter()
	}
}

func BenchmarkFilterBranchesContextThousands(b *testing.B) {
	branches := make([]string, 50_000)
	for i := range branches {
		branches[i] = fmt.Sprintf("feature/branch-%05d", i)
	}
	branches[len(branches)/2] = "feature/needle"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		matches, err := filterBranchesContext(context.Background(), branches, "needle")
		if err != nil || len(matches) != 1 || matches[0] != "feature/needle" {
			b.Fatalf("background filter = (%#v, %v), want feature/needle", matches, err)
		}
	}
}
