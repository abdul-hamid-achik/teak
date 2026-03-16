package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/editor"
)

func renderedRuneStartWidth(t *testing.T, ed editor.Editor, row int, marker rune) int {
	t.Helper()

	lines := strings.Split(ed.View(), "\n")
	if row >= len(lines) {
		t.Fatalf("rendered row %d out of range (%d lines)", row, len(lines))
	}

	line := ansi.Strip(lines[row])
	start := strings.IndexRune(line, marker)
	if start < 0 {
		t.Fatalf("marker %q not found in rendered line %q", marker, line)
	}

	return ansi.StringWidth(line[:start])
}

func TestAppMouseClickFoldGutterWithTreeOffset(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()

	cfg := config.DefaultConfig()
	model := newSaveFlowModel(t, cfg, t.TempDir())
	addDirtyEditor(t, &model, "main.go", "zero\none\ntwo\nthree\n", "zero\none\ntwo\nthree\n")

	model.showTree = true
	model.width = 120
	model.height = 40
	model.relayout()

	model.editors[model.activeTab].Folds.Regions = []editor.FoldRegion{{StartLine: 1, EndLine: 2}}

	localX := renderedRuneStartWidth(t, model.editors[model.activeTab], 1, '\U000f0140')
	click := tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseLeft,
		X:      model.treeWidth() + 1 + localX,
		Y:      2, // tab bar row + visible fold row
	})

	updatedAny, cmd := model.Update(click)
	if cmd != nil {
		t.Fatalf("fold gutter click returned unexpected command")
	}

	updated := updatedAny.(Model)
	if !updated.editors[updated.activeTab].Folds.Regions[0].Collapsed {
		t.Fatalf("fold region should collapse after routed gutter click")
	}
}
