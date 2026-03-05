package editor

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// TabKind indicates the type of content in a tab.
type TabKind int

const (
	TabEditor TabKind = iota
	TabDiff
)

// Tab represents a single open file tab.
type Tab struct {
	ID           int
	Label        string
	FilePath     string
	Dirty        bool
	DiagSeverity int     // 0=none, 1=error, 2=warning, 3=info, 4=hint
	Preview      bool    // true if this is a preview tab (single-click, not yet pinned)
	Kind         TabKind // TabEditor or TabDiff
}

// TabBar renders a horizontal tab strip.
type TabBar struct {
	Tabs      []Tab
	ActiveIdx int
	Width     int
	theme     ui.Theme
}

// NewTabBar creates a new tab bar.
func NewTabBar(theme ui.Theme) TabBar {
	return TabBar{theme: theme}
}

// AddTab adds a tab and returns its index.
func (tb *TabBar) AddTab(label, filePath string) int {
	id := len(tb.Tabs)
	tb.Tabs = append(tb.Tabs, Tab{
		ID:       id,
		Label:    label,
		FilePath: filePath,
	})
	return id
}

// RemoveTab removes the tab at the given index.
func (tb *TabBar) RemoveTab(idx int) {
	if idx < 0 || idx >= len(tb.Tabs) {
		return
	}
	tb.Tabs = append(tb.Tabs[:idx], tb.Tabs[idx+1:]...)
	if tb.ActiveIdx >= len(tb.Tabs) {
		tb.ActiveIdx = max(0, len(tb.Tabs)-1)
	}
}

// FindPreviewTab returns the index of the current preview tab, or -1 if none.
func (tb *TabBar) FindPreviewTab() int {
	for i, t := range tb.Tabs {
		if t.Preview {
			return i
		}
	}
	return -1
}

// PinTab marks a tab as no longer a preview (pinned).
func (tb *TabBar) PinTab(idx int) {
	if idx >= 0 && idx < len(tb.Tabs) {
		tb.Tabs[idx].Preview = false
	}
}

// FindTab returns the index of a tab by file path, or -1 if not found.
func (tb *TabBar) FindTab(filePath string) int {
	for i, t := range tb.Tabs {
		if t.FilePath == filePath {
			return i
		}
	}
	return -1
}

// TabZoneID returns the zone ID for a tab's label area.
func TabZoneID(tab Tab) string {
	if tab.FilePath == "" {
		return fmt.Sprintf("tab-untitled-%d", tab.ID)
	}
	return "tab-" + strings.ReplaceAll(tab.FilePath, "/", "_")
}

// TabCloseZoneID returns the zone ID for a tab's close button.
func TabCloseZoneID(tab Tab) string {
	if tab.FilePath == "" {
		return fmt.Sprintf("tabclose-untitled-%d", tab.ID)
	}
	return "tabclose-" + strings.ReplaceAll(tab.FilePath, "/", "_")
}

// View renders the tab bar. No full-width fill — just the tabs.
func (tb TabBar) View() string {
	if len(tb.Tabs) == 0 {
		return ""
	}

	var tabs []string
	for i, tab := range tb.Tabs {
		label := tab.Label
		// Diagnostic or dirty indicator: error (red) > warning (yellow) > dirty (dim)
		if tab.DiagSeverity == 1 {
			label = lipgloss.NewStyle().Foreground(ui.Nord11).Render("●") + " " + label
		} else if tab.DiagSeverity == 2 {
			label = lipgloss.NewStyle().Foreground(ui.Nord13).Render("●") + " " + label
		} else if tab.Dirty {
			label = "● " + label
		}

		var labelStyle, closeStyle lipgloss.Style
		if i == tb.ActiveIdx {
			labelStyle = tb.theme.TabActive
			closeStyle = tb.theme.TabCloseActive
		} else {
			labelStyle = tb.theme.TabInactive
			closeStyle = tb.theme.TabCloseInactive
		}
		if tab.Preview {
			labelStyle = labelStyle.Italic(true)
		}

		styledLabel := zone.Mark(TabZoneID(tab), labelStyle.Render(label))
		styledClose := zone.Mark(TabCloseZoneID(tab), closeStyle.Render(" × "))
		tabs = append(tabs, styledLabel+styledClose)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}
