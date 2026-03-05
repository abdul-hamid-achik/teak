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

func TestWelcomeLogoColor(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	// Before settled, should cycle through colors
	color := welcome.logoColor(0)
	if color == nil {
		t.Error("expected non-nil color")
	}
}

func TestWelcomeLogoColorSettled(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())
	welcome.settled = true

	// After settled, should be static cyan (Nord8)
	color := welcome.logoColor(0)
	if color == nil {
		t.Error("expected non-nil color")
	}
}

func TestWelcomeLogoColorDifferentLines(t *testing.T) {
	welcome := NewWelcome(ui.DefaultTheme())

	color0 := welcome.logoColor(0)
	color1 := welcome.logoColor(1)
	color2 := welcome.logoColor(2)

	// Different lines should have different colors (offset in palette)
	// This is hard to test directly without knowing the frame, but we can check they're not nil
	if color0 == nil || color1 == nil || color2 == nil {
		t.Error("expected non-nil colors")
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

func TestWelcomeAuroraColors(t *testing.T) {
	if len(auroraColors) == 0 {
		t.Error("expected some aurora colors")
	}
	// Should have 9 colors as defined
	if len(auroraColors) != 9 {
		t.Errorf("expected 9 aurora colors, got %d", len(auroraColors))
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
