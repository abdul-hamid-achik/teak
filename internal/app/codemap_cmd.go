package app

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/codemap"
	"teak/internal/editor"
)

// codemapResultMsg carries the result of a codemap query.
type codemapResultMsg struct {
	kind    string
	symbols []codemap.Symbol
	err     error
}

// runCodemapQuery resolves the word at the cursor and runs a codemap query.
func (m Model) runCodemapQuery(kind string) tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}

	word := wordAtCursor(ed)
	if word == "" {
		m.status = "No symbol at cursor"
		return nil
	}

	rootDir := m.rootDir
	return func() tea.Msg {
		ctx := context.Background()

		if err := codemap.EnsureReady(ctx, rootDir); err != nil {
			return codemapResultMsg{kind: kind, err: err}
		}

		var symbols []codemap.Symbol
		var err error
		switch kind {
		case "callers":
			symbols, err = codemap.Callers(ctx, rootDir, word)
		case "callees":
			symbols, err = codemap.Callees(ctx, rootDir, word)
		case "impact":
			var result *codemap.ImpactResult
			result, err = codemap.Impact(ctx, rootDir, word, 3)
			if result != nil {
				for _, br := range result.BlastRadius {
					symbols = append(symbols, br.Symbol)
				}
			}
		}
		return codemapResultMsg{kind: kind, symbols: symbols, err: err}
	}
}

// wordAtCursor extracts the identifier-like word under the cursor.
func wordAtCursor(ed *editor.Editor) string {
	line := ed.Buffer.Line(ed.Buffer.Cursor.Line)
	col := ed.Buffer.Cursor.Col
	if col >= len(line) {
		return ""
	}

	isWordByte := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
	}

	start := col
	for start > 0 && isWordByte(line[start-1]) {
		start--
	}
	end := col
	for end < len(line) && isWordByte(line[end]) {
		end++
	}
	return strings.TrimSpace(string(line[start:end]))
}
