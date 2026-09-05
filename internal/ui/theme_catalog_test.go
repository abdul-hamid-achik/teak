package ui

import (
	"testing"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

func TestThemeOptionsContainSupportedThemesInOrder(t *testing.T) {
	want := []struct {
		id, name string
		variant  ThemeVariant
	}{
		{"nord", "Nord", ThemeDark},
		{"dracula", "Dracula", ThemeDark},
		{"catppuccin", "Catppuccin Mocha", ThemeDark},
		{"solarized-dark", "Solarized Dark", ThemeDark},
		{"one-dark", "One Dark", ThemeDark},
		{"github-dark", "GitHub Dark", ThemeDark},
		{"github-light", "GitHub Light", ThemeLight},
		{"tokyo-night", "Tokyo Night", ThemeDark},
		{"ayu-mirage", "Ayu Mirage", ThemeDark},
		{"solarized-light", "Solarized Light", ThemeLight},
		{"catppuccin-latte", "Catppuccin Latte", ThemeLight},
		{"gruvbox-dark", "Gruvbox Dark", ThemeDark},
		{"monokai", "Monokai", ThemeDark},
		{"night-owl", "Night Owl", ThemeDark},
		{"material-palenight", "Material Palenight", ThemeDark},
	}

	got := ThemeOptions()
	if len(got) != len(want) {
		t.Fatalf("got %d theme options, want %d", len(got), len(want))
	}
	for i, option := range got {
		if option.ID != want[i].id || option.Name != want[i].name || option.Variant != want[i].variant || option.Constructor == nil {
			t.Errorf("option %d = %+v, want id=%q name=%q variant=%q and constructor", i, option, want[i].id, want[i].name, want[i].variant)
		}
		if option.Constructor().Editor.GetBackground() == nil {
			t.Errorf("option %q returned a theme without an editor background", option.ID)
		}
	}
}

func TestSecondWaveThemePaletteAnchors(t *testing.T) {
	tests := []struct {
		id, background, keyword string
	}{
		{"gruvbox-dark", "#282828", "#FB4934"},
		{"monokai", "#272822", "#F92672"},
		{"night-owl", "#011627", "#C792EA"},
		{"material-palenight", "#292D3E", "#C792EA"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			theme := ThemeByName(tt.id)
			if got := theme.Editor.GetBackground(); got != lipgloss.Color(tt.background) {
				t.Errorf("background = %v, want %s", got, tt.background)
			}
			if theme.SyntaxKeyword != lipgloss.Color(tt.keyword) {
				t.Errorf("keyword = %v, want %s", theme.SyntaxKeyword, tt.keyword)
			}
		})
	}
}

func TestThemeOptionsReturnsIndependentCopy(t *testing.T) {
	options := ThemeOptions()
	original := options[0].ID
	options[0].ID = "changed"
	if again := ThemeOptions()[0].ID; again != original {
		t.Fatalf("ThemeOptions leaked mutation: got %q, want %q", again, original)
	}

	ids := ThemeIDs()
	ids[0] = "changed"
	if again := ThemeIDs()[0]; again != original {
		t.Fatalf("ThemeIDs leaked mutation: got %q, want %q", again, original)
	}
}

func TestHasThemeAndThemeByNameUseCatalog(t *testing.T) {
	for _, option := range ThemeOptions() {
		if !HasTheme(option.ID) {
			t.Errorf("HasTheme(%q) = false", option.ID)
		}
		if got := ThemeByName(option.ID); got.themeStyles == nil {
			t.Errorf("ThemeByName(%q) returned an empty theme", option.ID)
		}
	}
	if HasTheme("not-a-theme") {
		t.Error("HasTheme accepted an unknown theme")
	}
}

func TestApplyTextInputThemeMakesLightThemeTextExplicit(t *testing.T) {
	input := textinput.New()
	theme := GitHubLightTheme()
	ApplyTextInputTheme(&input, theme)
	styles := input.Styles()
	if styles.Focused.Text.GetForeground() != theme.Editor.GetForeground() ||
		styles.Focused.Placeholder.GetForeground() != theme.Gutter.GetForeground() ||
		styles.Cursor.Color != theme.PromptAccent.GetForeground() {
		t.Fatal("text input did not receive readable theme foregrounds")
	}
}
