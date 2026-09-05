package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestSelectionsPreserveSurroundingSyntax(t *testing.T) {
	for _, scroll := range []int{0, 4} {
		for _, tabSize := range []int{2, 4} {
			buf := text.NewBufferFromBytes([]byte("return\tcafé + 42\nreturn\tcafé + 42"))
			buf.FilePath = "sample.go"
			theme := ui.NordTheme()
			cfg := DefaultConfig()
			cfg.TabSize = tabSize
			ed := New(buf, theme, cfg)
			ed.SetSize(40, 4)
			ed.Highlighter.Tokenize([]byte(buf.Rope().String()))
			ed.Viewport.ScrollX = scroll
			plain := ansi.Strip(ed.Viewport.Render(buf, theme, ed.Highlighter, nil, nil))
			buf.RestoreSelections([]text.Selection{
				{Anchor: text.Position{Line: 0, Col: 7}, Head: text.Position{Line: 0, Col: 12}},
				{Anchor: text.Position{Line: 1, Col: 7}, Head: text.Position{Line: 1, Col: 12}},
			}, 0)
			rendered := ed.Viewport.Render(buf, theme, ed.Highlighter, nil, nil)
			if got := ansi.Strip(rendered); got != plain {
				t.Fatalf("scroll=%d tab=%d: selections changed text layout\ngot %q\nwant %q", scroll, tabSize, got, plain)
			}
			term := vt.NewEmulator(40, 4)
			if _, err := term.Write([]byte(strings.ReplaceAll(rendered, "\n", "\r\n"))); err != nil {
				t.Fatal(err)
			}
			for line, content := range strings.Split(plain, "\n")[:2] {
				x := strings.Index(content, "return"[scroll:])
				if x < 0 {
					t.Fatalf("keyword is missing from %q", content)
				}
				cell := term.CellAt(x, line)
				if cell.Style.Fg == nil || cell.Style.Fg != theme.SyntaxKeyword {
					t.Errorf("scroll=%d tab=%d line=%d: unselected keyword color = %v, want %v", scroll, tabSize, line, cell.Style.Fg, theme.SyntaxKeyword)
				}
			}
			term.Close()
		}
	}
}
