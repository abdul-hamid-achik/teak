package problems

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

func TestProblem_SeverityLabel(t *testing.T) {
	tests := []struct {
		name     string
		severity int
		want     string
	}{
		{"error", 1, "Error"},
		{"warning", 2, "Warning"},
		{"info", 3, "Info"},
		{"hint", 4, "Hint"},
		{"unknown", 99, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Problem{Severity: tt.severity}
			if got := p.SeverityLabel(); got != tt.want {
				t.Errorf("SeverityLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModel_SetProblems(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	problems := []Problem{
		{FilePath: "/test/root/main.go", Line: 10, Col: 5, Severity: 1, Message: "syntax error"},
		{FilePath: "/test/root/util.go", Line: 25, Col: 3, Severity: 2, Message: "unused variable"},
	}

	m.SetProblems(problems)

	if m.ProblemCount() != 2 {
		t.Errorf("ProblemCount() = %d, want 2", m.ProblemCount())
	}
	if m.ErrorCount() != 1 {
		t.Errorf("ErrorCount() = %d, want 1", m.ErrorCount())
	}
	if m.WarningCount() != 1 {
		t.Errorf("WarningCount() = %d, want 1", m.WarningCount())
	}
}

func TestModelSelectAt(t *testing.T) {
	tests := []struct {
		name         string
		index        int
		wantSelected int
		wantOK       bool
	}{
		{name: "first", index: 0, wantSelected: 0, wantOK: true},
		{name: "last", index: 2, wantSelected: 2, wantOK: true},
		{name: "negative", index: -1, wantSelected: 0, wantOK: false},
		{name: "past end", index: 3, wantSelected: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(ui.DefaultTheme(), "/tmp")
			m.SetProblems([]Problem{
				{FilePath: "/tmp/a.go"},
				{FilePath: "/tmp/b.go"},
				{FilePath: "/tmp/c.go"},
			})

			gotOK := m.SelectAt(tt.index)
			if gotOK != tt.wantOK {
				t.Errorf("SelectAt(%d) = %v, want %v", tt.index, gotOK, tt.wantOK)
			}
			if got := m.SelectedIndex(); got != tt.wantSelected {
				t.Errorf("SelectedIndex() = %d, want %d", got, tt.wantSelected)
			}
		})
	}
}

func TestModel_Groups(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	problems := []Problem{
		{FilePath: "/test/root/main.go", Line: 10, Col: 5, Severity: 1, Message: "error1"},
		{FilePath: "/test/root/main.go", Line: 20, Col: 5, Severity: 2, Message: "warning1"},
		{FilePath: "/test/root/util.go", Line: 5, Col: 3, Severity: 1, Message: "error2"},
	}

	m.SetProblems(problems)

	if len(m.groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(m.groups))
	}
	if got := m.groups[0].FilePath; got != "/test/root/main.go" {
		t.Errorf("Expected first group to be sorted by path, got %q", got)
	}
}

func TestModel_Selection(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	problems := []Problem{
		{FilePath: "/test/root/main.go", Line: 10, Col: 5, Severity: 1, Message: "error1"},
		{FilePath: "/test/root/util.go", Line: 20, Col: 3, Severity: 2, Message: "warning1"},
		{FilePath: "/test/root/test.go", Line: 5, Col: 1, Severity: 3, Message: "info1"},
	}

	m.SetProblems(problems)

	// Test initial selection
	if m.selectedIndex != 0 {
		t.Errorf("Initial selectedIndex = %d, want 0", m.selectedIndex)
	}

	// Test SelectNext
	m.SelectNext()
	if m.selectedIndex != 1 {
		t.Errorf("After SelectNext selectedIndex = %d, want 1", m.selectedIndex)
	}

	// Test SelectPrev
	m.SelectPrev()
	if m.selectedIndex != 0 {
		t.Errorf("After SelectPrev selectedIndex = %d, want 0", m.selectedIndex)
	}

	// Test wrap around
	m.SelectPrev()
	if m.selectedIndex != 2 {
		t.Errorf("After wrap around SelectPrev selectedIndex = %d, want 2", m.selectedIndex)
	}
}

func TestModel_Scrolling(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	// Create enough problems to require scrolling
	var problems []Problem
	for i := 0; i < 50; i++ {
		problems = append(problems, Problem{
			FilePath: "/test/root/file.go",
			Line:     i,
			Col:      0,
			Severity: 1,
			Message:  "error",
		})
	}

	m.SetProblems(problems)
	m.height = 10 // Set a small height for testing

	// Test ScrollDown
	m.ScrollDown(5)
	if m.scrollY != 5 {
		t.Errorf("After ScrollDown(5) scrollY = %d, want 5", m.scrollY)
	}

	// Test ScrollUp
	m.ScrollUp(3)
	if m.scrollY != 2 {
		t.Errorf("After ScrollUp(3) scrollY = %d, want 2", m.scrollY)
	}

	// Test boundary - scroll up past start
	m.ScrollUp(10)
	if m.scrollY != 0 {
		t.Errorf("After scroll up past start scrollY = %d, want 0", m.scrollY)
	}

	// Test boundary - scroll down past end
	m.ScrollDown(100)
	maxScroll := len(problems) - m.height
	if m.scrollY != maxScroll {
		t.Errorf("After scroll down past end scrollY = %d, want %d", m.scrollY, maxScroll)
	}
}

func TestModel_EnsureVisible(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	var problems []Problem
	for i := 0; i < 50; i++ {
		problems = append(problems, Problem{
			FilePath: "/test/root/file.go",
			Line:     i,
			Col:      0,
			Severity: 1,
			Message:  "error",
		})
	}

	m.SetProblems(problems)
	m.height = 10

	// Select an item that's out of view
	m.selectedIndex = 25
	m.scrollY = 0
	m.ensureVisible()

	if m.scrollY == 0 {
		t.Error("ensureVisible() should have scrolled to make selection visible")
	}
}

func TestModel_Summary(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	// Test no problems
	if m.Summary() != "No problems" {
		t.Errorf("Summary() with no problems = %v, want 'No problems'", m.Summary())
	}

	// Test with errors and warnings
	problems := []Problem{
		{Severity: 1, Message: "error1"},
		{Severity: 1, Message: "error2"},
		{Severity: 2, Message: "warning1"},
	}
	m.SetProblems(problems)

	summary := m.Summary()
	if summary != "2 error(s), 1 warning(s)" {
		t.Errorf("Summary() = %v, want '2 error(s), 1 warning(s)'", summary)
	}
}

func TestModel_SelectedPosition(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	problems := []Problem{
		{FilePath: "/test/root/main.go", Line: 10, Col: 5, Severity: 1, Message: "error"},
	}
	m.SetProblems(problems)

	filePath, pos := m.SelectedPosition()
	if filePath != "/test/root/main.go" {
		t.Errorf("SelectedPosition() filePath = %v, want '/test/root/main.go'", filePath)
	}
	if pos.Line != 10 {
		t.Errorf("SelectedPosition() line = %d, want 10", pos.Line)
	}
	if pos.Col != 5 {
		t.Errorf("SelectedPosition() col = %d, want 5", pos.Col)
	}
}

func TestModel_SelectedProblem_Empty(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")

	if m.SelectedProblem() != nil {
		t.Error("SelectedProblem() should return nil when no problems")
	}
}

func TestModel_View_Empty(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")
	m.SetSize(50, 10)

	view := m.View()
	if view == "" {
		t.Error("View() should not return empty string when no problems")
	}
}

func TestModel_View_WithProblems(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/test/root")
	m.SetSize(80, 5)

	problems := []Problem{
		{FilePath: "/test/root/main.go", Line: 10, Col: 5, Severity: 1, Message: "syntax error: unexpected EOF"},
		{FilePath: "/test/root/util.go", Line: 25, Col: 3, Severity: 2, Message: "unused variable: x"},
	}
	m.SetProblems(problems)

	view := m.View()
	if view == "" {
		t.Error("View() should not return empty string when problems exist")
	}
}

func TestModelViewNarrowWidthsTruncatesUnicodeMessagesSafely(t *testing.T) {
	for _, width := range []int{0, 15, 19, 22, 25} {
		t.Run("width="+strconv.Itoa(width), func(t *testing.T) {
			m := New(ui.DefaultTheme(), "/root")
			m.SetSize(width, 1)
			m.SetProblems([]Problem{{
				FilePath: "/root/a.go",
				Severity: 1,
				Message:  "\x1b[31m界界界 diagnostic\x1b[0m",
			}})

			view := m.View()
			if !utf8.ValidString(ansi.Strip(view)) {
				t.Fatalf("View() produced invalid UTF-8: %q", view)
			}
			if width == 25 {
				plain := ansi.Strip(view)
				if !strings.Contains(plain, "界...") {
					t.Errorf("View() = %q, want display-width truncation containing %q", plain, "界...")
				}
				if strings.Contains(plain, "界界") {
					t.Errorf("View() = %q, want message truncated before a second wide character", plain)
				}
			}
		})
	}
}

func TestModelScrollingIsSafeWithNonPositiveHeight(t *testing.T) {
	for _, height := range []int{0, -1} {
		t.Run("height="+strconv.Itoa(height), func(t *testing.T) {
			m := New(ui.DefaultTheme(), "/root")
			m.SetSize(25, height)
			m.SetProblems([]Problem{
				{FilePath: "/root/a.go"},
				{FilePath: "/root/b.go"},
			})
			m.selectedIndex = 1
			m.scrollY = 1

			m.ensureVisible()
			m.ScrollDown(3)
			m.ScrollUp(3)

			if got := m.ScrollY(); got != 0 {
				t.Errorf("ScrollY() = %d, want 0 when height is non-positive", got)
			}
		})
	}
}
