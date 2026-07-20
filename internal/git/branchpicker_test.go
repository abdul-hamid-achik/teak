package git

import (
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
	filtered, _ := m.Update(tea.KeyPressMsg{Text: "feature"})
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
