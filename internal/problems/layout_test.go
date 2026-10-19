package problems

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

const goplsMessage = "declared and not used: unused"

func goplsProblem() Problem {
	return Problem{FilePath: "/ws/cmd/server/main.go", Line: 38, Col: 1, Severity: 1, Message: goplsMessage}
}

// TestRowShowsMessageBeforeLocation pins the row layout: the diagnostic text
// leads and the location trails in the muted role, so a narrow sidebar clips
// the location rather than the message.
func TestRowShowsMessageBeforeLocation(t *testing.T) {
	tests := []struct {
		name         string
		width        int
		wantLocation bool
		wantMessage  string
	}{
		{"sidebar 25", 25, false, "declared and not used:"},
		{"sidebar 31", 31, false, "declared and not used: unused"},
		{"sidebar 42", 42, false, goplsMessage},
		{"medium 80", 80, true, goplsMessage},
		{"wide 140", 140, true, goplsMessage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(ui.NordTheme(), "/ws")
			m.SetSize(tt.width, 3)
			m.SetProblems([]Problem{goplsProblem()})
			view := m.View()
			plain := ansi.Strip(view)

			if !strings.HasPrefix(plain, "✗ ") {
				t.Errorf("row = %q, want severity icon first", plain)
			}
			if !strings.Contains(plain, tt.wantMessage) {
				t.Errorf("row = %q, want message %q visible", plain, tt.wantMessage)
			}
			if got := strings.Contains(plain, "cmd/server/main.go:39:2"); got != tt.wantLocation {
				t.Errorf("row = %q, full location visible = %v, want %v", plain, got, tt.wantLocation)
			}
			if tt.wantLocation && strings.Index(plain, goplsMessage) > strings.Index(plain, "main.go") {
				t.Errorf("row = %q, want message before location", plain)
			}
			if got := lipgloss.Width(view); got > tt.width {
				t.Errorf("row width = %d, want <= %d: %q", got, tt.width, plain)
			}
		})
	}
}

func TestRowLocationUsesMutedRole(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/ws")
	m.SetSize(80, 3)
	m.SetProblems([]Problem{goplsProblem()})

	muted := lipgloss.NewStyle().Foreground(theme.Muted.GetForeground()).Render("x")
	muted = strings.TrimSuffix(strings.TrimPrefix(muted[:strings.Index(muted, "x")], "\x1b["), "m")
	view := m.View()
	idx := strings.Index(view, "cmd/server/main.go")
	if idx < 0 {
		t.Fatalf("location missing from %q", view)
	}
	head := view[:idx]
	run := head[strings.LastIndex(head, "\x1b[m")+1:]
	if !strings.Contains(run, muted) {
		t.Errorf("location run %q lacks Muted colour %q", run, muted)
	}
}

func TestSelectedRowKeepsHighlightAcrossWholeWidth(t *testing.T) {
	theme := ui.NordTheme()
	m := New(theme, "/ws")
	m.SetSize(60, 4)
	m.SetProblems([]Problem{
		goplsProblem(),
		{FilePath: "/ws/web/app.ts", Line: 1, Col: 0, Severity: 2, Message: "second"},
	})
	cursorBg := lipgloss.NewStyle().Background(theme.TreeCursor.GetBackground()).Render("x")
	cursorBg = strings.TrimSuffix(strings.TrimPrefix(cursorBg[:strings.Index(cursorBg, "x")], "\x1b["), "m")

	rows := strings.Split(m.View(), "\n")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if lipgloss.Width(rows[0]) != 60 {
		t.Errorf("selected row width = %d, want 60 (highlight spans the panel)", lipgloss.Width(rows[0]))
	}
	for _, segment := range strings.Split(rows[0], "\x1b[m") {
		if strings.TrimSpace(ansi.Strip(segment)) == "" && ansi.Strip(segment) == "" {
			continue
		}
		if !strings.Contains(segment, cursorBg) {
			t.Errorf("selected segment %q lacks cursor background %q", segment, cursorBg)
		}
	}
	if strings.Contains(rows[1], cursorBg) {
		t.Errorf("unselected row carries the cursor background: %q", rows[1])
	}

	m.SelectNext()
	rows = strings.Split(m.View(), "\n")
	if strings.Contains(rows[0], cursorBg) || !strings.Contains(rows[1], cursorBg) {
		t.Errorf("selection highlight did not follow SelectNext: %q", rows)
	}
}
