package highlight

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"teak/internal/ui"
)

type countingLexer struct {
	chroma.Lexer
	calls int
}

func (l *countingLexer) Tokenise(_ *chroma.TokeniseOptions, value string) (chroma.Iterator, error) {
	l.calls++
	return chroma.Literator(chroma.Token{Type: chroma.Text, Value: value}), nil
}

func TestLongLinesBypassRegexLexerWithoutLosingText(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		wantCalls     int
	}{
		{"at limit", strings.Repeat("x", 64*1024), 1},
		{"above limit", strings.Repeat("x", 64*1024+1), 0},
		{"unicode bytes", strings.Repeat("界", 64*1024/3+1), 0},
		{"normal lines in large file", strings.Repeat("small\n", 20000) + "end", 1},
		{"surrounding lines", "before\n" + strings.Repeat("x", 64*1024+1) + "\nafter", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := New("test.go", ui.DefaultTheme())
			lexer := &countingLexer{Lexer: h.lexer}
			h.lexer = lexer
			lines, complete := h.TokenizeToLinesContext(context.Background(), []byte(tt.content))
			if !complete {
				t.Fatal("tokenization did not finish")
			}
			if lexer.calls != tt.wantCalls {
				t.Errorf("regex lexer calls=%d, want %d", lexer.calls, tt.wantCalls)
			}
			var rendered []string
			for _, line := range lines {
				var text strings.Builder
				for _, token := range line {
					text.WriteString(token.Text)
				}
				rendered = append(rendered, text.String())
			}
			if strings.Join(rendered, "\n") != tt.content {
				t.Fatal("fallback changed source text")
			}
		})
	}
}
