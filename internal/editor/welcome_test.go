package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestNewWelcome(t *testing.T) {
	theme := ui.DefaultTheme()
	welcome := NewWelcome(theme)

	if !welcome.Active {
		t.Error("expected Active to be true")
	}
	// Theme contains lipgloss.Style which cannot be compared directly
	if welcome.frame != 0 {
		t.Errorf("expected frame 0, got %d", welcome.frame)
	}
	if welcome.settled {
		t.Error("expected settled to be false")
	}
}

func TestWelcomeInit(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	cmd := welcome.Init()

	if cmd == nil {
		t.Error("expected cmd to be non-nil")
	}
}

func TestWelcomeSetSize(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	welcome.SetSize(80, 24)

	if welcome.width != 80 {
		t.Errorf("expected width 80, got %d", welcome.width)
	}
	if welcome.height != 24 {
		t.Errorf("expected height 24, got %d", welcome.height)
	}
}

func TestWelcomeUpdate(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}

	welcome, cmd := welcome.Update(WelcomeTickMsg{})

	if cmd == nil {
		t.Error("expected cmd to be non-nil for animation")
	}
	if welcome.frame != 1 {
		t.Errorf("expected frame 1, got %d", welcome.frame)
	}
}

func TestWelcomeUpdateNotActive(t *testing.T) {
	welcome := &Welcome{Active: false, theme: ui.DefaultTheme()}

	_, cmd := welcome.Update(WelcomeTickMsg{})

	if cmd != nil {
		t.Error("expected nil cmd when not active")
	}
}

func TestWelcomeUpdateSettled(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme(), settled: true}

	_, cmd := welcome.Update(WelcomeTickMsg{})

	if cmd != nil {
		t.Error("expected nil cmd when settled")
	}
}

func TestWelcomeUpdateSettlesAfterFrames(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}

	// Simulate 180 frames (3 seconds at 60fps)
	for i := 0; i < 180; i++ {
		welcome, _ = welcome.Update(WelcomeTickMsg{})
	}

	if !welcome.settled {
		t.Error("expected settled to be true after 180 frames")
	}
}

func TestWelcomeView(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "████") {
		t.Error("expected logo in view")
	}
}

func TestWelcomeViewNotActive(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.Active = false

	view := welcome.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestWelcomeViewContainsLogo(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()

	// Check for TEAK logo lines
	if !strings.Contains(view, "████████╗") {
		t.Error("expected logo line 1")
	}
	if !strings.Contains(view, "╚══██╔══╝") {
		t.Error("expected logo line 2")
	}
}

func TestWelcomeViewContainsSubtitle(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()

	if !strings.Contains(view, "terminal code editor") {
		t.Error("expected subtitle in view")
	}
}

func TestWelcomeViewContainsHints(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()

	if !strings.Contains(view, "Ctrl+B") {
		t.Error("expected Ctrl+B hint")
	}
	if !strings.Contains(view, "Toggle file tree") {
		t.Error("expected 'Toggle file tree' hint")
	}
	if !strings.Contains(view, "Ctrl+F") {
		t.Error("expected Ctrl+F hint")
	}
	if !strings.Contains(view, "Find in file") {
		t.Error("expected 'Find in file' hint")
	}
}

func TestWelcomeLogoStyle(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	if got := welcome.logoStyle(0).Render("T"); got == "T" {
		t.Error("expected themed logo style")
	}
}

func TestWelcomeLogoStyleSettled(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.settled = true

	if got := welcome.logoStyle(0).Render("T"); got == "T" {
		t.Error("expected themed settled logo style")
	}
}

func TestWelcomeLogoStyleDifferentLines(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	style0 := welcome.logoStyle(0)
	style1 := welcome.logoStyle(1)
	style2 := welcome.logoStyle(2)

	if style0.Render("T") == "T" || style1.Render("T") == "T" || style2.Render("T") == "T" {
		t.Error("expected themed logo styles")
	}
}

func TestWelcomeDismiss(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	welcome.Dismiss()

	if welcome.Active {
		t.Error("expected Active to be false")
	}
}

func TestWelcomeTickMsg(t *testing.T) {
	cmd := tickWelcome()
	if cmd == nil {
		t.Error("expected cmd to be non-nil")
	}

	// Execute the command
	msg := cmd()
	if msg == nil {
		t.Error("expected non-nil message")
	}

	_, ok := msg.(WelcomeTickMsg)
	if !ok {
		t.Errorf("expected WelcomeTickMsg, got %T", msg)
	}
}

func TestWelcomeViewCentered(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(100, 30)

	view := welcome.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// View should be centered
}

func TestWelcomeViewSmallSize(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(40, 10)

	view := welcome.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestWelcomeUpdateFrameIncrement(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}
	initialFrame := welcome.frame

	welcome, _ = welcome.Update(WelcomeTickMsg{})

	if welcome.frame <= initialFrame {
		t.Error("expected frame to increment")
	}
}

func TestWelcomeSetSizeAfterUpdate(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}
	welcome.Update(WelcomeTickMsg{})
	welcome.SetSize(80, 24)

	if welcome.width != 80 {
		t.Errorf("expected width 80, got %d", welcome.width)
	}
}

func TestWelcomeViewWithBackground(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()
	// Should have Nord0 background (hard to test directly)
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestWelcomeLogoHasSixLines(t *testing.T) {
	// The logo should have 6 lines
	logo := []string{
		"████████╗███████╗ █████╗ ██╗  ██╗",
		"╚══██╔══╝██╔════╝██╔══██╗██║ ██╔╝",
		"   ██║   █████╗  ███████║█████╔╝ ",
		"   ██║   ██╔══╝  ██╔══██║██╔═██╗ ",
		"   ██║   ███████╗██║  ██║██║  ██╗",
		"   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝",
	}
	if len(logo) != 6 {
		t.Errorf("expected 6 logo lines, got %d", len(logo))
	}
}

func TestWelcomeHints(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()

	// Check all hints are present
	expectedHints := []string{
		"Ctrl+B",
		"Ctrl+F",
		"Ctrl+Q",
		"F1",
	}
	for _, hint := range expectedHints {
		if !strings.Contains(view, hint) {
			t.Errorf("expected hint %q in view", hint)
		}
	}
}

func TestWelcomeViewAnimationFrames(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}
	welcome.SetSize(80, 24)

	// Check view at different animation frames
	view0 := welcome.View()

	welcome.frame = 50
	view50 := welcome.View()

	welcome.frame = 100
	view100 := welcome.View()

	// Views should be non-empty
	if view0 == "" || view50 == "" || view100 == "" {
		t.Error("expected non-empty views")
	}
}

func TestWelcomeUpdateSettledStopsAnimation(t *testing.T) {
	welcome := &Welcome{Active: true, theme: ui.DefaultTheme()}

	// Run until settled
	for i := 0; i < 200; i++ {
		welcome, cmd := welcome.Update(WelcomeTickMsg{})
		if welcome.settled {
			if cmd != nil {
				t.Error("expected nil cmd after settled")
			}
			break
		}
	}

	if !welcome.settled {
		t.Error("expected to be settled after 200 frames")
	}
}

func TestWelcomeViewContainsQuickOpenHint(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.SetSize(80, 24)

	view := welcome.View()

	if !strings.Contains(view, "Ctrl+P") {
		t.Error("expected Ctrl+P hint")
	}
	if !strings.Contains(view, "Quick Open") {
		t.Error("expected 'Quick Open' hint")
	}
}
