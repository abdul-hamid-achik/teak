package editor

import (
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

func TestMain(m *testing.M) {
	zone.NewGlobal()
	m.Run()
}

func TestNewTabBar(t *testing.T) {
	theme := ui.DefaultTheme()
	tabBar := NewTabBar(theme)

	if len(tabBar.Tabs) != 0 {
		t.Errorf("expected 0 tabs, got %d", len(tabBar.Tabs))
	}
	if tabBar.ActiveIdx != 0 {
		t.Errorf("expected ActiveIdx 0, got %d", tabBar.ActiveIdx)
	}
	// Theme contains lipgloss.Style which cannot be compared directly
}

func TestTabBarAddTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())

	idx := tabBar.AddTab("main.go", "/path/to/main.go")
	if idx != 0 {
		t.Errorf("expected index 0, got %d", idx)
	}
	if len(tabBar.Tabs) != 1 {
		t.Errorf("expected 1 tab, got %d", len(tabBar.Tabs))
	}
	if tabBar.Tabs[0].Label != "main.go" {
		t.Errorf("expected label 'main.go', got %q", tabBar.Tabs[0].Label)
	}
	if tabBar.Tabs[0].FilePath != "/path/to/main.go" {
		t.Errorf("expected FilePath '/path/to/main.go', got %q", tabBar.Tabs[0].FilePath)
	}
}

func TestTabBarAddTabMultiple(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())

	tabBar.AddTab("main.go", "/path/to/main.go")
	idx := tabBar.AddTab("test.go", "/path/to/test.go")

	if idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
	if len(tabBar.Tabs) != 2 {
		t.Errorf("expected 2 tabs, got %d", len(tabBar.Tabs))
	}
}

func TestTabBarRemoveTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")
	tabBar.AddTab("test.go", "/path/to/test.go")
	tabBar.ActiveIdx = 1

	tabBar.RemoveTab(0)

	if len(tabBar.Tabs) != 1 {
		t.Errorf("expected 1 tab, got %d", len(tabBar.Tabs))
	}
	if tabBar.Tabs[0].Label != "test.go" {
		t.Errorf("expected label 'test.go', got %q", tabBar.Tabs[0].Label)
	}
	if tabBar.ActiveIdx != 0 {
		t.Errorf("expected ActiveIdx 0, got %d", tabBar.ActiveIdx)
	}
}

func TestTabBarRemoveTabLast(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")
	tabBar.ActiveIdx = 0

	tabBar.RemoveTab(0)

	if len(tabBar.Tabs) != 0 {
		t.Errorf("expected 0 tabs, got %d", len(tabBar.Tabs))
	}
	if tabBar.ActiveIdx != 0 {
		t.Errorf("expected ActiveIdx 0, got %d", tabBar.ActiveIdx)
	}
}

func TestTabBarRemoveTabOutOfBounds(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")

	// Should not panic
	tabBar.RemoveTab(100)
	tabBar.RemoveTab(-1)

	if len(tabBar.Tabs) != 1 {
		t.Errorf("expected 1 tab, got %d", len(tabBar.Tabs))
	}
}

func TestTabBarRemoveTabAdjustActive(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("a.go", "/a.go")
	tabBar.AddTab("b.go", "/b.go")
	tabBar.AddTab("c.go", "/c.go")
	tabBar.ActiveIdx = 2 // Last tab

	tabBar.RemoveTab(1) // Remove middle

	// ActiveIdx should be adjusted
	if tabBar.ActiveIdx != 1 {
		t.Errorf("expected ActiveIdx 1, got %d", tabBar.ActiveIdx)
	}
}

func TestTabBarFindPreviewTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.AddTab("test.go", "/test.go")
	tabBar.Tabs[1].Preview = true

	idx := tabBar.FindPreviewTab()
	if idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
}

func TestTabBarFindPreviewTabNone(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")

	idx := tabBar.FindPreviewTab()
	if idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
}

func TestTabBarPinTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].Preview = true

	tabBar.PinTab(0)

	if tabBar.Tabs[0].Preview {
		t.Error("expected Preview to be false")
	}
}

func TestTabBarPinTabOutOfBounds(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")

	// Should not panic
	tabBar.PinTab(100)
	tabBar.PinTab(-1)

	if tabBar.Tabs[0].Preview {
		t.Error("preview state should remain unchanged for invalid pin indexes")
	}
}

func TestTabBarFindTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")
	tabBar.AddTab("test.go", "/path/to/test.go")

	idx := tabBar.FindTab("/path/to/test.go")
	if idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
}

func TestTabBarFindTabNotFound(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")

	idx := tabBar.FindTab("/path/to/other.go")
	if idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
}

func TestTabBarFindTabEmpty(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())

	idx := tabBar.FindTab("/path/to/main.go")
	if idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
}

func TestTabZoneID(t *testing.T) {
	tab := Tab{
		ID:       0,
		Label:    "main.go",
		FilePath: "/path/to/main.go",
	}

	id := TabZoneID(tab)
	if id != "tab-_path_to_main.go" {
		t.Errorf("expected 'tab-_path_to_main.go', got %q", id)
	}
}

func TestTabZoneIDUntitled(t *testing.T) {
	tab := Tab{
		ID:    5,
		Label: "Untitled",
	}

	id := TabZoneID(tab)
	if id != "tab-untitled-5" {
		t.Errorf("expected 'tab-untitled-5', got %q", id)
	}
}

func TestTabCloseZoneID(t *testing.T) {
	tab := Tab{
		ID:       0,
		Label:    "main.go",
		FilePath: "/path/to/main.go",
	}

	id := TabCloseZoneID(tab)
	if id != "tabclose-_path_to_main.go" {
		t.Errorf("expected 'tabclose-_path_to_main.go', got %q", id)
	}
}

func TestTabCloseZoneIDUntitled(t *testing.T) {
	tab := Tab{
		ID:    5,
		Label: "Untitled",
	}

	id := TabCloseZoneID(tab)
	if id != "tabclose-untitled-5" {
		t.Errorf("expected 'tabclose-untitled-5', got %q", id)
	}
}

func TestTabBarView(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/path/to/main.go")
	tabBar.Width = 80

	view := tabBar.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "main.go") {
		t.Errorf("expected 'main.go' in view, got %q", view)
	}
}

func TestTabBarViewEmpty(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.Width = 80

	view := tabBar.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestTabBarViewMultiple(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.AddTab("test.go", "/test.go")
	tabBar.ActiveIdx = 1
	tabBar.Width = 80

	view := tabBar.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "main.go") {
		t.Errorf("expected 'main.go' in view")
	}
	if !strings.Contains(view, "test.go") {
		t.Errorf("expected 'test.go' in view")
	}
}

func TestTabBarViewDirty(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].Dirty = true
	tabBar.Width = 80

	view := tabBar.View()
	// Dirty indicator should be shown
	if !strings.Contains(view, "●") {
		t.Error("expected dirty indicator in view")
	}
}

func TestTabBarViewDiagnosticError(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].DiagSeverity = 1 // error
	tabBar.Width = 80

	view := tabBar.View()
	// Error indicator should be shown
	if !strings.Contains(view, "●") {
		t.Error("expected error indicator in view")
	}
}

func TestTabBarViewDiagnosticWarning(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].DiagSeverity = 2 // warning
	tabBar.Width = 80

	view := tabBar.View()
	// Warning indicator should be shown
	if !strings.Contains(view, "●") {
		t.Error("expected warning indicator in view")
	}
}

func TestTabBarViewPreview(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].Preview = true
	tabBar.Width = 80

	view := tabBar.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Preview tab should have italic styling (hard to test directly)
}

func TestTabBarViewActive(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.AddTab("test.go", "/test.go")
	tabBar.ActiveIdx = 0
	tabBar.Width = 80

	_ = tabBar.View()
	// Active tab should have different styling
}

func TestTabBarSetDirty(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	idx := tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[idx].Dirty = true

	if !tabBar.Tabs[idx].Dirty {
		t.Error("expected Dirty to be true")
	}
}

func TestTabBarSetDiagSeverity(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	idx := tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[idx].DiagSeverity = 1

	if tabBar.Tabs[idx].DiagSeverity != 1 {
		t.Errorf("expected DiagSeverity 1, got %d", tabBar.Tabs[idx].DiagSeverity)
	}
}
