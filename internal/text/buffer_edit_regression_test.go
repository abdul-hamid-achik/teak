package text

import "testing"

// The incremental change record is sent to the language server as a pre-edit
// range. The appended newline lands at the end of the old document, so the
// range must address that old end, not the (nonexistent) line after the insert.
func TestEnsureFinalNewlineLastChangeAddressesPreEditEnd(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    EditChange
	}{
		{"single line", "abc", EditChange{StartLine: 0, StartCol: 3, EndLine: 0, EndCol: 3, Text: "\n"}},
		{"multi line", "ab\ncd", EditChange{StartLine: 1, StartCol: 2, EndLine: 1, EndCol: 2, Text: "\n"}},
		{"empty", "", EditChange{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewBufferFromBytes([]byte(tt.content))
			if !buf.EnsureFinalNewline() {
				t.Fatal("expected an insert")
			}
			got := buf.LastChange()
			if got == nil {
				t.Fatal("LastChange() = nil, want the appended newline")
			}
			if *got != tt.want {
				t.Fatalf("LastChange() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

// BackspaceWord records its undo snapshot while the cursor still sits at the
// original position. Undo must return the cursor there, as DeleteWord does.
func TestBackspaceWordUndoRestoresCursor(t *testing.T) {
	buf := NewBufferFromBytes([]byte("hello world"))
	buf.SetCursor(Position{Line: 0, Col: 11})
	buf.BackspaceWord()
	if got := buf.Content(); got != "hello " {
		t.Fatalf("content after BackspaceWord = %q, want %q", got, "hello ")
	}
	buf.Undo()
	if got := buf.Content(); got != "hello world" {
		t.Fatalf("content after undo = %q, want %q", got, "hello world")
	}
	if want := (Position{Line: 0, Col: 11}); buf.Cursor != want {
		t.Fatalf("cursor after undo = %+v, want %+v", buf.Cursor, want)
	}
	if got := buf.Selections.PrimaryCursor(); got != buf.Cursor {
		t.Fatalf("primary selection %+v disagrees with cursor %+v", got, buf.Cursor)
	}
}
