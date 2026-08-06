package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestEditorVerticalMovementDoesNotMaterializeGiantLine(t *testing.T) {
	const giantLineBytes = 8 << 20
	ed := newEditor("ab\n"+strings.Repeat("x", giantLineBytes), 0, 1)
	key := tea.KeyPressMsg{Text: "down"}

	result := testing.Benchmark(func(b *testing.B) {
		candidate := ed
		for b.Loop() {
			candidate.Buffer.SetCursor(text.Position{Line: 0, Col: 1})
			candidate.verticalGoalValid = false
			candidate, _ = candidate.Update(key)
		}
	})
	if got := result.AllocedBytesPerOp(); got > 128<<10 {
		t.Fatalf("vertical movement allocated %d B/op for a giant line; want bounded below 128 KiB", got)
	}
}

func TestEditorVerticalMovementPreservesDisplayColumn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		start   text.Position
		want    text.Position
	}{
		{
			name:    "multibyte rune",
			content: "ab\néx",
			start:   text.Position{Line: 0, Col: 1},
			want:    text.Position{Line: 1, Col: len("é")},
		},
		{
			name:    "wide rune",
			content: "abx\n你x",
			start:   text.Position{Line: 0, Col: 2},
			want:    text.Position{Line: 1, Col: len("你")},
		},
		{
			name:    "tab stop",
			content: "    x\n\tx",
			start:   text.Position{Line: 0, Col: 4},
			want:    text.Position{Line: 1, Col: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newEditor(tt.content, tt.start.Line, tt.start.Col)
			ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
			if got := ed.Buffer.Cursor; got != tt.want {
				t.Fatalf("down cursor = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEditorVerticalMovementRetainsGoalAcrossShortLine(t *testing.T) {
	ed := newEditor("abcdef\nx\nabcdef", 0, 5)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 1, Col: 1}); got != want {
		t.Fatalf("first down cursor = %+v, want %+v", got, want)
	}
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 2, Col: 5}); got != want {
		t.Fatalf("second down cursor = %+v, want sticky column %+v", got, want)
	}
}

func TestEditorHorizontalMovementResetsVerticalGoal(t *testing.T) {
	ed := newEditor("abcdef\nxy\nabcdef", 0, 5)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "left"})
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 2, Col: 1}); got != want {
		t.Fatalf("cursor after down/left/down = %+v, want reset goal %+v", got, want)
	}
}

func TestEditorShiftVerticalMovementUsesDisplayColumn(t *testing.T) {
	ed := newEditor("ab\néx", 0, 1)

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "shift+down"})
	if got, want := ed.Buffer.Selections.Primary(), (text.Selection{
		Anchor: text.Position{Line: 0, Col: 1},
		Head:   text.Position{Line: 1, Col: len("é")},
	}); got != want {
		t.Fatalf("selection = %#v, want %#v", got, want)
	}
}

func TestEditorVerticalMovementSkipsCollapsedFold(t *testing.T) {
	ed := newEditor("start\nhidden one\nhidden two\nafter", 0, 2)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: 0, EndLine: 2, Collapsed: true}})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 3, Col: 2}); got != want {
		t.Fatalf("down across fold = %+v, want %+v", got, want)
	}
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "up"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 0, Col: 2}); got != want {
		t.Fatalf("up across fold = %+v, want %+v", got, want)
	}
}

func TestEditorVerticalMovementUsesWrappedRows(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("abcdefgh"))
	cfg := DefaultConfig()
	cfg.WordWrap = true
	ed := New(buf, ui.DefaultTheme(), cfg)
	ed.Viewport.Height = 2
	ed.Wrap = NewWrapLayout(buf.Line, buf.LineCount(), 4)
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 1})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "down"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 0, Col: 5}); got != want {
		t.Fatalf("down one wrapped row = %+v, want %+v", got, want)
	}
	ed, _ = ed.Update(tea.KeyPressMsg{Text: "up"})
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 0, Col: 1}); got != want {
		t.Fatalf("up one wrapped row = %+v, want %+v", got, want)
	}
}

func TestEditorArrowKeysMoveEveryCursor(t *testing.T) {
	ed := newEditor("abc\ndef", 0, 0)
	ed.Buffer.Selections.Add(text.Selection{
		Anchor: text.Position{Line: 1, Col: 0},
		Head:   text.Position{Line: 1, Col: 0},
	})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "right"})
	if got, want := ed.Buffer.Selections.All(), []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("right with multiple cursors = %#v, want %#v", got, want)
	}
}

func TestEditorShiftArrowExtendsEveryCursor(t *testing.T) {
	ed := newEditor("abc\ndef", 0, 0)
	ed.Buffer.Selections.Add(text.Selection{
		Anchor: text.Position{Line: 1, Col: 0},
		Head:   text.Position{Line: 1, Col: 0},
	})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "shift+right"})
	if got, want := ed.Buffer.Selections.All(), []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 0}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 1}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("shift+right with multiple cursors = %#v, want %#v", got, want)
	}
}

func TestEditorVerticalArrowPreservesMultipleUTF8SafeCursors(t *testing.T) {
	ed := newEditor("ab\néx\ncd", 0, 1)
	ed.Buffer.Selections.Add(text.Selection{
		Anchor: text.Position{Line: 2, Col: 1},
		Head:   text.Position{Line: 2, Col: 1},
	})

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "up"})
	if got, want := ed.Buffer.Selections.All(), []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 0}, Head: text.Position{Line: 1, Col: 0}},
	}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("up with multiple cursors = %#v, want %#v", got, want)
	}
}
