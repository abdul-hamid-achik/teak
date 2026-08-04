package editor

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/mattn/go-runewidth"
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
	if id != "tab-file-0-L3BhdGgvdG8vbWFpbi5nbw" {
		t.Errorf("expected encoded file zone ID, got %q", id)
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
	if id != "tabclose-file-0-L3BhdGgvdG8vbWFpbi5nbw" {
		t.Errorf("expected encoded file close zone ID, got %q", id)
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

func TestTabZoneIDsDoNotCollideForDistinctPaths(t *testing.T) {
	left := Tab{ID: 1, FilePath: "/work/a_b.go"}
	right := Tab{ID: 2, FilePath: "/work/a/b.go"}
	if TabZoneID(left) == TabZoneID(right) {
		t.Fatalf("label zone IDs collide: %q", TabZoneID(left))
	}
	if TabCloseZoneID(left) == TabCloseZoneID(right) {
		t.Fatalf("close zone IDs collide: %q", TabCloseZoneID(left))
	}
}

func TestTabBarViewRespectsWidthAndKeepsActiveTabVisible(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("first-file.go", "/first-file.go")
	tabBar.AddTab("second-file.go", "/second-file.go")
	tabBar.AddTab("third-file.go", "/third-file.go")
	tabBar.ActiveIdx = 2
	tabBar.Width = 12

	view := tabBar.View()
	if got := lipgloss.Width(view); got > tabBar.Width {
		t.Fatalf("tab bar width = %d, want <= %d; view %q", got, tabBar.Width, view)
	}
	if !strings.Contains(view, "third") {
		t.Fatalf("active tab is not visible in %q", view)
	}
}

func TestTruncateTabLabelUsesTerminalCellWidths(t *testing.T) {
	got := truncateTabLabel("你好世界", 5)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated label is invalid UTF-8: %q", got)
	}
	if width := runewidth.StringWidth(got); width > 5 {
		t.Fatalf("truncated label width = %d, want <= 5; got %q", width, got)
	}
	if got != "你好…" {
		t.Fatalf("truncated label = %q, want %q", got, "你好…")
	}
}

func TestTabBarRemoveBeforeActiveKeepsSameActiveTab(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("a", "/a")
	tabBar.AddTab("b", "/b")
	tabBar.AddTab("c", "/c")
	tabBar.AddTab("d", "/d")
	tabBar.ActiveIdx = 3

	tabBar.RemoveTab(1)
	if tabBar.ActiveIdx != 2 || tabBar.Tabs[tabBar.ActiveIdx].Label != "d" {
		t.Fatalf("active tab after removal = idx %d (%q), want d at idx 2", tabBar.ActiveIdx, tabBar.Tabs[tabBar.ActiveIdx].Label)
	}
}

func TestTabBarNewTabAfterRemovalKeepsIndexAndUniqueZoneID(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("a", "/a")
	tabBar.AddTab("b", "/b")
	tabBar.RemoveTab(0)
	idx := tabBar.AddTab("c", "/c")
	if idx != 1 {
		t.Fatalf("AddTab index = %d, want 1", idx)
	}
	if tabBar.Tabs[0].ID == tabBar.Tabs[1].ID {
		t.Fatalf("tab IDs collided after removal: %+v", tabBar.Tabs)
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
	// Errors get their own glyph so a clean file with errors is not mistaken
	// for a dirty one.
	if !strings.Contains(view, "✗") {
		t.Error("expected the error indicator glyph in view")
	}
}

func TestTabBarViewDiagnosticWarning(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "/main.go")
	tabBar.Tabs[0].DiagSeverity = 2 // warning
	tabBar.Width = 80

	view := tabBar.View()
	if !strings.Contains(view, "▲") {
		t.Error("expected the warning indicator glyph in view")
	}
}

func TestTabBarLabelsDistinguishDirtyFromDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		tab     Tab
		prefixes []string
	}{
		{"dirty only", Tab{Label: "a.go", Dirty: true}, []string{"●"}},
		{"error only", Tab{Label: "a.go", DiagSeverity: 1}, []string{"✗"}},
		{"warning only", Tab{Label: "a.go", DiagSeverity: 2}, []string{"▲"}},
		{"dirty and error", Tab{Label: "a.go", Dirty: true, DiagSeverity: 1}, []string{"●", "✗"}},
		{"clean", Tab{Label: "a.go"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := tabLabelText(tt.tab)
			for _, glyph := range []string{"●", "✗", "▲"} {
				want := false
				for _, p := range tt.prefixes {
					if p == glyph {
						want = true
					}
				}
				if got := strings.Contains(label, glyph); got != want {
					t.Errorf("tabLabelText(%+v) = %q, glyph %q present=%v want %v", tt.tab, label, glyph, got, want)
				}
			}
		})
	}
}

func TestTabBarScrollByMovesAndClamps(t *testing.T) {
	tabBar := NewTabBar(ui.DefaultTheme())
	for i := 0; i < 10; i++ {
		tabBar.AddTab(fmt.Sprintf("file%d.go", i), fmt.Sprintf("/f%d.go", i))
	}
	tabBar.Width = 200
	tabBar.SetActive(5)

	tabBar.ScrollBy(3)
	if tabBar.ScrollIdx != 3 {
		t.Fatalf("ScrollIdx after +3 = %d, want 3", tabBar.ScrollIdx)
	}
	tabBar.ScrollBy(-100)
	if tabBar.ScrollIdx != 0 {
		t.Fatalf("ScrollIdx after large negative scroll = %d, want 0", tabBar.ScrollIdx)
	}

	// Scrolling toward the end clamps at the last tab when it stays visible.
	tabBar.SetActive(9)
	tabBar.ScrollBy(100)
	if tabBar.ScrollIdx != 9 {
		t.Fatalf("ScrollIdx after large positive scroll = %d, want 9", tabBar.ScrollIdx)
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
