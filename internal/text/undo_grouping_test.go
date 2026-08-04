package text

import (
	"testing"
	"time"
)

// typeString inserts content one character at a time, as a user typing would.
func typeString(b *Buffer, content string) {
	for _, r := range content {
		b.InsertAtCursor([]byte(string(r)))
	}
}

func TestTypingGroupsIntoOneUndo(t *testing.T) {
	buf := NewBufferFromBytes(nil)

	typeString(buf, "hello")

	if got := buf.Content(); got != "hello" {
		t.Fatalf("buffer = %q, want %q", got, "hello")
	}

	// Every call site used to pass isCharInsert=false, so this undid one
	// character at a time and the stack held one snapshot per keystroke.
	buf.Undo()
	if got := buf.Content(); got != "" {
		t.Errorf("after one undo, buffer = %q, want %q (a typing run is one group)", got, "")
	}
}

func TestTypingAfterCursorMoveStartsNewUndoGroup(t *testing.T) {
	buf := NewBuffer()
	buf.InsertAtCursor([]byte("a"))
	buf.MoveCursor(DirLeft)
	buf.InsertAtCursor([]byte("b"))

	if got := buf.Rope().String(); got != "ba" {
		t.Fatalf("content after moved insertion = %q, want %q", got, "ba")
	}
	buf.Undo()
	if got := buf.Rope().String(); got != "a" {
		t.Fatalf("one undo after cursor move = %q, want %q", got, "a")
	}
}

func TestNewlineEndsTheUndoGroup(t *testing.T) {
	buf := NewBufferFromBytes(nil)

	typeString(buf, "ab")
	buf.InsertAtCursor([]byte("\n"))
	typeString(buf, "cd")

	if got, want := buf.Content(), "ab\ncd"; got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}

	// Enter is a boundary a user expects Ctrl+Z to stop at.
	buf.Undo()
	if got, want := buf.Content(), "ab\n"; got != want {
		t.Errorf("after one undo, buffer = %q, want %q", got, want)
	}
}

func TestPauseEndsTheUndoGroup(t *testing.T) {
	buf := NewBufferFromBytes(nil)

	typeString(buf, "ab")
	// Grouping is time-bounded; a pause longer than groupTimeout starts a new
	// group so a long editing session is not collapsed into a single undo.
	buf.undo.lastTime = time.Now().Add(-2 * groupTimeout)
	typeString(buf, "cd")

	buf.Undo()
	if got, want := buf.Content(), "ab"; got != want {
		t.Errorf("after one undo, buffer = %q, want %q", got, want)
	}
}

func TestMultiCharacterInsertIsNotGrouped(t *testing.T) {
	buf := NewBufferFromBytes(nil)

	typeString(buf, "ab")
	// A paste is a discrete action, not typing, and must be undoable on its own.
	buf.InsertAtCursor([]byte("PASTED"))

	buf.Undo()
	if got, want := buf.Content(), "ab"; got != want {
		t.Errorf("after one undo, buffer = %q, want %q", got, want)
	}
}

func TestTypingRunKeepsUndoStackSmall(t *testing.T) {
	buf := NewBufferFromBytes(nil)

	typeString(buf, "the quick brown fox jumps over the lazy dog")

	// One snapshot per keystroke also pinned one rope per entry, which is what
	// made the undo stack retain hundreds of MB on large files.
	if got := len(buf.undo.undo); got > 2 {
		t.Errorf("undo stack has %d entries after one typing run, want it grouped into ~1", got)
	}
}

func TestIsTypedRune(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"single ascii", "a", true},
		{"single multibyte", "é", true},
		{"emoji", "🙂", true},
		{"newline", "\n", false},
		{"two characters", "ab", false},
		{"text containing newline", "a\nb", false},
		{"empty", "", false},
		{"tab", "\t", true},
		{"space", " ", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTypedRune([]byte(tc.in)); got != tc.want {
				t.Errorf("isTypedRune(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
