package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/codemap"
	"teak/internal/editor"
	"teak/internal/overlay"
)

// codemapResultMsg carries the result of a codemap query.
type codemapResultMsg struct {
	kind       string
	symbols    []codemap.Symbol
	err        error
	generation uint64
}

type codemapSymbolPickerMsg struct {
	Symbol codemap.Symbol
}

const maxCodemapPickerItems = 500

// runCodemapQuery resolves the word at the cursor and runs a codemap query.
func (m Model) startCodemapQuery(kind string) (Model, tea.Cmd) {
	if m.codemapCancel != nil {
		m.codemapCancel()
	}
	m.codemapGeneration++
	ctx, cancel := context.WithCancel(context.Background())
	m.codemapCancel = cancel
	cmd := m.runCodemapQuery(kind, m.codemapGeneration, ctx)
	if cmd == nil {
		cancel()
		m.codemapCancel = nil
	}
	return m, cmd
}

// runCodemapQuery resolves the word at the cursor and runs a codemap query.
func (m Model) runCodemapQuery(kind string, generation uint64, ctx context.Context) tea.Cmd {
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
		if err := codemap.EnsureReady(ctx, rootDir); err != nil {
			return codemapResultMsg{kind: kind, generation: generation, err: err}
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
		return codemapResultMsg{kind: kind, generation: generation, symbols: symbols, err: err}
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

func codemapSymbolsToPickerItems(symbols []codemap.Symbol, rootDir string) []overlay.PickerItem {
	if len(symbols) > maxCodemapPickerItems {
		symbols = symbols[:maxCodemapPickerItems]
	}
	items := make([]overlay.PickerItem, 0, len(symbols))
	for _, symbol := range symbols {
		label := symbol.Symbol
		if label == "" {
			label = symbol.FQN
		}
		if label == "" {
			label = "<anonymous>"
		}
		file := symbol.File
		if rootDir != "" && !filepath.IsAbs(file) {
			file = filepath.Join(rootDir, file)
		}
		rel := file
		if rootDir != "" {
			if candidate, err := filepath.Rel(rootDir, file); err == nil {
				rel = candidate
			}
		}
		location := rel
		if location == "" {
			location = "<unknown file>"
		}
		if symbol.StartLine > 0 {
			location += ":" + strconv.Itoa(symbol.StartLine)
		}
		description := location
		if symbol.Kind != "" {
			description = symbol.Kind + " · " + description
		}
		items = append(items, overlay.PickerItem{
			Label:       label,
			Description: description,
			Value:       codemapSymbolPickerMsg{Symbol: symbol},
		})
	}
	return items
}
