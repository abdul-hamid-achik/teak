package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestDisconnectedEmptyState(t *testing.T) {
	const installHint = "curl -fsSL https://opencode.ai/install | bash"
	tests := []struct {
		name         string
		reason, hint string
		width        int
		wantAll      []string
		wantNone     []string
	}{
		{
			name:     "not configured shows the config snippet",
			width:    38,
			wantAll:  []string{"Agent not connected.", "[agent]", "enabled = true", `command = "opencode"`},
			wantNone: []string{"Install with:"},
		},
		{
			name:     "start failure shows the reason and hint instead of the snippet",
			reason:   "opencode is not installed.",
			hint:     installHint,
			width:    38,
			wantAll:  []string{"Agent not connected.", "opencode is not installed.", "Install with:", "curl -fsSL"},
			wantNone: []string{"[agent]", "Configure in"},
		},
		{
			name:     "start failure without a hint",
			reason:   "agent exited immediately",
			width:    30,
			wantAll:  []string{"agent exited immediately"},
			wantNone: []string{"Install with:", "[agent]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(ui.DefaultTheme())
			m.SetSize(tt.width+2, 30)
			m.SetStartFailure(tt.reason, tt.hint)
			lines := plainLines(m.buildChatLines(tt.width))
			joined := strings.Join(lines, "\n")
			for _, want := range tt.wantAll {
				if !strings.Contains(joined, want) {
					t.Errorf("missing %q in:\n%s", want, joined)
				}
			}
			for _, bad := range tt.wantNone {
				if strings.Contains(joined, bad) {
					t.Errorf("unexpected %q in:\n%s", bad, joined)
				}
			}
			for _, l := range lines {
				if l == "" {
					continue
				}
				if !strings.HasPrefix(l, " ") {
					t.Errorf("row %q lost its left padding", l)
				}
				if w := ansi.StringWidth(l); w > tt.width {
					t.Errorf("row %q is %d cols, wider than %d", l, w, tt.width)
				}
			}
		})
	}
}

// At the 140x40 panel width the config path line used to wrap and its second
// row started one column left of every other row.
func TestDisconnectedWrappedRowsKeepPadding(t *testing.T) {
	m := New(ui.DefaultTheme())
	m.SetSize(40, 30)
	lines := plainLines(m.buildChatLines(37))
	var rows []string
	for _, l := range lines {
		if strings.Contains(l, "config.toml:") || strings.Contains(l, "Configure in") {
			rows = append(rows, l)
		}
	}
	if len(rows) < 2 {
		t.Fatalf("expected the configure line to wrap at 37 cols, got %q", rows)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r, " ") || strings.HasPrefix(r, "  ") {
			t.Errorf("wrapped row %q should be padded by exactly one column", r)
		}
	}
}

func TestSetConnectedClearsStartFailure(t *testing.T) {
	m := New(ui.DefaultTheme())
	m.SetSize(40, 20)
	m.SetStartFailure("opencode is not installed.", "brew install opencode")
	m.SetConnected(true)
	if m.startReason != "" || m.startHint != "" {
		t.Fatalf("start failure survived a successful connection: %q %q", m.startReason, m.startHint)
	}
}

func TestDisconnectedUsesMutedForeground(t *testing.T) {
	theme := ui.DefaultTheme()
	m := New(theme)
	m.SetSize(42, 20)
	raw := strings.Join(m.buildChatLines(40), "\n")
	// The SGR prefix lipgloss emits for the Muted foreground must colour the
	// empty-state text; the gutter foreground must not.
	mutedSeq := strings.SplitN(theme.Muted.Render("x"), "x", 2)[0]
	if mutedSeq == "" || !strings.Contains(raw, mutedSeq) {
		t.Errorf("empty state does not use theme.Muted (%q)", mutedSeq)
	}
}
