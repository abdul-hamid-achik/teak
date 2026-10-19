package editor

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/mattn/go-runewidth"
	"teak/internal/ui"
)

const (
	tabLabelPadding  = 2 // Tab styles add one cell on each side.
	tabCloseWidth    = 3 // " × "
	maxTabLabelWidth = 30
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
	ScrollIdx int
	theme     ui.Theme
	nextID    int
}

// NewTabBar creates a new tab bar.
func NewTabBar(theme ui.Theme) TabBar {
	return TabBar{theme: theme}
}

// SetTheme updates rendering without changing tabs or the active selection.
func (tb *TabBar) SetTheme(theme ui.Theme) {
	tb.theme = theme
}

// AddTab adds a tab and returns its index.
func (tb *TabBar) AddTab(label, filePath string) int {
	for _, tab := range tb.Tabs {
		tb.nextID = max(tb.nextID, tab.ID+1)
	}
	id := tb.nextID
	tb.nextID++
	tb.Tabs = append(tb.Tabs, Tab{
		ID:       id,
		Label:    label,
		FilePath: filePath,
	})
	tb.ensureActiveVisible()
	return len(tb.Tabs) - 1
}

// RemoveTab removes the tab at the given index.
func (tb *TabBar) RemoveTab(idx int) {
	if idx < 0 || idx >= len(tb.Tabs) {
		return
	}
	tb.Tabs = append(tb.Tabs[:idx], tb.Tabs[idx+1:]...)
	if idx < tb.ActiveIdx {
		tb.ActiveIdx--
	} else if tb.ActiveIdx >= len(tb.Tabs) {
		tb.ActiveIdx = max(0, len(tb.Tabs)-1)
	}
	tb.ensureActiveVisible()
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
	return fmt.Sprintf("tab-file-%d-%s", tab.ID, base64.RawURLEncoding.EncodeToString([]byte(tab.FilePath)))
}

// TabCloseZoneID returns the zone ID for a tab's close button.
func TabCloseZoneID(tab Tab) string {
	if tab.FilePath == "" {
		return fmt.Sprintf("tabclose-untitled-%d", tab.ID)
	}
	return fmt.Sprintf("tabclose-file-%d-%s", tab.ID, base64.RawURLEncoding.EncodeToString([]byte(tab.FilePath)))
}

// SetActive changes the active tab and scrolls the tab window so it is visible.
func (tb *TabBar) SetActive(idx int) {
	if idx < 0 || idx >= len(tb.Tabs) {
		return
	}
	tb.ActiveIdx = idx
	tb.ensureActiveVisible()
}

// ScrollBy changes the leftmost candidate tab. The active tab remains visible
// on the next render, so wheel navigation cannot strand the selected tab.
func (tb *TabBar) ScrollBy(delta int) {
	if len(tb.Tabs) == 0 {
		tb.ScrollIdx = 0
		return
	}
	tb.ScrollIdx = min(max(0, tb.ScrollIdx+delta), len(tb.Tabs)-1)
	tb.ensureActiveVisible()
}

func (tb *TabBar) ensureActiveVisible() {
	if len(tb.Tabs) == 0 {
		tb.ActiveIdx = 0
		tb.ScrollIdx = 0
		return
	}
	tb.ActiveIdx = min(max(0, tb.ActiveIdx), len(tb.Tabs)-1)
	tb.ScrollIdx = tb.visibleStart(tb.ScrollIdx)
}

func (tb TabBar) visibleStart(start int) int {
	if len(tb.Tabs) == 0 || tb.Width <= 0 {
		return 0
	}
	start = min(max(0, start), len(tb.Tabs)-1)
	if tb.rangeContainsActive(start, tb.tabTexts()) {
		return start
	}
	return tb.ActiveIdx
}

func (tb TabBar) rangeContainsActive(start int, texts []string) bool {
	remaining := tb.Width
	for i := start; i < len(tb.Tabs) && remaining > 0; i++ {
		width := tabNaturalWidth(tb.Tabs[i], texts[i])
		if i == start {
			width = min(width, remaining)
		}
		if i == tb.ActiveIdx {
			return true
		}
		remaining -= width
	}
	return false
}

// tabNaturalWidth is the width a tab wants before the strip runs out of room.
// The dirty and diagnostic glyphs are measured with the label but sit outside
// the maxTabLabelWidth cap, which bounds the file name alone: otherwise a
// tab carrying both glyphs would truncate a name that fits.
func tabNaturalWidth(tab Tab, text string) int {
	indicators := runewidth.StringWidth(tabIndicatorPrefix(tab))
	name := max(0, runewidth.StringWidth(text)-indicators)
	return tabLabelPadding + indicators + min(name, maxTabLabelWidth) + tabCloseWidth
}

func tabLabelText(tab Tab) string {
	return tabLabelWithName(tab, tab.Label)
}

// tabLabelWithName prefixes name with the tab's dirty and diagnostic glyphs.
func tabLabelWithName(tab Tab, name string) string {
	return tabIndicatorPrefix(tab) + name
}

// tabIndicatorPrefix returns the dirty and diagnostic glyphs that precede a
// tab's name, each followed by a space.
func tabIndicatorPrefix(tab Tab) string {
	// Dirty and diagnostics use distinct glyphs: a shared marker made a dirty
	// file with errors indistinguishable from a clean file with errors.
	var prefix strings.Builder
	if tab.Dirty {
		prefix.WriteString("● ")
	}
	switch tab.DiagSeverity {
	case 1:
		prefix.WriteString("✗ ")
	case 2:
		prefix.WriteString("▲ ")
	}
	return prefix.String()
}

// tabTexts returns the full text each tab displays, before truncation.
// Editor tabs that share a base name with another open editor tab get their
// parent directory appended, deepened until the names differ, so that two
// "main.go" tabs can be told apart.
func (tb TabBar) tabTexts() []string {
	names := make([]string, len(tb.Tabs))
	groups := make(map[string][]int)
	for i, tab := range tb.Tabs {
		names[i] = tab.Label
		if tab.Kind == TabEditor && tab.FilePath != "" && tab.Label == filepath.Base(tab.FilePath) {
			groups[tab.Label] = append(groups[tab.Label], i)
		}
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		dirs := make([][]string, len(idxs))
		maxDepth := 0
		for j, i := range idxs {
			dirs[j] = strings.FieldsFunc(filepath.ToSlash(filepath.Dir(tb.Tabs[i].FilePath)), func(r rune) bool { return r == '/' })
			maxDepth = max(maxDepth, len(dirs[j]))
		}
		for depth := 1; depth <= maxDepth; depth++ {
			suffixes := make([]string, len(idxs))
			seen := make(map[string]bool, len(idxs))
			unique := true
			for j := range idxs {
				parts := dirs[j]
				suffixes[j] = strings.Join(parts[max(0, len(parts)-depth):], "/")
				if seen[suffixes[j]] {
					unique = false
				}
				seen[suffixes[j]] = true
			}
			if unique || depth == maxDepth {
				for j, i := range idxs {
					if suffixes[j] != "" {
						names[i] = tb.Tabs[i].Label + " (" + suffixes[j] + ")"
					}
				}
				break
			}
		}
	}
	texts := make([]string, len(tb.Tabs))
	for i, tab := range tb.Tabs {
		texts[i] = tabLabelWithName(tab, names[i])
	}
	return texts
}

// truncateTabLabel truncates on rune boundaries and measures terminal cells,
// not bytes. An ellipsis is used only when it fits alongside content.
func truncateTabLabel(label string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(label) <= width {
		return label
	}
	const ellipsis = "…"
	ellipsisWidth := runewidth.StringWidth(ellipsis)
	if width < ellipsisWidth {
		return ""
	}
	limit := width - ellipsisWidth
	var b strings.Builder
	used := 0
	for _, r := range label {
		runeWidth := runewidth.RuneWidth(r)
		if used+runeWidth > limit {
			break
		}
		b.WriteRune(r)
		used += runeWidth
	}
	return b.String() + ellipsis
}

// styleTabLabel colors the indicator glyphs of a plain label. Every segment is
// rendered on base, the tab's own colors: lipgloss closes each glyph's style
// with a reset, and an unstyled remainder would fall back to the terminal
// default instead of the tab background.
func styleTabLabel(tab Tab, label string, theme ui.Theme, base lipgloss.Style) string {
	var sb strings.Builder
	rest := label
	if tab.Dirty && strings.HasPrefix(rest, "● ") {
		sb.WriteString(base.Foreground(theme.GitModified.GetForeground()).Render("●"))
		sb.WriteString(base.Render(" "))
		rest = strings.TrimPrefix(rest, "● ")
	}
	if tab.DiagSeverity == 1 && strings.HasPrefix(rest, "✗ ") {
		sb.WriteString(base.Foreground(theme.DiagError.GetForeground()).Render("✗"))
		sb.WriteString(base.Render(" "))
		rest = strings.TrimPrefix(rest, "✗ ")
	} else if tab.DiagSeverity == 2 && strings.HasPrefix(rest, "▲ ") {
		sb.WriteString(base.Foreground(theme.DiagWarning.GetForeground()).Render("▲"))
		sb.WriteString(base.Render(" "))
		rest = strings.TrimPrefix(rest, "▲ ")
	}
	sb.WriteString(base.Render(rest))
	return sb.String()
}

// View renders the visible tab window. It never exceeds Width and uses the
// current active tab as an anchor whenever the strip overflows.
func (tb TabBar) View() string {
	if len(tb.Tabs) == 0 {
		return ""
	}
	if tb.Width <= 0 {
		return ""
	}
	start := tb.visibleStart(tb.ScrollIdx)
	remaining := tb.Width
	texts := tb.tabTexts()

	var tabs []string
	for i, tab := range tb.Tabs {
		if i < start || remaining <= 0 {
			continue
		}
		width := min(tabNaturalWidth(tab, texts[i]), remaining)
		if width <= 0 {
			break
		}
		remaining -= width

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

		padding := tabLabelPadding
		showClose := width >= tabLabelPadding+tabCloseWidth+1
		if width <= tabLabelPadding {
			padding = 0
			labelStyle = labelStyle.Padding(0, 0)
		}
		labelWidth := width - padding
		if showClose {
			labelWidth -= tabCloseWidth
		}
		label := truncateTabLabel(texts[i], labelWidth)
		base := lipgloss.NewStyle().
			Foreground(labelStyle.GetForeground()).
			Background(labelStyle.GetBackground()).
			Bold(labelStyle.GetBold()).
			Italic(labelStyle.GetItalic())
		styledLabel := zone.Mark(TabZoneID(tab), labelStyle.Render(styleTabLabel(tab, label, tb.theme, base)))
		if showClose {
			styledClose := zone.Mark(TabCloseZoneID(tab), closeStyle.Render(" × "))
			tabs = append(tabs, styledLabel+styledClose)
		} else {
			tabs = append(tabs, styledLabel)
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}
