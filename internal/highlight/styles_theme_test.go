package highlight

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"teak/internal/ui"
)

func TestErrorTokensUseActiveThemeForeground(t *testing.T) {
	theme := ui.DraculaTheme()
	styles := buildStyleMap(theme)
	want := lipgloss.NewStyle().Foreground(theme.DiagError.GetForeground()).Render("x")
	for _, token := range []chroma.TokenType{chroma.Error, chroma.GenericDeleted} {
		if got := styles[token].Render("x"); got != want {
			t.Errorf("%v = %q, want active theme error style %q", token, got, want)
		}
	}
}
