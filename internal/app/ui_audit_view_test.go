package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/acp"
	"teak/internal/settings"
	"teak/internal/toolpath"
)

// statusBarRow returns the plain text of the status bar's content row.
func statusBarRow(t *testing.T, m Model) string {
	t.Helper()
	rows := strings.Split(m.renderStatusBar(), "\n")
	if len(rows) != 2 {
		t.Fatalf("status bar rendered %d rows, want divider + bar", len(rows))
	}
	if w := ansi.StringWidth(rows[1]); w != m.width {
		t.Fatalf("status bar is %d columns wide, want %d", w, m.width)
	}
	return ansi.Strip(rows[1])
}

func TestStatusBarLongMessageKeepsRightSegments(t *testing.T) {
	const longMessage = `Agent stopped: agent command "opencode" not found: opencode not found. Install with: curl -fsSL https://opencode.ai/install | bash`
	tests := []struct {
		name      string
		width     int
		message   string
		wantAll   []string
		wantNone  []string
		wantEllip bool
	}{
		{
			name:    "no message keeps every segment",
			width:   140,
			wantAll: []string{"F1 Help", "Ln 1, Col 1", "Spaces: 4", "LF", "UTF-8", "All"},
		},
		{
			name:      "wide terminal truncates the message with an ellipsis",
			width:     140,
			message:   longMessage,
			wantAll:   []string{"F1 Help", "Ln 1, Col 1", "Spaces: 4", "UTF-8", "Agent stopped"},
			wantEllip: true,
		},
		{
			name:      "narrow terminal drops low-priority segments first",
			width:     80,
			message:   longMessage,
			wantAll:   []string{"F1 Help", "Ln 1, Col 1", "Agent stopped"},
			wantNone:  []string{"UTF-8", "Spaces: 4"},
			wantEllip: true,
		},
		{
			name:    "short message on a narrow terminal keeps everything it can",
			width:   80,
			message: "Saved",
			wantAll: []string{"Saved", "Ln 1, Col 1", "UTF-8"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newViewTestModel(t, false)
			m.width = tt.width
			m.relayout()
			m.status = tt.message
			row := statusBarRow(t, m)
			for _, want := range tt.wantAll {
				if !strings.Contains(row, want) {
					t.Errorf("status bar missing %q:\n%s", want, row)
				}
			}
			for _, bad := range tt.wantNone {
				if strings.Contains(row, bad) {
					t.Errorf("status bar should have dropped %q:\n%s", bad, row)
				}
			}
			if tt.wantEllip != strings.Contains(row, "…") {
				t.Errorf("ellipsis present = %v, want %v:\n%s", !tt.wantEllip, tt.wantEllip, row)
			}
		})
	}
}

func TestStatusBarKeepsCursorAndProblemsOnTinyWidths(t *testing.T) {
	for _, width := range []int{30, 40, 50, 60} {
		t.Run(fmt.Sprintf("%d cols", width), func(t *testing.T) {
			m := newViewTestModel(t, false)
			m.width = width
			m.relayout()
			m.status = strings.Repeat("a long status message ", 6)
			row := statusBarRow(t, m)
			if ansi.StringWidth(row) != width {
				t.Fatalf("row width = %d, want %d", ansi.StringWidth(row), width)
			}
		})
	}
}

func TestSettingsHintFitsBoxWidth(t *testing.T) {
	for _, width := range []int{80, 100, 140, 40, 24} {
		t.Run(fmt.Sprintf("%d cols", width), func(t *testing.T) {
			m := newViewTestModel(t, true)
			m.width, m.height = width, 24
			m.relayout()
			m.showSettings = true
			m.settingsM = settings.New(m.theme, m.appCfg, "")
			m.setSettingsOverlaySize()

			_, _, modalWidth, _ := m.settingsModalGeometry()
			inner := max(1, modalWidth-6)
			hint := m.settingsHint(inner)
			var joined []string
			for _, row := range strings.Split(hint, "\n") {
				plain := ansi.Strip(row)
				if w := ansi.StringWidth(plain); w != inner {
					t.Errorf("hint row %q is %d cols, want exactly %d", plain, w, inner)
				}
				joined = append(joined, strings.TrimSpace(plain))
			}
			if inner >= 66 && len(joined) != 1 {
				t.Errorf("hint wrapped into %d rows at inner width %d: %q", len(joined), inner, joined)
			}
			text := strings.Join(joined, " ")
			for _, item := range settingsHintItems {
				if inner >= 24 && !strings.Contains(text, item) {
					t.Errorf("hint dropped item %q: %q", item, text)
				}
			}

			// The rendered overlay must not contain a bare hint row: every row
			// of the hint carries the muted foreground and the panel background.
			want := lipgloss.NewStyle().
				Foreground(m.theme.Muted.GetForeground()).
				Background(m.theme.Panel.GetBackground()).Render("x")
			prefix := strings.SplitN(want, "x", 2)[0]
			if prefix == "" || !strings.Contains(hint, prefix) {
				t.Errorf("hint does not use theme.Muted on theme.Panel (%q)", prefix)
			}
			for _, row := range strings.Split(hint, "\n") {
				if !strings.Contains(row, "\x1b[") {
					t.Errorf("hint row %q carries no styling", row)
				}
			}
		})
	}
}

func TestSettingsOverlayHintDoesNotWrapToColumnZero(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 80, 24
	m.relayout()
	m.showSettings = true
	m.settingsM = settings.New(m.theme, m.appCfg, "")
	m.setSettingsOverlaySize()

	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "cancel" || strings.HasPrefix(trimmed, "cancel") && !strings.Contains(trimmed, "Esc") {
			t.Fatalf("hint wrapped mid-item: %q", line)
		}
	}
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Esc cancel") {
			found = true
		}
	}
	if !found {
		t.Fatal("settings hint 'Esc cancel' not found intact in the 80x24 view")
	}
}

func TestUnsavedFilesMessagePluralization(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "You have 1 unsaved file:"},
		{2, "You have 2 unsaved files:"},
		{3, "You have 3 unsaved files:"},
	}
	for _, tt := range tests {
		if got := unsavedFilesMessage(tt.n); got != tt.want {
			t.Errorf("unsavedFilesMessage(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestAgentToggleRefusesWhenPanelCannotBeShown(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 80, 24
	m.relayout()
	if !m.treeVisible() {
		t.Fatal("setup: sidebar should be visible at 80x24")
	}
	if m.agentPanelFits() {
		t.Fatal("setup: the agent panel should not fit beside the sidebar at 80x24")
	}

	cmd := m.toggleAgentPanel()
	if cmd != nil {
		t.Errorf("toggle returned a command for a panel that cannot be shown")
	}
	if m.showAgent {
		t.Error("showAgent = true although the panel cannot be displayed")
	}
	if m.focus != FocusEditor {
		t.Errorf("focus = %v, want editor", m.focus)
	}
	if !strings.Contains(m.status, "Ctrl+B") || !strings.Contains(m.status, "terminal") {
		t.Errorf("status = %q, want guidance about hiding the sidebar or widening the terminal", m.status)
	}

	// Hiding the sidebar makes room, so the same chord now works.
	m.showTree = false
	m.relayout()
	m.status = ""
	if cmd := m.toggleAgentPanel(); cmd == nil && !m.showAgent {
		t.Fatal("toggle still refused with the sidebar hidden")
	}
	if !m.showAgent || m.focus != FocusAgent {
		t.Errorf("showAgent=%v focus=%v, want shown and focused", m.showAgent, m.focus)
	}
}

func TestAgentFocusReturnsToEditorWhenTerminalShrinks(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width, m.height = 120, 40
	m.relayout()
	_ = m.toggleAgentPanel()
	if m.focus != FocusAgent || m.agentPanelWidth() == 0 {
		t.Fatalf("setup: focus=%v width=%d, want a focused, visible agent panel", m.focus, m.agentPanelWidth())
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = updated.(Model)
	if m.agentPanelWidth() != 0 {
		t.Fatal("agent panel should auto-hide at 60 columns")
	}
	if m.focus != FocusEditor {
		t.Errorf("focus = %v after the agent panel auto-hid, want editor", m.focus)
	}
	if m.agentPanel.IsInputFocused() {
		t.Error("agent input still focused after auto-hide")
	}
}

func TestAgentWriteRequestDoesNotFocusInvisiblePanel(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 80, 24
	m.relayout()

	updated, _ := m.handleACPMsg(acpMsg{msg: acp.AgentWriteFileMsg{Path: "x.go", Content: "package x\n"}})
	m = updated.(Model)
	if m.focus == FocusAgent {
		t.Error("write request moved focus into an agent panel that is not visible")
	}
	if m.status == "" {
		t.Error("expected a status explaining that the agent needs room")
	}
}

func TestAgentStartFailureMessages(t *testing.T) {
	t.Run("missing command is short and defers to the panel", func(t *testing.T) {
		m := newViewTestModel(t, false)
		err := fmt.Errorf("agent command %q not found: %w", "opencode",
			&toolpath.MissingToolError{Tool: "opencode", Hint: "curl -fsSL https://opencode.ai/install | bash"})
		updated, _ := m.handleACPMsg(acpMsg{msg: acp.AgentStoppedMsg{Err: err}})
		m = updated.(Model)

		want := "Agent unavailable: opencode not installed (Ctrl+J)"
		if m.status != want {
			t.Errorf("status = %q, want %q", m.status, want)
		}
		if strings.Contains(m.status, "curl") || strings.Count(m.status, "not found") != 0 {
			t.Errorf("status repeats the install hint: %q", m.status)
		}

		m.agentPanel.SetSize(40, 30)
		view := ansi.Strip(m.agentPanel.View())
		for _, want := range []string{"opencode is not installed.", "Install with:", "curl -fsSL"} {
			if !strings.Contains(view, want) {
				t.Errorf("agent panel missing %q:\n%s", want, view)
			}
		}
		if strings.Contains(view, "[agent]") {
			t.Errorf("agent panel still shows the config snippet for a configured agent:\n%s", view)
		}
	})

	t.Run("other failures keep the generic message", func(t *testing.T) {
		m := newViewTestModel(t, false)
		updated, _ := m.handleACPMsg(acpMsg{msg: acp.AgentStoppedMsg{Err: errors.New("boom")}})
		m = updated.(Model)
		if m.status != "Agent stopped: boom" {
			t.Errorf("status = %q", m.status)
		}
	})
}

func TestAgentStartFailureKeepsBrokenOverrideError(t *testing.T) {
	m := newViewTestModel(t, false)
	err := fmt.Errorf("agent command %q not found: %w", "opencode",
		&toolpath.MissingToolError{Tool: "opencode", Hint: "configured path /opt/opencode is not executable; fix or remove the override"})
	updated, _ := m.handleACPMsg(acpMsg{msg: acp.AgentStoppedMsg{Err: err}})
	m = updated.(Model)

	if !strings.HasPrefix(m.status, "Agent stopped: ") || !strings.Contains(m.status, "not executable") {
		t.Errorf("status = %q, want the full override error", m.status)
	}
	m.agentPanel.SetSize(40, 30)
	view := strings.Join(strings.Fields(ansi.Strip(m.agentPanel.View())), " ")
	if strings.Contains(view, "is not installed") || !strings.Contains(view, "not executable") {
		t.Errorf("agent panel should show the override error, not a missing install:\n%s", view)
	}
}

func TestAgentFocusChordRefusesHiddenPanel(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 80, 24
	m.showAgent = true
	m.relayout()
	if m.agentPanelFits() {
		t.Fatal("setup: the agent panel should not fit beside the sidebar at 80x24")
	}

	updated, cmd, handled := m.handleGlobalKey(tea.KeyPressMsg{Code: '\'', Mod: tea.ModCtrl})
	m = updated.(Model)
	if !handled {
		t.Fatal("ctrl+' was not handled")
	}
	if cmd != nil || m.focus == FocusAgent {
		t.Errorf("focus = %v, cmd = %v; a hidden agent panel must not take focus", m.focus, cmd)
	}
	if m.status != agentNoRoomStatus {
		t.Errorf("status = %q, want %q", m.status, agentNoRoomStatus)
	}
}

func TestCommandPaletteRightAlignsShortcuts(t *testing.T) {
	m := newViewTestModel(t, false)
	for _, item := range m.buildCommandList() {
		command := item.Value.(Command)
		if item.Label != command.Label || item.Description != command.Shortcut {
			t.Fatalf("palette item %q/%q, want label %q and shortcut %q as description",
				item.Label, item.Description, command.Label, command.Shortcut)
		}
	}

	updated, _ := m.openCommandPalette()
	m = updated.(Model)
	view := ansi.Strip(m.overlayStack.Top().View())
	var row string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Save File") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("palette has no Save File row:\n%s", view)
	}
	// The shortcut sits at the end of the row, separated from the label by
	// padding, with only the box padding and border after it.
	trimmed := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(row, " "), "│"), " ")
	if !strings.HasSuffix(trimmed, "Ctrl+S") || strings.Contains(row, "Save File  Ctrl+S") {
		t.Errorf("Save File row does not right-align its shortcut: %q", row)
	}
}
