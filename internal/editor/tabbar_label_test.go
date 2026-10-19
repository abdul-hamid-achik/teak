package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// The dirty and diagnostic glyphs are part of the measured label. A tab must
// show its whole name whenever there is room, and when there is not, the end
// of the name is replaced by an ellipsis: the beginning of a file name is the
// part people read, so it is never the part that gets cut.
func TestTabBarLabelsKeepFullNameWhenThereIsRoom(t *testing.T) {
	const longName = "a-rather-long-file-name-a.go" // 28 cells
	tests := []struct {
		name  string
		width int
		tabs  []Tab
		want  []string // substrings of the plain-text tab bar, in order
		avoid []string
	}{
		{
			name:  "dirty and error",
			width: 140,
			tabs:  []Tab{{Label: "main.go", FilePath: "/w/main.go", Dirty: true, DiagSeverity: 1}},
			want:  []string{" ● ✗ main.go  × "},
		},
		{
			name:  "dirty only",
			width: 140,
			tabs:  []Tab{{Label: "main.go", FilePath: "/w/main.go", Dirty: true}},
			want:  []string{" ● main.go  × "},
		},
		{
			name:  "error only",
			width: 140,
			tabs:  []Tab{{Label: "main.go", FilePath: "/w/main.go", DiagSeverity: 1}},
			want:  []string{" ✗ main.go  × "},
		},
		{
			name:  "dirty and warning",
			width: 140,
			tabs:  []Tab{{Label: "main.go", FilePath: "/w/main.go", Dirty: true, DiagSeverity: 2}},
			want:  []string{" ● ▲ main.go  × "},
		},
		{
			name:  "preview tab",
			width: 140,
			tabs:  []Tab{{Label: "main.go", FilePath: "/w/main.go", Dirty: true, DiagSeverity: 1, Preview: true}},
			want:  []string{" ● ✗ main.go  × "},
		},
		{
			name:  "duplicate names are disambiguated with their indicators",
			width: 140,
			tabs: []Tab{
				{Label: "main.go", FilePath: "/w/api/main.go", Dirty: true, DiagSeverity: 1},
				{Label: "main.go", FilePath: "/w/web/main.go"},
			},
			want: []string{" ● ✗ main.go (api)  × ", " main.go (web)  × "},
		},
		{
			// The 30-cell cap bounds the name; indicators come on top of it
			// instead of eating into it.
			name:  "indicators do not shrink a name that fits the cap",
			width: 140,
			tabs:  []Tab{{Label: longName, FilePath: "/w/" + longName, Dirty: true, DiagSeverity: 1}},
			want:  []string{" ● ✗ " + longName + "  × "},
		},
		{
			name:  "narrow width truncates the end of the name",
			width: 20,
			tabs:  []Tab{{Label: "internal-store.go", FilePath: "/w/internal-store.go", Dirty: true, DiagSeverity: 1}},
			want:  []string{" ● ✗ internal-s…  × "},
			avoid: []string{"-store", "store.go"},
		},
		{
			name:  "narrow width keeps the first characters without indicators",
			width: 16,
			tabs:  []Tab{{Label: "internal-store.go", FilePath: "/w/internal-store.go"}},
			want:  []string{" internal-s…  × "},
		},
		{
			name:  "narrow width with error only",
			width: 16,
			tabs:  []Tab{{Label: "internal-store.go", FilePath: "/w/internal-store.go", DiagSeverity: 1}},
			want:  []string{" ✗ internal…  × "},
		},
		{
			name:  "many tabs share a narrow bar without clipping names from the left",
			width: 40,
			tabs: []Tab{
				{Label: "alpha.go", FilePath: "/w/alpha.go", Dirty: true, DiagSeverity: 1},
				{Label: "beta.go", FilePath: "/w/beta.go"},
				{Label: "gamma.go", FilePath: "/w/gamma.go"},
			},
			want: []string{" ● ✗ alpha.go  × ", " beta.go  × "},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tabBar := NewTabBar(ui.DefaultTheme())
			for _, tab := range tt.tabs {
				idx := tabBar.AddTab(tab.Label, tab.FilePath)
				tabBar.Tabs[idx].Dirty = tab.Dirty
				tabBar.Tabs[idx].DiagSeverity = tab.DiagSeverity
				tabBar.Tabs[idx].Preview = tab.Preview
			}
			tabBar.Width = tt.width

			view := tabBar.View()
			if got := lipgloss.Width(view); got > tt.width {
				t.Fatalf("tab bar width = %d, want <= %d", got, tt.width)
			}
			plain := ansi.Strip(view)
			for _, want := range tt.want {
				if !strings.Contains(plain, want) {
					t.Errorf("tab bar %q does not contain %q", plain, want)
				}
			}
			for _, avoid := range tt.avoid {
				if strings.Contains(plain, avoid) {
					t.Errorf("tab bar %q contains %q; a name must be cut from the end", plain, avoid)
				}
			}
		})
	}
}

// The indicator glyphs carry their own colors, and lipgloss closes each with a
// reset. The label text after them must still carry the tab's own colors, or
// the file name of a dirty or failing tab is painted in the terminal default
// instead of the tab background.
func TestTabBarLabelKeepsTabColorsAfterIndicators(t *testing.T) {
	for _, entry := range ui.ThemeOptions() {
		theme := entry.Constructor()
		t.Run(entry.ID, func(t *testing.T) {
			tabBar := NewTabBar(theme)
			active := tabBar.AddTab("main.go", "/w/main.go")
			tabBar.Tabs[active].Dirty = true
			tabBar.Tabs[active].DiagSeverity = 1
			other := tabBar.AddTab("other.go", "/w/other.go")
			tabBar.Tabs[other].DiagSeverity = 2
			tabBar.ActiveIdx = active
			tabBar.Width = 80

			view := tabBar.View()
			buf := helpCells(view)
			plain := []rune(ansi.Strip(view))
			check := func(name string, style lipgloss.Style) {
				t.Helper()
				x := strings.Index(string(plain), name)
				if x < 0 {
					t.Fatalf("tab bar %q has no %q", string(plain), name)
				}
				x = ansi.StringWidth(string(plain)[:x])
				for i := range ansi.StringWidth(name) {
					cell := buf.CellAt(x+i, 0)
					if !sameHelpColor(cell.Style.Bg, style.GetBackground()) || !sameHelpColor(cell.Style.Fg, style.GetForeground()) {
						t.Fatalf("%q cell %d: fg=%v bg=%v, want fg=%v bg=%v", name, i,
							cell.Style.Fg, cell.Style.Bg, style.GetForeground(), style.GetBackground())
					}
				}
			}
			check(" main.go", theme.TabActive)
			check(" other.go", theme.TabInactive)
		})
	}
}
