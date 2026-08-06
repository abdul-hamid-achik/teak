package text

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelectWordAtCursorUsesUnicodeWordBoundaries(t *testing.T) {
	content := "café κόσμος 变量 e\u0301clair δelta_２ — 👍🏽"
	tests := []struct {
		name   string
		needle string
		want   string
	}{
		{name: "accented latin", needle: "é", want: "café"},
		{name: "greek", needle: "σ", want: "κόσμος"},
		{name: "cjk", needle: "量", want: "变量"},
		{name: "combining mark", needle: "\u0301", want: "e\u0301clair"},
		{name: "underscore and unicode digit", needle: "２", want: "δelta_２"},
		{name: "punctuation", needle: "—", want: "—"},
		{name: "emoji sequence", needle: "👍", want: "👍🏽"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte(content))
			col := strings.Index(content, tt.needle)
			if col < 0 {
				t.Fatalf("test needle %q not found", tt.needle)
			}
			buf.SetCursor(Position{Line: 0, Col: col})
			buf.SelectWordAtCursor()
			if got := string(buf.SelectedText()); got != tt.want {
				t.Fatalf("selected %q, want %q", got, tt.want)
			}
			if got, want := buf.Selections.PrimaryCursor(), buf.Cursor; got != want {
				t.Fatalf("primary cursor = %+v, cursor = %+v", got, want)
			}
		})
	}
}

func TestSelectWordAtCursorTreatsUnicodeWhitespaceAsSeparator(t *testing.T) {
	content := "left\u00a0right"
	buf := NewBufferFromBytes([]byte(content))
	buf.SetCursor(Position{Line: 0, Col: strings.Index(content, "\u00a0")})
	buf.SelectWordAtCursor()
	if !buf.Selections.Primary().IsEmpty() {
		t.Fatalf("unicode whitespace selection = %#v, want empty", buf.Selections.Primary())
	}
}

func TestMoveCursorWordUsesUnicodeBoundaries(t *testing.T) {
	content := "café 变量 — fin"
	starts := []int{
		0,
		strings.Index(content, "变量"),
		strings.Index(content, "—"),
		strings.Index(content, "fin"),
		len(content),
	}
	buf := NewBufferFromBytes([]byte(content))
	for i := 1; i < len(starts); i++ {
		buf.MoveCursorWordRight()
		if got := buf.Cursor.Col; got != starts[i] {
			t.Fatalf("right step %d = %d, want %d", i, got, starts[i])
		}
	}
	for i := len(starts) - 2; i >= 0; i-- {
		buf.MoveCursorWordLeft()
		if got := buf.Cursor.Col; got != starts[i] {
			t.Fatalf("left step %d = %d, want %d", i, got, starts[i])
		}
	}
}

func TestUnicodeWordDeletionUsesByteColumns(t *testing.T) {
	t.Run("backspace", func(t *testing.T) {
		buf := NewBufferFromBytes([]byte("café 变量"))
		buf.SetCursor(Position{Line: 0, Col: buf.Rope().Len()})
		buf.BackspaceWord()
		if got, want := buf.Content(), "café "; got != want {
			t.Fatalf("content = %q, want %q", got, want)
		}
		if got, want := buf.Cursor.Col, len("café "); got != want {
			t.Fatalf("cursor byte column = %d, want %d", got, want)
		}
	})

	t.Run("delete", func(t *testing.T) {
		buf := NewBufferFromBytes([]byte("café 变量"))
		buf.DeleteWord()
		if got, want := buf.Content(), "变量"; got != want {
			t.Fatalf("content = %q, want %q", got, want)
		}
		change := buf.LastChange()
		if change == nil || change.StartCol != 0 || change.EndCol != len("café ") {
			t.Fatalf("LastChange = %#v, want byte range [0,%d)", change, len("café "))
		}
	})
}

func TestWordNavigationHandlesInvalidUTF8AsSymbols(t *testing.T) {
	t.Run("invalid leading bytes", func(t *testing.T) {
		content := []byte{0xff, 0xfe, ' ', 'a'}
		buf := NewBufferFromBytes(content)
		buf.SelectWordAtCursor()
		if got := buf.SelectedText(); !bytes.Equal(got, []byte{0xff, 0xfe}) {
			t.Fatalf("selected bytes = %v, want invalid-byte run", got)
		}
		buf.ClearSelection()
		buf.SetCursor(Position{})
		buf.MoveCursorWordRight()
		if got := buf.Cursor.Col; got != 3 {
			t.Fatalf("cursor after invalid-byte run = %d, want 3", got)
		}
	})

	t.Run("stray continuation byte", func(t *testing.T) {
		content := []byte{'a', 0x80, ' ', 'b'}
		buf := NewBufferFromBytes(content)
		buf.SetCursor(Position{Line: 0, Col: 1})
		buf.SelectWordAtCursor()
		if got := buf.SelectedText(); !bytes.Equal(got, []byte{0x80}) {
			t.Fatalf("selected bytes = %v, want stray continuation byte", got)
		}
		buf.ClearSelection()
		buf.SetCursor(Position{Line: 0, Col: 1})
		buf.MoveCursorWordRight()
		if got := buf.Cursor.Col; got != 3 {
			t.Fatalf("cursor after stray continuation byte = %d, want 3", got)
		}
	})
}

func TestWordNavigationAlignsStaleMidRuneCursor(t *testing.T) {
	const content = "café"
	insideAccent := strings.Index(content, "é") + 1

	buf := NewBufferFromBytes([]byte(content))
	buf.Cursor = Position{Line: 0, Col: insideAccent}
	buf.SelectWordAtCursor()
	if got := string(buf.SelectedText()); got != content {
		t.Fatalf("selected %q from mid-rune cursor, want %q", got, content)
	}

	buf.ClearSelection()
	buf.Cursor = Position{Line: 0, Col: insideAccent}
	buf.MoveCursorWordRight()
	if got, want := buf.Cursor.Col, len(content); got != want {
		t.Fatalf("right from mid-rune cursor = %d, want %d", got, want)
	}
}

func TestASCIIWordNavigationSemanticsRemainStable(t *testing.T) {
	buf := NewBufferFromBytes([]byte("hello world"))
	buf.MoveCursorWordRight()
	if got := buf.Cursor.Col; got != len("hello ") {
		t.Fatalf("first right = %d, want %d", got, len("hello "))
	}
	buf.MoveCursorWordRight()
	buf.MoveCursorWordLeft()
	if got := buf.Cursor.Col; got != len("hello ") {
		t.Fatalf("left from end = %d, want %d", got, len("hello "))
	}

	buf = NewBufferFromBytes([]byte("foo.bar"))
	buf.MoveCursorWordRight()
	if got := buf.Cursor.Col; got != len("foo") {
		t.Fatalf("right to punctuation = %d, want %d", got, len("foo"))
	}
	buf.MoveCursorWordRight()
	if got := buf.Cursor.Col; got != len("foo.") {
		t.Fatalf("right across punctuation = %d, want %d", got, len("foo."))
	}
}
