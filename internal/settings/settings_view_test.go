package settings

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/config"
	"teak/internal/ui"
)

// colorKey renders a color the way cellBackgrounds reports an active
// truecolor background.
func colorKey(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d,%d,%d", r>>8, g>>8, b>>8)
}

// cellBackgrounds returns the SGR background in effect for every printed rune
// of line ("" means the terminal default, i.e. no background was set). Inner
// lipgloss styles end with a reset, so a segment that does not carry its own
// background shows up here as "".
func cellBackgrounds(line string) []string {
	var cells []string
	bg := ""
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] != 0x1b || i+1 >= len(runes) || runes[i+1] != '[' {
			cells = append(cells, bg)
			continue
		}
		j := i + 2
		for j < len(runes) && (runes[j] < 0x40 || runes[j] > 0x7e) {
			j++
		}
		if j >= len(runes) {
			break
		}
		if runes[j] == 'm' {
			params := strings.Split(string(runes[i+2:j]), ";")
			for k := 0; k < len(params); k++ {
				switch p := params[k]; {
				case p == "" || p == "0" || p == "49":
					bg = ""
				case p == "48" && k+4 < len(params) && params[k+1] == "2":
					bg = strings.Join(params[k+2:k+5], ",")
					k += 4
				case p == "48" && k+2 < len(params) && params[k+1] == "5":
					bg = "idx" + params[k+2]
					k += 2
				case p == "38" && k+1 < len(params) && params[k+1] == "2":
					k += 4
				case p == "38" && k+1 < len(params) && params[k+1] == "5":
					k += 2
				default:
					if n, err := strconv.Atoi(p); err == nil && ((n >= 40 && n <= 47) || (n >= 100 && n <= 107)) {
						bg = "ansi" + p
					}
				}
			}
		}
		i = j
	}
	return cells
}

func newViewModel(t *testing.T, themeID string, width, height int) Model {
	t.Helper()
	model := New(ui.ThemeByName(themeID), config.DefaultConfig(), "/test/config.toml")
	model.SetSize(width, height)
	return model
}

func TestSettingsViewCellsCarryPanelBackground(t *testing.T) {
	for _, option := range ui.ThemeOptions() {
		t.Run(option.ID, func(t *testing.T) {
			model := newViewModel(t, option.ID, 66, 18)
			model.SelectNextSetting()
			model.SelectNextSetting()
			model.status = "Unsaved changes — press Ctrl+S to save"
			panel := colorKey(model.theme.Panel.GetBackground())
			cursor := colorKey(model.theme.TreeCursor.GetBackground())

			for _, category := range []int{0, 1, 2} {
				model.selectedCategory = category
				model.selectedSetting = min(2, len(model.categories[category].Settings)-1)
				selectedLabel := model.SelectedSetting().Label + ":"
				for n, line := range strings.Split(model.View(), "\n") {
					plain := ansi.Strip(line)
					want := panel
					isSelected := strings.Contains(plain, selectedLabel)
					if isSelected {
						want = cursor
					}
					cells := cellBackgrounds(line)
					if len(cells) != 66 {
						t.Fatalf("category %d line %d %q is %d cells wide, want 66", category, n, plain, len(cells))
					}
					for col, got := range cells {
						if got != want {
							t.Fatalf("category %d line %d col %d of %q has background %q, want %q", category, n, col, plain, got, want)
						}
					}
				}
			}
		})
	}
}

func TestSettingsValuesFormAColumn(t *testing.T) {
	model := newViewModel(t, "nord", 66, 20)
	view := ansi.Strip(model.View())
	columns := map[string]int{}
	for _, line := range strings.Split(view, "\n") {
		for _, label := range []string{"Tab Size:", "Format on Save:", "Insert Final Newline:", "Word Wrap:", "Scroll Margin:"} {
			if strings.Contains(line, label) {
				columns[label] = strings.Index(line, "[")
			}
		}
	}
	if len(columns) != 5 {
		t.Fatalf("rows not found in view:\n%s", view)
	}
	first := columns["Tab Size:"]
	for label, col := range columns {
		if col != first || col < 0 {
			t.Errorf("%s value starts at column %d, Tab Size at %d\n%s", label, col, first, view)
		}
	}
}

func TestSettingsControlHitTargetsMatchRenderedControls(t *testing.T) {
	for _, width := range []int{66, 40} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			model := newViewModel(t, "nord", width, 24)
			lines := strings.Split(ansi.Strip(model.View()), "\n")
			cat := model.SelectedCategory()
			for i := range cat.Settings {
				setting := &cat.Settings[i]
				if setting.Type != TypeBool && setting.Type != TypeInt {
					continue
				}
				row := lines[settingsFirstRow+i]
				idx := strings.Index(row, "[")
				if idx < 0 {
					t.Fatalf("row %q has no control", row)
				}
				if got := ansi.StringWidth(row[:idx]); got != model.settingControlStart(setting) {
					t.Errorf("%s control starts at %d, hit target at %d (%q)", setting.Label, got, model.settingControlStart(setting), row)
				}
			}
		})
	}
}

func TestSettingsConfigPathKeepsFileNameVisible(t *testing.T) {
	const home = "/Users/me"
	tests := []struct {
		name  string
		path  string
		width int
		want  string
	}{
		{"fits under home", home + "/.config/teak/config.toml", 60, "~/.config/teak/config.toml"},
		{"home itself", home, 20, "~"},
		{"home lookalike is not abbreviated", home + "x/teak/config.toml", 60, home + "x/teak/config.toml"},
		{"middle truncated under home", home + "/projects/a/very/deep/tree/teak/config.toml", 22, "~/…/teak/config.toml"},
		{"middle truncated absolute", "/private/tmp/claude-501/scratch/teak/config.toml", 22, "/…/teak/config.toml"},
		{"only the file name fits", home + "/some/long/dir/config.toml", 11, "…ig.toml"},
		{"empty", "", 20, ""},
		{"zero width", "/a/b", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displayConfigPath(tt.path, home, tt.width)
			if tt.name == "only the file name fits" {
				if ansi.StringWidth(got) > tt.width || !strings.HasSuffix(got, "g.toml") {
					t.Fatalf("displayConfigPath = %q, want a tail of the file name within %d cells", got, tt.width)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("displayConfigPath(%q, %d) = %q, want %q", tt.path, tt.width, got, tt.want)
			}
			if ansi.StringWidth(got) > tt.width {
				t.Fatalf("result %q wider than %d", got, tt.width)
			}
		})
	}
}

func TestSettingsViewShowsHomeRelativeConfigPathButSavesTheRealOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/projects/a/very/deep/tree/of/directories/teak/config.toml"
	model := New(ui.DefaultTheme(), config.DefaultConfig(), path)
	model.SetSize(40, 18)
	view := ansi.Strip(model.View())
	var hint string
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, "Config:") {
			hint = strings.TrimRight(line, " ")
		}
	}
	if !strings.Contains(hint, "config.toml") || !strings.Contains(hint, "~") || strings.Contains(hint, home) {
		t.Fatalf("config hint = %q, want home-relative path ending in config.toml", hint)
	}
	if ansi.StringWidth(hint) > 40 {
		t.Fatalf("config hint %q is wider than the view", hint)
	}
	if model.ConfigPath() != path {
		t.Fatalf("ConfigPath() = %q, want unchanged %q", model.ConfigPath(), path)
	}
}
