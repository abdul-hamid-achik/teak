package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func newTestBuffer(content string) *text.Buffer {
	return text.NewBufferFromRope(text.NewFromString(content))
}

func TestFindModelShowHide(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	if f.Visible() {
		t.Error("find should start hidden")
	}
	f.Show()
	if !f.Visible() {
		t.Error("find should be visible after Show")
	}
	f.Hide()
	if f.Visible() {
		t.Error("find should be hidden after Hide")
	}
}

func TestFindModelMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("hello world\nhello again\nnothing here")
	f.Show()
	f.input.SetValue("hello")
	f.updateMatches(buf)

	if f.MatchCount() != 2 {
		t.Errorf("expected 2 matches, got %d", f.MatchCount())
	}
	if f.CurrentMatch() != 0 {
		t.Errorf("expected current match 0, got %d", f.CurrentMatch())
	}

	m := f.CurrentMatchPosition()
	if m == nil {
		t.Fatal("expected current match position")
	}
	if m.Start.Line != 0 || m.Start.Col != 0 {
		t.Errorf("expected match at 0:0, got %d:%d", m.Start.Line, m.Start.Col)
	}
}

func TestFindModelRegex(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("foo123bar foo456bar")
	f.Show()
	f.regex = true
	f.input.SetValue(`foo\d+bar`)
	f.updateMatches(buf)

	if f.MatchCount() != 2 {
		t.Errorf("expected 2 regex matches, got %d", f.MatchCount())
	}
}

func TestFindModelNoMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("hello world")
	f.Show()
	f.input.SetValue("xyz")
	f.updateMatches(buf)

	if f.MatchCount() != 0 {
		t.Errorf("expected 0 matches, got %d", f.MatchCount())
	}
	if f.CurrentMatch() != -1 {
		t.Errorf("expected current match -1, got %d", f.CurrentMatch())
	}
}

func TestFindModelMatchRangesForLine(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	buf := newTestBuffer("aaa bbb aaa\nccc aaa")
	f.Show()
	f.input.SetValue("aaa")
	f.updateMatches(buf)

	ranges0 := f.FindMatchRangesForLine(0)
	if len(ranges0) != 2 {
		t.Errorf("expected 2 ranges on line 0, got %d", len(ranges0))
	}

	ranges1 := f.FindMatchRangesForLine(1)
	if len(ranges1) != 1 {
		t.Errorf("expected 1 range on line 1, got %d", len(ranges1))
	}

	ranges2 := f.FindMatchRangesForLine(2)
	if len(ranges2) != 0 {
		t.Errorf("expected 0 ranges on line 2, got %d", len(ranges2))
	}
}

func TestFindModelView(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	if f.View() != "" {
		t.Error("hidden find should render empty")
	}
	f.Show()
	view := f.View()
	if view == "" {
		t.Error("visible find should render non-empty")
	}
}

func TestFindModelMaxMatches(t *testing.T) {
	f := NewFindModel(ui.DefaultTheme())
	// Create a buffer with many matches
	content := ""
	for i := 0; i < 12000; i++ {
		content += "x\n"
	}
	buf := newTestBuffer(content)
	f.Show()
	f.input.SetValue("x")
	f.updateMatches(buf)

	if f.MatchCount() != 10000 {
		t.Errorf("expected matches capped at 10000, got %d", f.MatchCount())
	}
}
